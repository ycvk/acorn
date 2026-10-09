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
	"github.com/ycvk/acorn/internal/watch"
)

// RunStarter starts the runs the scheduler wakes.
type RunStarter interface {
	// StartWakeRun starts a run in threadID with input as the run input and
	// wake as the description of what woke it. When the thread no longer
	// exists the run starts in a reminders thread.
	StartWakeRun(ctx context.Context, threadID, wake, input string) (runID string, err error)
	// StartRoutineRun starts in threadID or creates a thread with title.
	StartRoutineRun(ctx context.Context, threadID, title, wake, input string) (thread, runID string, err error)
}

// WatchChecker fetches a watch and records what it found.
type WatchChecker interface {
	Check(ctx context.Context, w core.Watch) (watch.Result, error)
}

// Briefing schedules the morning briefing at At after owner-local midnight.
type Briefing struct {
	Enabled bool
	At      time.Duration
}

// Config holds the scheduler's dependencies; every field is required.
type Config struct {
	Routines           core.RoutineStore
	PhoneNotifications core.PhoneNotificationStore
	Store              core.PresenceStore
	Events             core.EventAppender
	Runs               RunStarter
	Watches            core.WatchStore
	Checker            WatchChecker
	Clock              func() time.Time
	Location           *time.Location
	// DailyLimit caps commitment and watch wakes per owner-local day; zero
	// disables them.
	DailyLimit  int
	DailyTokens int
	Thinking    Thinking
	// MaxChecksPerTick bounds the watches fetched in one tick.
	MaxChecksPerTick int
	Briefing         Briefing
	Interval         time.Duration
}

// retryDelay is how far a commitment moves when its run could not start.
const retryDelay = 5 * time.Minute

// Scheduler keeps commitments. Tick is safe to call from several processes
// sharing one database: claiming a commitment is a conditional update.
type Scheduler struct {
	cfg           Config
	notifications NotificationFlusher

	mu     sync.Mutex
	warned map[string]string // commitment or watch -> local day already warned about the limit
}

func NewScheduler(cfg Config) (*Scheduler, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("wake scheduler: Store is required")
	case cfg.Routines == nil:
		return nil, errors.New("wake scheduler: Routines is required")
	case cfg.PhoneNotifications == nil:
		return nil, errors.New("wake scheduler: PhoneNotifications is required")
	case cfg.DailyTokens < 1:
		return nil, errors.New("wake scheduler: DailyTokens must be positive")
	case cfg.Events == nil:
		return nil, errors.New("wake scheduler: Events is required")
	case cfg.Runs == nil:
		return nil, errors.New("wake scheduler: Runs is required")
	case cfg.Watches == nil:
		return nil, errors.New("wake scheduler: Watches is required")
	case cfg.Checker == nil:
		return nil, errors.New("wake scheduler: Checker is required")
	case cfg.MaxChecksPerTick <= 0:
		return nil, errors.New("wake scheduler: MaxChecksPerTick must be positive")
	case cfg.Clock == nil:
		return nil, errors.New("wake scheduler: Clock is required")
	case cfg.Location == nil:
		return nil, errors.New("wake scheduler: Location is required")
	case cfg.Interval <= 0:
		return nil, errors.New("wake scheduler: Interval must be positive")
	case cfg.DailyLimit < 0:
		return nil, errors.New("wake scheduler: DailyLimit must be >= 0")
	}
	return &Scheduler{cfg: cfg, warned: map[string]string{}}, nil
}

// NotificationFlusher sends queued notifications whose time has come.
type NotificationFlusher interface {
	FlushDue(ctx context.Context) error
}

// WithNotifications makes each tick also send notifications held back by
// quiet hours. Without push notifications nothing is ever queued.
func (s *Scheduler) WithNotifications(flusher NotificationFlusher) *Scheduler {
	s.notifications = flusher
	return s
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

// Tick applies decay, wakes every due commitment within the daily limit,
// checks due watches, starts the morning briefing when its time has come, then
// sends notifications whose quiet hours ended.
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
	if err := s.checkWatches(ctx, now); err != nil {
		errs = append(errs, err)
	}
	if err := s.runRoutines(ctx, now); err != nil {
		errs = append(errs, fmt.Errorf("routines: %w", err))
	}
	if _, err := s.cfg.PhoneNotifications.PrunePhoneNotifications(ctx, now.Add(-7*24*time.Hour)); err != nil {
		errs = append(errs, err)
	}
	if s.notifications != nil {
		if err := s.notifications.FlushDue(ctx); err != nil {
			errs = append(errs, fmt.Errorf("flush notifications: %w", err))
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
	allowed, err := s.withinBudget(ctx, fmt.Sprintf("commitment #%d", item.ID), now)
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

// withinBudget reports whether another wake fits today's limit and warns
// once a day per subject when it does not.
func (s *Scheduler) withinBudget(ctx context.Context, subject string, now time.Time) (bool, error) {
	local := now.In(s.cfg.Location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.cfg.Location)
	count, err := s.cfg.Store.CountWakesSince(ctx, midnight)
	if err != nil {
		return false, err
	}
	tokens, err := s.cfg.Store.SumAutonomousTokensSince(ctx, midnight)
	if err != nil {
		return false, err
	}
	if count < s.cfg.DailyLimit && tokens < s.cfg.DailyTokens {
		return true, nil
	}
	day := local.Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warned[subject] != day {
		s.warned[subject] = day
		reason := "count"
		if tokens >= s.cfg.DailyTokens {
			reason = "tokens"
		}
		slog.Warn("wake deferred: daily budget reached", "subject", subject, "reason", reason, "count", count, "limit", s.cfg.DailyLimit, "tokens", tokens, "token_limit", s.cfg.DailyTokens, "day", day)
	}
	return false, nil
}

// retryLater gives the claimed commitment back with a later wake time.
func (s *Scheduler) retryLater(ctx context.Context, item core.MemoryItem, now time.Time, cause error) error {
	item.Status = core.MemoryActive
	item.WakeAt = now.Add(retryDelay)
	item.UpdatedAt = now
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
		CreatedAt:   now,
	}); err != nil {
		return fmt.Errorf("schedule next occurrence: %w", err)
	}
	return nil
}
