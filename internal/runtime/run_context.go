package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ycvk/acorn/internal/domain"
)

// RunContext represents a single run in the execution tree. It carries the
// parent-child links used for cascade cleanup of subagent runs.
type RunContext struct {
	RunID    string
	ParentID string   // empty for root runs
	ChildIDs []string // child run IDs (stored as strings, not pointers, to avoid GC retention)
	Depth    int      // 0 for root; propagated to subagents
}

// Registry provides thread-safe registration of RunContext instances.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*RunContext // keyed by runID
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		entries: make(map[string]*RunContext),
	}
}

// Register atomically adds a RunContext and links it to its parent.
// Returns error if parent not found.
func (r *Registry) Register(ctx *RunContext) error {
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

// Clear removes a run and all its descendants from the registry.
func (r *Registry) Clear(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		return
	}
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
func (r *Registry) Get(runID string) (*RunContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		return nil, false
	}
	ctx, ok := r.entries[runID]
	return ctx, ok
}

// RunController tracks per-run cancellation functions so an in-flight run can
// be interrupted by ID.
type RunController struct {
	activeMu      sync.Mutex
	activeCancels map[string]context.CancelFunc
}

func NewRunController() *RunController {
	return &RunController{}
}

func (c *RunController) Register(runID string, cancel context.CancelFunc) {
	if c == nil || strings.TrimSpace(runID) == "" || cancel == nil {
		return
	}
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeCancels == nil {
		c.activeCancels = make(map[string]context.CancelFunc)
	}
	c.activeCancels[runID] = cancel
}

func (c *RunController) Clear(runID string) {
	if c == nil || strings.TrimSpace(runID) == "" {
		return
	}
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.activeCancels == nil {
		return
	}
	delete(c.activeCancels, runID)
}

func (c *RunController) Interrupt(runID string) error {
	if c == nil {
		return fmt.Errorf("run controller is nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run id", domain.ErrRunNotActive)
	}
	c.activeMu.Lock()
	cancel, ok := c.activeCancels[runID]
	c.activeMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", domain.ErrRunNotActive, runID)
	}
	cancel()
	return nil
}
