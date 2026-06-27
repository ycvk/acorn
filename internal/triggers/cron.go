package triggers

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// CronConfig configures a single cron trigger.
type CronConfig struct {
	ID       string
	Schedule string // 5-field cron expression: "min hour dom month dow"
	Prompt   string // input injected into the run when the trigger fires
}

// CronTrigger is a Trigger that fires on a cron schedule. It owns a goroutine
// that sleeps until the next matching time and fires. Unlike webhook, cron
// is self-contained: no HTTP listener needed.
type CronTrigger struct {
	cfg      CronConfig
	schedule *cronSchedule
	handler  FireFunc
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
}

// NewCronTrigger constructs a cron trigger from config. The schedule is
// parsed eagerly so a malformed expression fails at construction, not at
// fire time.
func NewCronTrigger(cfg CronConfig) (*CronTrigger, error) {
	s, err := parseCron(cfg.Schedule)
	if err != nil {
		return nil, err
	}
	return &CronTrigger{cfg: cfg, schedule: s, done: make(chan struct{})}, nil
}

func (c *CronTrigger) ID() string { return c.cfg.ID }

// Start launches the cron goroutine. It computes the next fire time, sleeps
// until then, fires, and repeats. The goroutine exits when Stop is called
// or the context is cancelled.
func (c *CronTrigger) Start(ctx context.Context, handler FireFunc) error {
	c.mu.Lock()
	c.handler = handler
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()

	go c.loop(runCtx)
	return nil
}

func (c *CronTrigger) loop(ctx context.Context) {
	defer close(c.done)
	for {
		now := time.Now()
		next := c.schedule.next(now)
		if next.IsZero() {
			slog.Warn("cron trigger has no future fire time, stopping", "id", c.cfg.ID)
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			c.mu.Lock()
			handler := c.handler
			c.mu.Unlock()
			if handler != nil {
				handler(ctx, c.cfg.ID, c.cfg.Prompt)
			}
		}
	}
}

// Stop signals the cron goroutine to exit and waits for it to drain.
func (c *CronTrigger) Stop() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.mu.Unlock()
	<-c.done
}
