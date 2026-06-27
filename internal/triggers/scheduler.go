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
}

// NewScheduler constructs a Scheduler that routes trigger fires to creator.
func NewScheduler(creator RunCreator) *Scheduler {
	return &Scheduler{
		creator: creator,
		logger:  slog.Default(),
	}
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

// Stop tears down all triggers.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	s.started = false
	triggers := make([]Trigger, len(s.triggers))
	copy(triggers, s.triggers)
	s.mu.Unlock()

	for _, t := range triggers {
		t.Stop()
	}
}

// Fire is the FireFunc handed to triggers. It routes to the RunCreator.
// Unknown trigger IDs are silently ignored.
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

	go func() {
		// Detach from the HTTP request context: the run must outlive the
		// webhook response. WithoutCancel prevents the run from being
		// cancelled when the HTTP handler returns.
		runCtx := context.WithoutCancel(ctx)
		if err := s.creator.CreateRun(runCtx, triggerID, input); err != nil {
			s.logger.Error("trigger fire run creation failed", "trigger_id", triggerID, "error", err)
		}
	}()
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
