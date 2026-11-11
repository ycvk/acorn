package wire

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
)

// ThinkingStatus is an operator diagnostic; it is independent of the mobile API.
type ThinkingStatus struct {
	Usage core.UsageReport `json:"usage"`
	Wakes int              `json:"wakes"`
}

func (c *Container) ThinkingStatus(ctx context.Context) (ThinkingStatus, error) {
	var status ThinkingStatus
	current, err := c.settings.Settings(ctx)
	if err != nil {
		return status, err
	}
	loc := current.Location
	now := c.clock()
	midnight := core.Midnight(now, loc)
	status.Usage, err = c.store.UsageReport(ctx, midnight)
	if err != nil {
		return status, err
	}
	status.Wakes, err = c.store.CountWakesSince(ctx, midnight)
	return status, err
}
