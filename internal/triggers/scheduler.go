// Package triggers implements ambient agent trigger sources. A Trigger
// observes the world (webhook, timer, file watch) and fires events into a
// Scheduler, which starts a new run via a RunCreator.
//
// This is the ambient layer above direct_response: triggers are run-external
// event sources that inject messages, not a new orchestration mode. See
// docs/adr/0001-ambient-agent-direction.md.
package triggers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// RunCreator starts a new run for a trigger fire. It is implemented by
// api.RunService so this package avoids a hard dependency on internal/api.
type RunCreator interface {
	CreateRun(ctx context.Context, triggerID, input string) error
}

// FireFunc is the callback a Trigger calls when it observes an event worth
// waking the agent for. The scheduler provides it to Start.
type FireFunc func(ctx context.Context, triggerID, input string)

// Trigger is an ambient event source. Start launches the observer (HTTP
// listener, timer, etc.) and calls handler on fire. Stop tears it down.
type Trigger interface {
	ID() string
	Start(ctx context.Context, handler FireFunc) error
	Stop()
}

// Scheduler owns a set of Triggers and routes fires to a RunCreator.
type Scheduler struct {
	creator  RunCreator
	triggers []Trigger
	mu       sync.Mutex
	started  bool
	logger   *slog.Logger
	debounce time.Duration
	pending  map[string]*pendingFire // triggerID -> pending fire (nil map = no debounce)
}

// pendingFire holds a coalesced fire waiting for the debounce window to expire.
type pendingFire struct {
	input string
	timer *time.Timer
}

// SchedulerOption configures a Scheduler at construction time.
type SchedulerOption func(*Scheduler)

// WithDebounce sets the debounce window: multiple fires of the same trigger
// within this duration are coalesced into a single run (last input wins).
// Zero or negative disables debounce (default).
func WithDebounce(d time.Duration) SchedulerOption {
	return func(s *Scheduler) {
		if d > 0 {
			s.debounce = d
			s.pending = make(map[string]*pendingFire)
		}
	}
}

// NewScheduler constructs a Scheduler that routes trigger fires to creator.
func NewScheduler(creator RunCreator, opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{
		creator: creator,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register adds a trigger to the scheduler. Must be called before Start.
func (s *Scheduler) Register(t Trigger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggers = append(s.triggers, t)
}

// Start launches all registered triggers. Each trigger receives a FireFunc
// that routes to the RunCreator.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	s.started = true
	triggers := make([]Trigger, len(s.triggers))
	copy(triggers, s.triggers)
	s.mu.Unlock()

	handler := s.fireHandler(ctx)
	for _, t := range triggers {
		if err := t.Start(ctx, handler); err != nil {
			s.logger.Error("trigger start failed", "trigger", t.ID(), "error", err)
		}
	}
	return nil
}

// Stop tears down all triggers and cancels any pending debounce timers.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	s.started = false
	triggers := make([]Trigger, len(s.triggers))
	copy(triggers, s.triggers)
	// Cancel pending debounce timers so they don't fire after shutdown.
	for triggerID, p := range s.pending {
		if p.timer != nil {
			p.timer.Stop()
		}
		delete(s.pending, triggerID)
	}
	s.mu.Unlock()

	for _, t := range triggers {
		t.Stop()
	}
}

// Fire is the FireFunc handed to triggers. It routes to the RunCreator.
// Unknown trigger IDs are silently ignored. If debounce is configured,
// rapid fires of the same trigger within the window are coalesced (last
// input wins).
func (s *Scheduler) Fire(ctx context.Context, triggerID, input string) {
	s.mu.Lock()
	triggers := make([]Trigger, len(s.triggers))
	copy(triggers, s.triggers)
	s.mu.Unlock()

	known := false
	for _, t := range triggers {
		if t.ID() == triggerID {
			known = true
			break
		}
	}
	if !known {
		s.logger.Warn("fire from unknown trigger, ignoring", "trigger_id", triggerID)
		return
	}

	// With debounce: coalesce rapid fires via a per-trigger timer.
	if s.debounce > 0 {
		s.scheduleDebounced(ctx, triggerID, input)
		return
	}

	// No debounce: fire immediately.
	go s.createRun(ctx, triggerID, input)
}

// scheduleDebounced records a pending fire. If no timer is running for this
// trigger, one is started. Subsequent fires within the window replace the
// input and reset the timer (last input wins).
func (s *Scheduler) scheduleDebounced(ctx context.Context, triggerID, input string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.pending[triggerID]; ok && p.timer != nil {
		// Already pending: replace input, reset timer.
		p.input = input
		p.timer.Reset(s.debounce)
		return
	}

	p := &pendingFire{input: input}
	p.timer = time.AfterFunc(s.debounce, func() {
		s.mu.Lock()
		delete(s.pending, triggerID)
		s.mu.Unlock()
		go s.createRun(ctx, triggerID, p.input)
	})
	s.pending[triggerID] = p
}

// createRun detaches from the HTTP context and calls CreateRun.
func (s *Scheduler) createRun(ctx context.Context, triggerID, input string) {
	runCtx := context.WithoutCancel(ctx)
	if err := s.creator.CreateRun(runCtx, triggerID, input); err != nil {
		s.logger.Error("trigger fire run creation failed", "trigger_id", triggerID, "error", err)
	}
}

// HandleWebhook routes an incoming HTTP request to the webhook trigger with
// the given ID. Returns an error if the trigger does not exist or is not a
// webhook trigger.
func (s *Scheduler) HandleWebhook(ctx context.Context, triggerID string, r *http.Request) error {
	s.mu.Lock()
	for _, t := range s.triggers {
		if t.ID() == triggerID {
			s.mu.Unlock()
			if wt, ok := t.(*WebhookTrigger); ok {
				return wt.HandleWebhook(ctx, r)
			}
			return errors.New("trigger is not a webhook trigger")
		}
	}
	s.mu.Unlock()
	return &TriggerNotFoundError{ID: triggerID}
}

// TriggerNotFoundError is returned by HandleWebhook when no trigger with the
// given ID is registered. The API layer maps it to HTTP 404.
type TriggerNotFoundError struct{ ID string }

func (e *TriggerNotFoundError) Error() string { return "trigger not found: " + e.ID }

// fireHandler returns the FireFunc that triggers call when they observe events.
func (s *Scheduler) fireHandler(ctx context.Context) FireFunc {
	return func(_ context.Context, triggerID, input string) {
		s.Fire(ctx, triggerID, input)
	}
}
