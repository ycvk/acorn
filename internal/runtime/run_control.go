package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var ErrRunNotActive = errors.New("run not active")

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
		return errors.New("run controller is nil")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("%w: empty run id", ErrRunNotActive)
	}
	c.activeMu.Lock()
	cancel, ok := c.activeCancels[runID]
	c.activeMu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrRunNotActive, runID)
	}
	cancel()
	return nil
}

// InterruptTree interrupts a run and all its children in the execution tree.
// Children in finalization phase are allowed to complete.
func (c *RunController) InterruptTree(runID string, registry *runRegistry) {
	if c == nil || registry == nil {
		return
	}
	c.activeMu.Lock()
	cancelFuncs := make(map[string]context.CancelFunc, len(c.activeCancels))
	for id, cancel := range c.activeCancels {
		cancelFuncs[id] = cancel
	}
	c.activeMu.Unlock()

	registry.InterruptTree(runID, cancelFuncs)
}
