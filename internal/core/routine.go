package core

import (
	"context"
	"errors"
	"time"
)

var ErrRoutineTaken = errors.New("routine slot already taken")

// RoutineStore persists owner-local slots for briefings and idle thinking.
type RoutineStore interface {
	ClaimRoutine(ctx context.Context, routine, slot string, at time.Time) error
	ReleaseRoutine(ctx context.Context, routine, slot string) error
	SetRoutineRun(ctx context.Context, routine, slot, threadID, runID string) error
	LatestRoutineThread(ctx context.Context, routines ...string) (string, error)
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
