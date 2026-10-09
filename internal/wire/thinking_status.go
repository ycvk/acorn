package wire

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// ThinkingStatus is an operator diagnostic; it is independent of the mobile API.
type ThinkingStatus struct {
	Usage  core.UsageReport              `json:"usage"`
	Wakes  int                           `json:"wakes"`
	Phones []core.PhoneNotificationCount `json:"phone_notifications_24h"`
}

func (c *Container) ThinkingStatus(ctx context.Context) (ThinkingStatus, error) {
	var status ThinkingStatus
	loc, err := c.cfg.OwnerLocation()
	if err != nil {
		return status, err
	}
	now := c.clock()
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	status.Usage, err = c.store.UsageReport(ctx, midnight)
	if err != nil {
		return status, err
	}
	status.Wakes, err = c.store.CountWakesSince(ctx, midnight)
	if err != nil {
		return status, err
	}
	status.Phones, err = c.store.PhoneNotificationCounts(ctx, now.Add(-24*time.Hour))
	return status, err
}
