// Package wake runs the serve-process scheduler that keeps the agent's
// commitments: it decays working memory and starts a run in the commitment's
// conversation when its wake time arrives.
package wake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
)

// RunStarter starts a run woken by a commitment.
type RunStarter interface {
	// StartWakeRun starts a run in threadID with input as the run input and
	// wake as the description of what woke it. When the thread no longer
	// exists the run starts in a reminders thread.
	StartWakeRun(ctx context.Context, threadID, wake, input string) (runID string, err error)
}

// Config holds the scheduler's dependencies; every field is required.
type Config struct {
	Store    core.PresenceStore
	Events   core.EventAppender
	Runs     RunStarter
	Clock    func() time.Time
	Location *time.Location
	// DailyLimit caps commitment wakes per owner-local day; zero disables them.
	DailyLimit int
	Interval   time.Duration
}

// retryDelay is how far a commitment moves when its run could not start.
const retryDelay = 5 * time.Minute

// Scheduler keeps commitments. Tick is safe to call from several processes
// sharing one database: claiming a commitment is a conditional update.
type Scheduler struct {
	cfg Config

	mu     sync.Mutex
	warned map[int64]string // commitment id -> local day already warned about the limit
}

func NewScheduler(cfg Config) (*Scheduler, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("wake scheduler: Store is required")
	case cfg.Events == nil:
		return nil, errors.New("wake scheduler: Events is required")
	case cfg.Runs == nil:
		return nil, errors.New("wake scheduler: Runs is required")
	case cfg.Clock == nil:
		return nil, errors.New("wake scheduler: Clock is required")
	case cfg.Location == nil:
		return nil, errors.New("wake scheduler: Location is required")
	case cfg.Interval <= 0:
		return nil, errors.New("wake scheduler: Interval must be positive")
	case cfg.DailyLimit < 0:
		return nil, errors.New("wake scheduler: DailyLimit must be >= 0")
	}
	return &Scheduler{cfg: cfg, warned: map[int64]string{}}, nil
}

// Run ticks immediately, then every interval until ctx ends. Tick errors are
// logged; the next tick tries again.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil {
			slog.Error("wake scheduler tick", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick applies decay, then wakes every due commitment within the daily limit.
// Errors are joined; one failing commitment does not stop the others.
func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.cfg.Clock()
	var errs []error
	if err := s.decay(ctx, now); err != nil {
		errs = append(errs, err)
	}
	due, err := s.cfg.Store.ListDueCommitments(ctx, now)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("list due commitments: %w", err))...)
	}
	for _, item := range due {
		if err := s.wake(ctx, item, now); err != nil {
			errs = append(errs, fmt.Errorf("commitment #%d: %w", item.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) decay(ctx context.Context, now time.Time) error {
	items, err := s.cfg.Store.ListMemoryItems(ctx, []core.MemoryStatus{core.MemoryActive, core.MemoryResting})
	if err != nil {
		return fmt.Errorf("list memory for decay: %w", err)
	}
	for _, item := range presence.Decay(items, now) {
		if err := s.cfg.Store.UpdateMemoryItem(ctx, item); err != nil {
			return fmt.Errorf("decay memory item %d: %w", item.ID, err)
		}
	}
	return nil
}

func (s *Scheduler) wake(ctx context.Context, item core.MemoryItem, now time.Time) error {
	allowed, err := s.withinDailyLimit(ctx, item, now)
	if err != nil || !allowed {
		return err
	}
	if err := s.cfg.Store.ClaimDueCommitment(ctx, item.ID, now); err != nil {
		if errors.Is(err, core.ErrMemoryItemNotDue) {
			return nil // another scheduler took it
		}
		return err
	}
	wake := fmt.Sprintf("commitment #%d: %s", item.ID, item.Content)
	input := fmt.Sprintf("[commitment #%d, made %s] %s",
		item.ID, item.CreatedAt.In(s.cfg.Location).Format("2006-01-02 15:04"), item.Content)
	runID, err := s.cfg.Runs.StartWakeRun(ctx, item.SessionID, wake, input)
	if err != nil {
		return s.retryLater(ctx, item, now, err)
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, core.EventWakeFired, map[string]any{"memory_id": item.ID}); err != nil {
		return fmt.Errorf("record wake of run %s: %w", runID, err)
	}
	if strings.TrimSpace(item.Recurrence) != "" {
		return s.scheduleNext(ctx, item, now)
	}
	return nil
}

func (s *Scheduler) withinDailyLimit(ctx context.Context, item core.MemoryItem, now time.Time) (bool, error) {
	local := now.In(s.cfg.Location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.cfg.Location)
	count, err := s.cfg.Store.CountWakesSince(ctx, midnight)
	if err != nil {
		return false, err
	}
	if count < s.cfg.DailyLimit {
		return true, nil
	}
	day := local.Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warned[item.ID] != day {
		s.warned[item.ID] = day
		slog.Warn("commitment wake deferred: daily wake limit reached", "memory_id", item.ID, "limit", s.cfg.DailyLimit, "day", day)
	}
	return false, nil
}

// retryLater gives the claimed commitment back with a later wake time.
func (s *Scheduler) retryLater(ctx context.Context, item core.MemoryItem, now time.Time, cause error) error {
	item.Status = core.MemoryActive
	item.WakeAt = now.Add(retryDelay)
	if err := s.cfg.Store.UpdateMemoryItem(ctx, item); err != nil {
		return errors.Join(fmt.Errorf("start wake run: %w", cause), fmt.Errorf("reschedule: %w", err))
	}
	return fmt.Errorf("start wake run (retrying at %s): %w", item.WakeAt.Format(time.RFC3339), cause)
}

// scheduleNext adds the next occurrence of a recurring commitment. The woken
// item stays for the agent to settle.
func (s *Scheduler) scheduleNext(ctx context.Context, item core.MemoryItem, now time.Time) error {
	schedule, err := presence.ParseCron(item.Recurrence)
	if err != nil {
		return fmt.Errorf("recurrence: %w", err)
	}
	next, err := schedule.Next(now.In(s.cfg.Location))
	if err != nil {
		return fmt.Errorf("recurrence: %w", err)
	}
	if _, err := s.cfg.Store.AddMemoryItem(ctx, core.MemoryItem{
		Kind:        core.MemoryCommitment,
		Content:     item.Content,
		Status:      core.MemoryActive,
		SessionID:   item.SessionID,
		SourceRunID: item.SourceRunID,
		WakeAt:      next.UTC(),
		Recurrence:  item.Recurrence,
	}); err != nil {
		return fmt.Errorf("schedule next occurrence: %w", err)
	}
	return nil
}
