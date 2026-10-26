// Package wake runs the serve-process scheduler that keeps the agent's
// commitments: it starts a run in the commitment's
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
	StartWakeRun(ctx context.Context, threadID, input string, wake core.ScheduledWake) (runID string, err error)
	// StartRoutineRun starts in threadID or creates a thread with title.
	StartRoutineRun(ctx context.Context, threadID, title, input string, wake core.ScheduledWake) (thread, runID string, err error)
	// StartThreadRun starts a run in threadID; a deleted thread fails with
	// core.ErrSessionNotFound.
	StartThreadRun(ctx context.Context, threadID, input string, wake core.ScheduledWake) (runID string, err error)
}

// WatchChecker fetches a watch and records what it found.
type WatchChecker interface {
	Check(ctx context.Context, w core.Watch) (watch.Result, error)
	// Apply records what a trigger source's trigger printed as a check.
	Apply(ctx context.Context, w core.Watch, fetched watch.Fetched) (watch.Result, error)
}

// Config holds the scheduler's dependencies; every field is required.
type Config struct {
	Routines           core.RoutineStore
	PhoneNotifications core.PhoneNotificationStore
	Store              core.ActivityStore
	Commitments        core.CommitmentStore
	Memory             core.MemoryStore
	Skills             core.SkillStore
	Knowledge          KnowledgeReader
	RunRecords         RunReader
	Events             core.EventAppender
	Runs               RunStarter
	Watches            core.WatchStore
	Checker            WatchChecker
	// News offers the sorted news stories to the briefing; Sorter sorts
	// what is waiting before it.
	News     core.NewsStore
	Sorter   NewsSorter
	Triggers core.TriggerStore
	Clock    func() time.Time
	// Settings gives each tick the owner's time zone, the times of the
	// briefing and the reflections, and the wake budget.
	Settings core.SettingsReader
	// Backups reports when the newest backup snapshot was taken, for the
	// briefing's health check; nil when backups are off.
	Backups  func() (time.Time, error)
	Interval time.Duration
}

// retryDelay is how far a commitment moves when its run could not start.
const retryDelay = 5 * time.Minute

// maxChecksPerTick bounds the watches fetched in one tick.
const maxChecksPerTick = 5

// Scheduler keeps commitments. Tick is safe to call from several processes
// sharing one database: claiming a commitment is a conditional update.
type Scheduler struct {
	cfg      Config
	notifier Notifier
	triggers TriggerRunner
	panels   PanelFeeder

	mu     sync.Mutex
	warned map[string]string // commitment or watch -> local day already warned about the limit
}

func NewScheduler(cfg Config) (*Scheduler, error) {
	switch {
	case cfg.Commitments == nil || cfg.Memory == nil:
		return nil, errors.New("wake scheduler: Commitments and Memory are required")
	case cfg.Store == nil:
		return nil, errors.New("wake scheduler: Store is required")
	case cfg.Skills == nil:
		return nil, errors.New("wake scheduler: Skills is required")
	case cfg.Knowledge == nil || cfg.RunRecords == nil:
		return nil, errors.New("wake scheduler: Knowledge and RunRecords are required")
	case cfg.Routines == nil:
		return nil, errors.New("wake scheduler: Routines is required")
	case cfg.PhoneNotifications == nil:
		return nil, errors.New("wake scheduler: PhoneNotifications is required")
	case cfg.Events == nil:
		return nil, errors.New("wake scheduler: Events is required")
	case cfg.Runs == nil:
		return nil, errors.New("wake scheduler: Runs is required")
	case cfg.Watches == nil:
		return nil, errors.New("wake scheduler: Watches is required")
	case cfg.Checker == nil:
		return nil, errors.New("wake scheduler: Checker is required")
	case cfg.News == nil || cfg.Sorter == nil:
		return nil, errors.New("wake scheduler: News and Sorter are required")
	case cfg.Triggers == nil:
		return nil, errors.New("wake scheduler: Triggers is required")
	case cfg.Clock == nil:
		return nil, errors.New("wake scheduler: Clock is required")
	case cfg.Settings == nil:
		return nil, errors.New("wake scheduler: Settings is required")
	case cfg.Interval <= 0:
		return nil, errors.New("wake scheduler: Interval must be positive")
	}
	return &Scheduler{cfg: cfg, warned: map[string]string{}}, nil
}

