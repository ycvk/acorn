package core

import (
	"context"
	"errors"
	"time"
)

// Follow-up states. The wake scheduler checks a waiting follow-up each tick;
// a claimed one is starting its run, a fired one started it.
const (
	FollowUpWaiting   = "waiting"
	FollowUpClaimed   = "claimed"
	FollowUpFired     = "fired"
	FollowUpCancelled = "cancelled"
)

var (
	ErrFollowUpNotFound = errors.New("follow-up not found")
	// ErrFollowUpNotWaiting means the follow-up fired, is firing or was
	// cancelled.
	ErrFollowUpNotWaiting = errors.New("follow-up is not waiting")
)

// FollowUp wakes the agent in its conversation once something on the
// workstation happened: the job JobID ended, or printed OutputContains, or the
// file at Path is ready. At DeadlineAt it wakes the agent regardless.
type FollowUp struct {
	ID             int64
	SessionID      string
	SourceRunID    string
	Task           string
	JobID          string
	OutputContains string
	Path           string
	DeadlineAt     time.Time
	State          string
	// ScannedOffset is how far the job's output was searched for
	// OutputContains.
	ScannedOffset int64
	// PathSize is the size of Path at the previous check; nil while it was
	// missing.
	PathSize *int64
	// Reason says what woke the run.
	Reason    string
	RunID     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type FollowUpStore interface {
	AddFollowUp(context.Context, FollowUp) (FollowUp, error)
	// WaitingFollowUps lists the waiting follow-ups of sessionID, or of
	// every conversation when it is empty, earliest deadline first.
	WaitingFollowUps(ctx context.Context, sessionID string) ([]FollowUp, error)
	// RecordFollowUpCheck keeps what a check saw for the next one.
	RecordFollowUpCheck(ctx context.Context, id, scannedOffset int64, pathSize *int64, now time.Time) error
	// ClaimFollowUp takes a waiting follow-up to start its run.
	ClaimFollowUp(ctx context.Context, id int64, reason string, now time.Time) error
	// StartFollowUp records the run a claimed follow-up started.
	StartFollowUp(ctx context.Context, id int64, runID string, now time.Time) error
	// ReleaseFollowUp returns a claimed follow-up whose run did not start.
	ReleaseFollowUp(ctx context.Context, id int64, now time.Time) error
	// RecoverFollowUpClaims returns the follow-ups claimed more than five
	// minutes ago without a run.
	RecoverFollowUpClaims(ctx context.Context, now time.Time) error
	// CancelFollowUp cancels a waiting follow-up of sessionID, or of any
	// conversation when it is empty.
	CancelFollowUp(ctx context.Context, sessionID string, id int64, now time.Time) error
}
