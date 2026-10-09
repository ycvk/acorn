package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ycvk/acorn/internal/core"
)

// RunController tracks per-run cancellation functions so an in-flight run can
// be interrupted by ID.
type RunController struct {
	activeMu      sync.Mutex
	activeCancels map[string]*activeRun
}

type activeRun struct {
	cancel context.CancelFunc
}

func NewRunController() *RunController {
	return &RunController{activeCancels: make(map[string]*activeRun)}
}

// Register records cancel as the active cancellation for runID and returns a
// function that removes this registration only. A resumed run registers under
// the same ID while the previous attempt is still unwinding, so the previous
// attempt's cleanup must not drop the newer registration.
func (c *RunController) Register(runID string, cancel context.CancelFunc) func() {
	entry := &activeRun{cancel: cancel}
	c.activeMu.Lock()
	c.activeCancels[runID] = entry
	c.activeMu.Unlock()
	return func() {
		c.activeMu.Lock()
		defer c.activeMu.Unlock()
		if c.activeCancels[runID] == entry {
			delete(c.activeCancels, runID)
		}
	}
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
	entry.cancel()
	return nil
}
