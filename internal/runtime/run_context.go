package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// RunContext represents a single run in the execution tree.
// It replaces the activeRunID/activeSink singleton pattern with
// per-run context that supports parent-child relationships and
// mid-finalization safety.
type RunContext struct {
	RunID    string
	ParentID string   // empty for root runs
	ChildIDs []string // NOT []*RunContext — prevent GC leak (Oracle ruling)
	Depth    int      // 0 for root
	Budget   *RunBudget
	Sink     StreamSink

	// finalizing is true during finishCollectedRun.
	// Children in finalization phase are allowed to complete
	// when InterruptTree is called on a parent.
	finalizing atomic.Bool
}

// IsFinalizing returns true if this run is in its finalization phase.
func (rc *RunContext) IsFinalizing() bool {
	if rc == nil {
		return false
	}
	return rc.finalizing.Load()
}

// SetFinalizing marks the run as entering finalization.
func (rc *RunContext) SetFinalizing() {
	if rc != nil {
		rc.finalizing.Store(true)
	}
}

// RunBudget tracks iteration and token budget for a run.
type RunBudget struct {
	MaxIterations int // from config Agent.MaxIterations
}

// NewRunBudget creates a RunBudget with the given limits.
func NewRunBudget(maxIterations int) *RunBudget {
	return &RunBudget{
		MaxIterations: maxIterations,
	}
}

// runRegistry provides thread-safe registration of RunContext instances.
// Uses sync.Mutex (NOT sync.Map — Metis ruling #3a for atomic parent-child registration).
type runRegistry struct {
	mu      sync.Mutex
	entries map[string]*RunContext // keyed by runID
}

func newRunRegistry() *runRegistry {
	return &runRegistry{
		entries: make(map[string]*RunContext),
	}
}

// Register atomically adds a RunContext and links it to its parent.
// Returns error if parent not found.
func (r *runRegistry) Register(ctx *RunContext) error {
	if ctx == nil {
		return fmt.Errorf("run context is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if ctx.ParentID != "" {
		parent, ok := r.entries[ctx.ParentID]
		if !ok {
			return fmt.Errorf("parent run %s not found", ctx.ParentID)
		}
		parent.ChildIDs = append(parent.ChildIDs, ctx.RunID)
		ctx.Depth = parent.Depth + 1
	}

	r.entries[ctx.RunID] = ctx
	return nil
}

// Clear removes a run and all its children from the registry.
func (r *runRegistry) Clear(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		return
	}

	// BFS to collect all descendant IDs
	queue := []string{runID}
	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]
		current, ok := r.entries[currentID]
		if !ok {
			continue
		}
		if current.ChildIDs != nil {
			queue = append(queue, current.ChildIDs...)
		}
		delete(r.entries, currentID)
	}
}

// Get returns a RunContext by ID.
func (r *runRegistry) Get(runID string) (*RunContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		return nil, false
	}
	ctx, ok := r.entries[runID]
	return ctx, ok
}

// InterruptTree interrupts a run and all its children.
// Children in finalization phase are allowed to complete.
// cancelFuncs maps runID to context.CancelFunc.
// The interrupt order is leaf-to-root: children are cancelled first,
// then the target run itself.
func (r *runRegistry) InterruptTree(runID string, cancelFuncs map[string]context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.entries == nil {
		return
	}

	// BFS to collect all descendants, then reverse for leaf-to-root order
	var toInterrupt []string
	queue := []string{runID}
	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]
		current, ok := r.entries[currentID]
		if !ok {
			continue
		}
		// Skip runs in finalization phase
		if current.IsFinalizing() {
			continue
		}
		toInterrupt = append(toInterrupt, currentID)
		if current.ChildIDs != nil {
			queue = append(queue, current.ChildIDs...)
		}
	}

	// Reverse order: interrupt children (leaves) before parents
	for i := len(toInterrupt) - 1; i >= 0; i-- {
		id := toInterrupt[i]
		if cancel, ok := cancelFuncs[id]; ok {
			cancel()
		}
	}
}
