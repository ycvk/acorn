package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/tooling"
)

type StreamingToolExecutor struct {
	node          *SafeParallelToolsNode
	scheduler     *toolExecutionScheduler
	ctx           context.Context
	siblingCtx    context.Context
	siblingCancel context.CancelFunc

	mu                sync.Mutex
	submitted         []*submittedTool
	resultIndex       map[string]int
	completed         int
	discarded         bool
	classificationErr error
	sem               chan struct{}
}

type submittedTool struct {
	call    schema.ToolCall
	status  toolExecutionStatus
	isSafe  bool
	index   int
	paths   []string
	argsErr string
	result  *schema.Message
	err     error
}

type toolExecutionStatus int

const (
	statusQueued toolExecutionStatus = iota
	statusExecuting
	statusCompleted
	statusYielded
)

func NewStreamingToolExecutor(node *SafeParallelToolsNode, scheduler *toolExecutionScheduler, ctx context.Context) *StreamingToolExecutor {
	siblingCtx, cancel := context.WithCancel(ctx)
	maxP := 1
	if scheduler != nil && scheduler.maxParallel > 0 {
		maxP = scheduler.maxParallel
	}
	return &StreamingToolExecutor{
		node:          node,
		scheduler:     scheduler,
		ctx:           ctx,
		siblingCtx:    siblingCtx,
		siblingCancel: cancel,
		resultIndex:   make(map[string]int),
		sem:           make(chan struct{}, maxP),
	}
}

func (e *StreamingToolExecutor) Submit(call schema.ToolCall) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.discarded || e.classificationErr != nil {
		return
	}

	idx := len(e.submitted)
	e.resultIndex[strings.TrimSpace(call.ID)] = idx

	var args map[string]any
	argsErr := ""
	if unmarshalErr := json.Unmarshal([]byte(call.Function.Arguments), &args); unmarshalErr != nil {
		args = nil
		argsErr = unmarshalErr.Error()
	}

	policy := tooling.ToolExecutionPolicy{ParallelPolicy: tooling.ParallelPolicySerial}
	isSafe := false
	var paths []string
	if argsErr == "" {
		policyErr := error(nil)
		var pathErr error
		resolvedPolicy, policyErr := e.scheduler.resolver.ExecutionPolicy(call.Function.Name, args)
		if policyErr == nil {
			policy = resolvedPolicy
		}
		isSafe = policy.ParallelPolicy == tooling.ParallelPolicyReadOnly
		if policy.ParallelPolicy == tooling.ParallelPolicySerial && strings.TrimSpace(policy.PathArg) == "" {
			argsErr = fmt.Sprintf("write-scoped tool %q is missing path arg", call.Function.Name)
		} else {
			paths, pathErr = executionPathsFromArgs(args, policy.PathArg, policy.ParallelPolicy == tooling.ParallelPolicySerial)
			if pathErr != nil {
				argsErr = pathErr.Error()
			}
		}
	}

	st := &submittedTool{
		call:    call,
		status:  statusQueued,
		isSafe:  isSafe,
		index:   idx,
		paths:   paths,
		argsErr: argsErr,
	}
	e.submitted = append(e.submitted, st)

	if e.canExecuteImmediately(st) {
		e.startExecution(st)
	}
}

func (e *StreamingToolExecutor) canExecuteImmediately(st *submittedTool) bool {
	for _, other := range e.submitted {
		if other.status != statusExecuting {
			continue
		}
		if (!st.isSafe && len(st.paths) == 0) || (!other.isSafe && len(other.paths) == 0) {
			return false
		}
		if pathsOverlap(st.paths, other.paths) {
			return false
		}
	}
	return true
}

func (e *StreamingToolExecutor) startExecution(st *submittedTool) {
	st.status = statusExecuting

	go func() {
		e.sem <- struct{}{}
		defer func() {
			if r := recover(); r != nil {
				e.mu.Lock()
				if !e.discarded {
					st.status = statusCompleted
					st.err = fmt.Errorf("tool execution panic: %v", r)
					e.completed++
				}
				e.mu.Unlock()
			}
			<-e.sem
		}()

		call := classifiedCall{
			index:    st.index,
			toolCall: st.call,
			safety:   tooling.ParallelPolicySerial,
			argsErr:  st.argsErr,
			paths:    st.paths,
		}
		if len(st.paths) > 0 {
			call.safety = tooling.ParallelPolicySerial
		} else if st.isSafe {
			call.safety = tooling.ParallelPolicyReadOnly
		}

		msg, err := e.node.invokeSingle(e.siblingCtx, call)

		e.mu.Lock()
		defer e.mu.Unlock()

		if e.discarded {
			return
		}

		st.status = statusCompleted
		st.result = msg
		st.err = err

		if err != nil {
			if IsInterruptError(err) {
				e.siblingCancel()
			}
		}

		e.completed++
	}()
}

func (e *StreamingToolExecutor) GetRemainingResults(ctx context.Context) ([]*schema.Message, error) {
	e.mu.Lock()
	classificationErr := e.classificationErr
	if len(e.submitted) == 0 {
		e.mu.Unlock()
		return nil, fmt.Errorf("safe parallel tools node: no tool calls in input message")
	}
	e.mu.Unlock()
	if classificationErr != nil {
		return nil, classificationErr
	}
	for {
		e.mu.Lock()
		allDone := e.completed >= len(e.submitted)
		e.mu.Unlock()

		if allDone {
			break
		}

		e.mu.Lock()
		for _, st := range e.submitted {
			if st.status != statusQueued {
				continue
			}
			if e.canExecuteImmediately(st) {
				e.startExecution(st)
			}
		}
		e.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-e.siblingCtx.Done():
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	output := make([]*schema.Message, len(e.submitted))
	for i, st := range e.submitted {
		if st.err != nil {
			if IsInterruptError(st.err) {
				return nil, st.err
			}
			return nil, fmt.Errorf("tool %q (id %s) runtime failure: %w", st.call.Function.Name, st.call.ID, st.err)
		}
		output[i] = st.result
	}
	return output, nil
}

func (e *StreamingToolExecutor) Discard() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.discarded = true
	e.siblingCancel()
}
