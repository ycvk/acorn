package core

import (
	"context"
	"errors"
	"strconv"
	"time"
)

// ErrTriggerNotDue means a trigger's slot was taken by another scheduler or
// moved since it was read.
var ErrTriggerNotDue = errors.New("trigger not due")

// TriggerStatus is a trigger's health. A failing trigger still fires, after
// a growing wait.
type TriggerStatus string

const (
	TriggerActive  TriggerStatus = "active"
	TriggerFailing TriggerStatus = "failing"
)

// TriggerState is the schedule of one trigger of a skill in use: when it
// fires next and how its last runs went.
type TriggerState struct {
	Skill     string
	Name      string
	Schedule  string
	Status    TriggerStatus
	NextAt    time.Time
	Failures  int
	LastError string
	LastRunAt time.Time
}

// TriggerEventStatus tracks what has been done with an event.
type TriggerEventStatus string

const (
	TriggerEventPending TriggerEventStatus = "pending"
	TriggerEventWoken   TriggerEventStatus = "woken"
	TriggerEventBriefed TriggerEventStatus = "briefed"
)

// TriggerEvent is one line a trigger's command printed. Key is unique per
// skill and trigger.
type TriggerEvent struct {
	ID      int64
	Skill   string
	Trigger string
	Key     string
	Text    string
	Status  TriggerEventStatus
	RunID   string
	SeenAt  time.Time
}

// TriggerSourceID names the memory source of a trigger event.
func TriggerSourceID(eventID int64) string {
	return "trigger:" + strconv.FormatInt(eventID, 10)
}

// TriggerRun is the outcome of one firing: the trigger's state after it and
// the events its command printed.
type TriggerRun struct {
	At        time.Time
	NextAt    time.Time
	Status    TriggerStatus
	Failures  int
	LastError string
	Events    []TriggerEvent
}

// TriggerStore persists the schedules of package triggers and their events.
type TriggerStore interface {
	ListTriggers(ctx context.Context) ([]TriggerState, error)
	// PutTrigger schedules a trigger afresh: active, without failures.
	PutTrigger(ctx context.Context, state TriggerState) error
	RemoveTrigger(ctx context.Context, skill, name string) error
	// ClaimTrigger moves a due trigger from due to next, so the slot fires
	// once; it fails with ErrTriggerNotDue when the trigger is not at due.
	ClaimTrigger(ctx context.Context, skill, name string, due, next time.Time) error
	// RecordTriggerRun stores a firing's outcome and the events whose key is
	// new for the trigger, which it returns with their ids.
	RecordTriggerRun(ctx context.Context, skill, name string, run TriggerRun) ([]TriggerEvent, error)
	ListTriggerEvents(ctx context.Context, status TriggerEventStatus, limit int) ([]TriggerEvent, error)
	MarkTriggerEvents(ctx context.Context, ids []int64, status TriggerEventStatus, runID string) error
	// SkillThread is the thread a skill's trigger runs happen in, empty
	// before the first.
	SkillThread(ctx context.Context, skill string) (string, error)
	SetSkillThread(ctx context.Context, skill, threadID string) error
}
