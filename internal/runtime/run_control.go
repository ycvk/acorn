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
		return fmt.Errorf("%w: empty run id", core.ErrRunNotActive)
	}
	c.activeMu.Lock()
	cancel, ok := c.activeCancels[runID]
	c.activeMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", core.ErrRunNotActive, runID)
	}
	cancel()
	return nil
}
