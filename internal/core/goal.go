package core

import (
	"context"
	"errors"
	"time"
)

// Goal states. An active goal continues its conversation after each run;
// done and blocked goals are announced to the owner, stopped ones are not.
const (
	GoalActive  = "active"
	GoalDone    = "done"
	GoalBlocked = "blocked"
	GoalStopped = "stopped"
)

var (
	ErrGoalNotFound = errors.New("no active goal")
	// ErrGoalActive means the conversation already has an active goal.
	ErrGoalActive = errors.New("the conversation already has an active goal")
	// ErrGoalChanged means the goal ended or took a run since it was read.
	ErrGoalChanged = errors.New("goal changed")
)

// Goal is an outcome the agent works toward in a conversation over up to
// MaxRuns runs of its own; Runs counts the runs started for it.
type Goal struct {
	ID          int64
	SessionID   string
	SourceRunID string
	Objective   string
	MaxRuns     int
	Runs        int
	State       string
	// Result says what was achieved, what the agent needs or why it stopped.
	Result    string
	Announced bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GoalStore interface {
	// StartGoal fails with ErrGoalActive while the conversation has one.
	StartGoal(context.Context, Goal) (Goal, error)
	// ActiveGoal fails with ErrGoalNotFound when the conversation has none.
	ActiveGoal(ctx context.Context, sessionID string) (Goal, error)
	ActiveGoals(context.Context) ([]Goal, error)
	// EndGoal ends an active goal; a stopped goal needs no announcement.
	EndGoal(ctx context.Context, id int64, state, result string, now time.Time) (Goal, error)
	// CountGoalRun counts a run for the goal while it is active and has
	// taken runs runs; otherwise it fails with ErrGoalChanged.
	CountGoalRun(ctx context.Context, id int64, runs int, now time.Time) error
	// UnannouncedGoals lists the done and blocked goals not yet announced.
	UnannouncedGoals(context.Context) ([]Goal, error)
	MarkGoalAnnounced(ctx context.Context, id int64, now time.Time) error
}
