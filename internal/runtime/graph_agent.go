package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"

	"github.com/ycvk/acorn/internal/orchestration"
)

type graphAgent struct {
	name        string
	description string
	runnable    compose.Runnable[*agentGraphInput, *schema.Message]
	model       einomodel.BaseChatModel
	tools       []einotool.BaseTool
	handlers    []adk.ChatModelAgentMiddleware
	maxIter     int
	store       compose.CheckPointStore
	bindContext func(context.Context) context.Context
}

func newGraphAgent(
	name string,
	description string,
	runnable compose.Runnable[*agentGraphInput, *schema.Message],
	model einomodel.BaseChatModel,
	tools []einotool.BaseTool,
	handlers []adk.ChatModelAgentMiddleware,
	maxIter int,
	store compose.CheckPointStore,
	bindContext func(context.Context) context.Context,
) *graphAgent {
	return &graphAgent{
		name:        name,
		description: description,
		runnable:    runnable,
		model:       model,
		tools:       tools,
		handlers:    handlers,
		maxIter:     maxIter,
		store:       store,
		bindContext: bindContext,
	}
}

func (a *graphAgent) Name(_ context.Context) string {
	return a.name
}

func (a *graphAgent) Description(_ context.Context) string {
	return a.description
}

func (a *graphAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				generator.Send(&adk.AgentEvent{AgentName: a.name, Err: fmt.Errorf("agent panic: %v", r)})
			}
			generator.Close()
		}()

		if a.bindContext != nil {
			ctx = a.bindContext(ctx)
		}

		graphInput := &agentGraphInput{
			Messages: input.Messages,
		}

		msg, err := a.runnable.Invoke(ctx, graphInput)
		if err != nil {
			if _, ok := compose.ExtractInterruptInfo(err); ok {
				generator.Send(a.buildInterruptEvent(err))
				return
			}
			if signal, ok := errors.AsType[*adk.InterruptSignal](err); ok {
				generator.Send(a.buildInterruptEventFromSignal(signal))
				return
			}
			generator.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}

		generator.Send(&adk.AgentEvent{
			AgentName: a.name,
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					Message: msg,
					Role:    schema.Assistant,
				},
			},
		})
	}()

	return iterator
}

func (a *graphAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				generator.Send(&adk.AgentEvent{AgentName: a.name, Err: fmt.Errorf("agent resume panic: %v", r)})
			}
			generator.Close()
		}()

		if a.bindContext != nil {
			ctx = a.bindContext(ctx)
		}

		graphInput := &agentGraphInput{
			Messages: nil,
		}

		var runOpts []compose.Option
		if info != nil && info.InterruptInfo != nil {
			// Compose checkpoint is keyed by the same ID the Runner
			// stores in InterruptInfo.Data.
			runOpts = append(runOpts, compose.WithCheckPointID(fmt.Sprintf("%s", info.InterruptInfo.Data)))
		}

		msg, err := a.runnable.Invoke(ctx, graphInput, runOpts...)
		if err != nil {
			if _, ok := compose.ExtractInterruptInfo(err); ok {
				generator.Send(a.buildInterruptEvent(err))
				return
			}
			if signal, ok := errors.AsType[*adk.InterruptSignal](err); ok {
				generator.Send(a.buildInterruptEventFromSignal(signal))
				return
			}
			generator.Send(&adk.AgentEvent{AgentName: a.name, Err: err})
			return
		}

		generator.Send(&adk.AgentEvent{
			AgentName: a.name,
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					Message: msg,
					Role:    schema.Assistant,
				},
			},
		})
	}()

	return iterator
}

func graphAgentContextBinder(buildCtx context.Context) func(context.Context) context.Context {
	lifecycle := contextplane.ToolLifecycleContextFromContext(buildCtx)
	if lifecycle == nil {
		return nil
	}
	infos := toolInfosFromLifecycle(lifecycle)
	return func(runCtx context.Context) context.Context {
		if contextplane.ToolLifecycleContextFromContext(runCtx) != nil {
			return runCtx
		}
		return contextplane.WithToolLifecycleContext(runCtx, lifecycle.Plane, lifecycle.State, lifecycle.Catalog, infos)
	}
}

func toolInfosFromLifecycle(lifecycle *contextplane.ToolLifecycleContext) []*schema.ToolInfo {
	if lifecycle == nil || len(lifecycle.ToolInfosByName) == 0 {
		return nil
	}
	names := make([]string, 0, len(lifecycle.ToolInfosByName))
	for name := range lifecycle.ToolInfosByName {
		names = append(names, name)
	}
	sort.Strings(names)
	infos := make([]*schema.ToolInfo, 0, len(names))
	for _, name := range names {
		if info := lifecycle.ToolInfosByName[name]; info != nil {
			infos = append(infos, info)
		}
	}
	return infos
}

func (a *graphAgent) buildInterruptEvent(err error) *adk.AgentEvent {
	interruptInfo, _ := compose.ExtractInterruptInfo(err)
	return &adk.AgentEvent{
		AgentName: a.name,
		Action: &adk.AgentAction{
			Interrupted: &adk.InterruptInfo{Data: interruptInfo},
		},
	}
}

func (a *graphAgent) buildInterruptEventFromSignal(signal *adk.InterruptSignal) *adk.AgentEvent {
	return &adk.AgentEvent{
		AgentName: a.name,
		Action: &adk.AgentAction{
			Interrupted: orchestration.InterruptInfoFromSignal(signal),
		},
	}
}

var _ adk.ResumableAgent = (*graphAgent)(nil)
