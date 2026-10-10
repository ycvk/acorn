package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCommitmentNotFound = errors.New("commitment not found")
	ErrCommitmentNotDue   = errors.New("commitment not due")
	// ErrCommitmentEnded means the commitment is already completed or cancelled.
	ErrCommitmentEnded = errors.New("commitment already ended")
)

type Commitment struct {
	ID          int64     `json:"id"`
	Content     string    `json:"content"`
	State       string    `json:"state"`
	SessionID   string    `json:"session_id"`
	SourceRunID string    `json:"source_run_id"`
	ConcernID   string    `json:"concern_id,omitempty"`
	WakeAt      time.Time `json:"wake_at"`
	Recurrence  string    `json:"recurrence,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CommitmentOccurrence struct {
	ID               int64     `json:"id"`
	CommitmentID     int64     `json:"commitment_id"`
	DueAt            time.Time `json:"due_at"`
	State            string    `json:"state"`
	RunID            string    `json:"run_id,omitempty"`
	EvidenceSourceID string    `json:"evidence_source_id,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CommitmentSettlement struct {
	CommitmentID int64
	OccurrenceID int64
	Action       string
	SourceID     string
	RunID        string
	Now          time.Time
}

type ScheduledWake struct {
	Reason     string
	SourceIDs  []string
	Autonomous bool
	Commitment *CommitmentWake
}

type CommitmentWake struct {
	Occurrence CommitmentOccurrence
	Next       time.Time
}

type CommitmentStore interface {
	RecoverCommitmentClaims(context.Context, time.Time) error
	AddCommitment(context.Context, Commitment) (Commitment, error)
	LoadCommitment(context.Context, int64) (Commitment, error)
	ListCommitments(context.Context, bool) ([]Commitment, error)
	DueCommitments(context.Context, time.Time) ([]Commitment, error)
	ClaimCommitment(context.Context, int64, time.Time) (CommitmentOccurrence, error)
	StartCommitmentOccurrence(context.Context, CommitmentOccurrence, string, time.Time, time.Time) error
	RetryCommitment(context.Context, CommitmentOccurrence, time.Time, time.Time) error
	SettleCommitment(context.Context, CommitmentSettlement) error
	ListDueOccurrences(context.Context) ([]CommitmentOccurrence, error)
}

// ActivityStore serves scheduling budgets and exact model-input snapshots.
type ActivityStore interface {
	SaveContextSnapshot(context.Context, string, string) error
	CountWakesSince(context.Context, time.Time) (int, error)
	SumAutonomousTokensSince(context.Context, time.Time) (int, error)
	UsageReport(context.Context, time.Time) (UsageReport, error)
}
