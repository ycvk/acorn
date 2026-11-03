package runtime

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ycvk/acorn/internal/core"
)

// RunController tracks per-run cancellation functions so an in-flight run can
// be interrupted by ID. It also hands out the slots sub-runs run in and tells
// a run waiting for its sub-runs when one ends.
type RunController struct {
	activeMu      sync.Mutex
	activeCancels map[string]*activeRun
	stopped       bool

	subRunSlots chan struct{}
	endedMu     sync.Mutex
	ended       map[string]chan struct{}
}

type activeRun struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
}

func NewRunController() *RunController {
	return &RunController{
		activeCancels: make(map[string]*activeRun),
		subRunSlots:   make(chan struct{}, maxRunningSubRuns),
		ended:         make(map[string]chan struct{}),
	}
}

// acquireSubRunSlot waits until fewer than maxRunningSubRuns sub-runs run.
// The returned function frees the slot.
func (c *RunController) acquireSubRunSlot(ctx context.Context) (func(), error) {
	select {
	case c.subRunSlots <- struct{}{}:
		return func() { <-c.subRunSlots }, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

// subRunEnded is closed when a sub-run of parent ends after the call. A
// waiter takes it before it reads the sub-runs, so no end is missed.
func (c *RunController) subRunEnded(parent string) <-chan struct{} {
	c.endedMu.Lock()
	defer c.endedMu.Unlock()
	ch, ok := c.ended[parent]
	if !ok {
		ch = make(chan struct{})
		c.ended[parent] = ch
	}
	return ch
}

// signalSubRunEnded wakes whoever waits for the sub-runs of parent.
func (c *RunController) signalSubRunEnded(parent string) {
	c.endedMu.Lock()
	defer c.endedMu.Unlock()
	if ch, ok := c.ended[parent]; ok {
		close(ch)
		delete(c.ended, parent)
	}
}

// forgetSubRunSignal drops the signal of a parent that no longer waits.
func (c *RunController) forgetSubRunSignal(parent string) {
	c.endedMu.Lock()
	defer c.endedMu.Unlock()
	delete(c.ended, parent)
}

// Register records cancel as the active cancellation for runID and returns a
// function that removes this registration only. A resumed run registers under
// the same ID while the previous attempt is still unwinding, so the previous
// attempt's cleanup must not drop the newer registration. After Stop it
// refuses with core.ErrShuttingDown.
func (c *RunController) Register(runID string, cancel context.CancelCauseFunc) (func(), error) {
	entry := &activeRun{cancel: cancel, done: make(chan struct{})}
	var once sync.Once
	c.activeMu.Lock()
	if c.stopped {
		c.activeMu.Unlock()
		return nil, fmt.Errorf("%w: run %s", core.ErrShuttingDown, runID)
	}
	c.activeCancels[runID] = entry
	c.activeMu.Unlock()
	return func() {
		c.activeMu.Lock()
		defer c.activeMu.Unlock()
		once.Do(func() { close(entry.done) })
		if c.activeCancels[runID] == entry {
			delete(c.activeCancels, runID)
		}
	}, nil
}

func (c *RunController) Interrupt(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run id", core.ErrRunNotActive)
	}
	c.activeMu.Lock()
	entry, ok := c.activeCancels[runID]
	c.activeMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", core.ErrRunNotActive, runID)
	}
	entry.cancel(nil)
	return nil
}

// active says the run executes in this process. A run is registered before
// it is recorded and stays registered until its end is recorded.
func (c *RunController) active(runID string) bool {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	_, ok := c.activeCancels[runID]
	return ok
}

// Stop refuses new runs, cancels the active ones with core.ErrShuttingDown,
// and waits until each has finished recording its end or ctx ends.
func (c *RunController) Stop(ctx context.Context) error {
	c.activeMu.Lock()
	c.stopped = true
	active := make(map[string]*activeRun, len(c.activeCancels))
	for id, entry := range c.activeCancels {
		active[id] = entry
		entry.cancel(core.ErrShuttingDown)
	}
	c.activeMu.Unlock()
	var unfinished []string
	for id, entry := range active {
		select {
		case <-entry.done:
		case <-ctx.Done():
			select {
			case <-entry.done:
			default:
				unfinished = append(unfinished, id)
			}
		}
	}
	if len(unfinished) > 0 {
		slices.Sort(unfinished)
		return fmt.Errorf("runs %s did not stop: %w", strings.Join(unfinished, ", "), ctx.Err())
	}
	return nil
}

// ForgetBarrier waits until older affected executions have stopped emitting.
// The caller is between model calls and refreshes its own input at the next hook.
func (c *RunController) ForgetBarrier(ctx context.Context, exclusion core.MemoryExclusion, caller string) error {
	c.activeMu.Lock()
	entries := make([]*activeRun, 0)
	for _, id := range exclusion.RunIDs {
		if id != caller {
			if entry, ok := c.activeCancels[id]; ok {
				entries = append(entries, entry)
				entry.cancel(nil)
			}
		}
	}
	c.activeMu.Unlock()
	for _, entry := range entries {
		select {
		case <-entry.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