// Notifier pushes to the owner, now or when quiet hours end, and sends
// queued notifications whose time has come.
type Notifier interface {
	Notify(ctx context.Context, n core.Notification) (core.Notification, error)
	FlushDue(ctx context.Context) error
}

// WithNotifications makes each finished briefing push its focus and each
// tick send notifications held back by quiet hours. Without push
// notifications nothing is ever pushed or queued.
func (s *Scheduler) WithNotifications(notifier Notifier) *Scheduler {
	s.notifier = notifier
	return s
}

// WithTriggers makes each tick fire the due triggers of skill packages and
// feed their view_data lines to panels. Without a workstation there is
// nothing to run them on.
func (s *Scheduler) WithTriggers(triggers TriggerRunner, panels PanelFeeder) *Scheduler {
	s.triggers, s.panels = triggers, panels
	return s
}

// Run ticks immediately, then every interval until ctx ends. Triggers fire
// in a loop of their own, so a trigger whose command runs long holds up only
// the other triggers. Tick errors are logged; the next tick tries again.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	if s.triggers != nil {
		wg.Go(func() { s.every(ctx, "wake trigger tick", s.tickTriggers) })
	}
	s.every(ctx, "wake scheduler tick", s.tickMain)
	wg.Wait()
}

func (s *Scheduler) every(ctx context.Context, name string, tick func(context.Context) error) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		if err := tick(ctx); err != nil {
			slog.Error(name, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick runs one round of both of Run's loops: it fires due skill package
// triggers, then does the rest of a tick.
func (s *Scheduler) Tick(ctx context.Context) error {
	return errors.Join(s.tickTriggers(ctx), s.tickMain(ctx))
}

// tickTriggers fires the due triggers of skill packages.
func (s *Scheduler) tickTriggers(ctx context.Context) error {
	if s.triggers == nil {
		return nil
	}
	settings, err := s.cfg.Settings.Settings(ctx)
	if err != nil {
		return err
	}
	return s.fireTriggers(ctx, s.cfg.Clock(), settings)
}

// tickMain wakes every due commitment within the daily limit, checks due
// watches, records what finished briefings picked and pushes their focus,
// starts the morning briefing when its time has come, then sends
// notifications whose quiet hours ended.
// Errors are joined; one failing commitment does not stop the others.
func (s *Scheduler) tickMain(ctx context.Context) error {
	settings, err := s.cfg.Settings.Settings(ctx)
	if err != nil {
		return err
	}
	now := s.cfg.Clock()
	var errs []error
	if err := s.cfg.Commitments.RecoverCommitmentClaims(ctx, now); err != nil {
		return err
	}
	due, err := s.cfg.Commitments.DueCommitments(ctx, now)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("list due commitments: %w", err))...)
	}
	for _, item := range due {
		if err := s.wake(ctx, item, now, settings); err != nil {
			errs = append(errs, fmt.Errorf("commitment #%d: %w", item.ID, err))
		}
	}
	if err := s.checkWatches(ctx, now, settings); err != nil {
		errs = append(errs, err)
	}
	if err := s.recordBriefingPicks(ctx, now); err != nil {
		errs = append(errs, fmt.Errorf("record briefing picks: %w", err))
	}
	if err := s.runRoutines(ctx, now, settings); err != nil {
		errs = append(errs, fmt.Errorf("routines: %w", err))
	}
	if _, err := s.cfg.PhoneNotifications.PrunePhoneNotifications(ctx, now.Add(-7*24*time.Hour)); err != nil {
		errs = append(errs, err)
	}
	if _, err := s.cfg.News.PruneNews(ctx, now.Add(-core.NewsRetention)); err != nil {
		errs = append(errs, err)
	}
	if err := s.cfg.News.PruneNewsReactions(ctx, now.Add(-core.NewsReactionRetention)); err != nil {
		errs = append(errs, err)
	}
	if s.notifier != nil {
		if err := s.notifier.FlushDue(ctx); err != nil {
			errs = append(errs, fmt.Errorf("flush notifications: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) wake(ctx context.Context, item core.Commitment, now time.Time, settings core.Settings) error {
	allowed, err := s.withinBudget(ctx, fmt.Sprintf("commitment #%d", item.ID), now, settings)
	if err != nil || !allowed {
		return err
	}
	var next time.Time
	if strings.TrimSpace(item.Recurrence) != "" {
		schedule, err := presence.ParseCron(item.Recurrence)
		if err != nil {
			return fmt.Errorf("recurrence: %w", err)
		}
		next, err = schedule.Next(now.In(settings.Location))
		if err != nil {
			return fmt.Errorf("recurrence: %w", err)
		}
	}
	concern, err := s.linkedConcern(ctx, item.ConcernID)
	if err != nil {
		return err
	}
	occurrence, err := s.cfg.Commitments.ClaimCommitment(ctx, item.ID, now)
	if errors.Is(err, core.ErrCommitmentNotDue) {
		return nil
	}
	if err != nil {
		return err
	}
	wake := fmt.Sprintf("commitment #%d: %s", item.ID, item.Content)
	input := fmt.Sprintf("[commitment #%d, made %s] %s",
		item.ID, item.CreatedAt.In(settings.Location).Format("2006-01-02 15:04"), item.Content)
	if concern != nil {
		input += "\nConcern " + concernText(*concern)
	}
	runID, err := s.cfg.Runs.StartWakeRun(ctx, item.SessionID, input, core.ScheduledWake{Reason: wake, Autonomous: true, Commitment: &core.CommitmentWake{Occurrence: occurrence, Next: next}})
	if err != nil {
		return s.retryLater(ctx, occurrence, now, err)
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, core.EventWakeFired, map[string]any{"commitment_id": item.ID, "occurrence_id": occurrence.ID}); err != nil {
		return fmt.Errorf("record wake of run %s: %w", runID, err)
	}
	return nil
}

// linkedConcern is the concern a commitment advances; nil when it has none or
// the concern was forgotten with its source or conversation.
func (s *Scheduler) linkedConcern(ctx context.Context, id string) (*core.MemoryConcern, error) {
	if id == "" {
		return nil, nil
	}
	concerns, err := s.cfg.Memory.ListConcerns(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("list concerns: %w", err)
	}
	for _, c := range concerns {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, nil
}

// withinBudget reports whether another wake fits today's limit and warns
// once a day per subject when it does not.
func (s *Scheduler) withinBudget(ctx context.Context, subject string, now time.Time, settings core.Settings) (bool, error) {
	local := now.In(settings.Location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, settings.Location)
	count, err := s.cfg.Store.CountWakesSince(ctx, midnight)
	if err != nil {
		return false, err
	}
	tokens, err := s.cfg.Store.SumAutonomousTokensSince(ctx, midnight)
	if err != nil {
		return false, err
	}
	if count < settings.WakeDailyLimit && (settings.WakeDailyTokens == 0 || tokens < settings.WakeDailyTokens) {
		return true, nil
	}
	day := local.Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warned[subject] != day {
		s.warned[subject] = day
		reason := "count"
		if settings.WakeDailyTokens > 0 && tokens >= settings.WakeDailyTokens {
			reason = "tokens"
		}
		slog.Warn("wake deferred: daily budget reached", "subject", subject, "reason", reason, "count", count, "limit", settings.WakeDailyLimit, "tokens", tokens, "token_limit", settings.WakeDailyTokens, "day", day)
	}
	return false, nil
}

// retryLater records the failed occurrence and moves the appointment forward.
func (s *Scheduler) retryLater(ctx context.Context, occurrence core.CommitmentOccurrence, now time.Time, cause error) error {
	next := now.Add(retryDelay)
	if err := s.cfg.Commitments.RetryCommitment(ctx, occurrence, next, now); err != nil {
		return errors.Join(fmt.Errorf("start wake run: %w", cause), fmt.Errorf("reschedule: %w", err))
	}
	return fmt.Errorf("start wake run (retrying at %s): %w", next.Format(time.RFC3339), cause)
}
