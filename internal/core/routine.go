package core

import (
	"context"
	"errors"
	"time"
)

var ErrRoutineTaken = errors.New("routine slot already taken")

// RoutineRun is the run a routine slot started.
type RoutineRun struct {
	Routine   string
	Slot      string
	ThreadID  string
	RunID     string
	CreatedAt time.Time
}

// RoutineOutput is the reply of a routine's newest succeeded run.
type RoutineOutput struct {
	Slot       string
	ThreadID   string
	RunID      string
	Text       string
	FinishedAt time.Time
}

// RoutineStore persists owner-local slots for briefings and idle thinking.
type RoutineStore interface {
	ClaimRoutine(ctx context.Context, routine, slot string, at time.Time) error
	ReleaseRoutine(ctx context.Context, routine, slot string) error
	SetRoutineRun(ctx context.Context, routine, slot, threadID, runID string) error
	// LatestRoutineThread is the thread of routine's newest started run;
	// empty when none.
	LatestRoutineThread(ctx context.Context, routine string) (string, error)
	// LatestRoutineRun is the newest started run of routine; zero when none.
	LatestRoutineRun(ctx context.Context, routine string) (RoutineRun, error)
	// LatestRoutineOutput is the reply of routine's newest succeeded run;
	// zero when none succeeded or its thread was deleted with the run.
	LatestRoutineOutput(ctx context.Context, routine string) (RoutineOutput, error)
	// LastRoutineAt excludes skipped slots and the current slot.
	LastRoutineAt(ctx context.Context, routine, beforeSlot string) (time.Time, error)
}

const EventModelUsage = "model.usage"

// UsageReport counts provider-reported usage by the model call's event time.
type UsageReport struct {
	AutonomousTokens int `json:"autonomous_tokens"`
	TotalTokens      int `json:"total_tokens"`
	UnreportedCalls  int `json:"unreported_calls"`
}
