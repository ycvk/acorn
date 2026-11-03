package core

import (
	"context"
	"errors"
	"time"
)

// Verdicts the delegating run gives a sub-run's report.
const (
	SubRunAccepted = "accepted"
	SubRunRedo     = "redo"
)

var (
	// ErrSubRunLimit means the run has delegated as many sub-runs as it may.
	ErrSubRunLimit = errors.New("sub-run limit reached")
	// ErrSubRunNotFound means the run has no such task, or the run is not a
	// sub-run.
	ErrSubRunNotFound = errors.New("sub-run not found")
	// ErrSubRunRunning means the task's latest attempt has not reported yet.
	ErrSubRunRunning = errors.New("sub-run has not reported")
	// ErrSubRunReviewed means the task's latest attempt already has a verdict.
	ErrSubRunReviewed = errors.New("sub-run already reviewed")
)

// SubRunBrief is the work a run hands a sub-run: all the sub-run knows of
// it. Block names tools the sub-run may not use, on top of the owner's
// approval.block.
type SubRunBrief struct {
	Objective    string   `json:"objective"`
	Deliverables []string `json:"deliverables,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`
	Acceptance   []string `json:"acceptance,omitempty"`
	Block        []string `json:"block,omitempty"`
}

// SubRunSubmission is what a sub-run hands back: the result, the evidence
// that it meets the brief, and what it could not do.
type SubRunSubmission struct {
	Summary     string   `json:"summary"`
	Evidence    []string `json:"evidence"`
	Limitations []string `json:"limitations"`
}

// SubRun is one attempt at a task a run delegated. Attempts of a task share
// its number; a redo verdict on one starts the next. Status, Output and
// Error are those of the sub-run's own run. Delivered says the report
// reached the delegating run.
type SubRun struct {
	RunID       string
	ParentRunID string
	Task        int
	Attempt     int
	Brief       SubRunBrief
	Submission  *SubRunSubmission
	Verdict     string
	Feedback    string
	Delivered   bool
	Status      RunStatus
	Output      string
	Error       string
	CreatedAt   time.Time
	FinishedAt  time.Time
}

// Reported says the sub-run has ended.
func (s SubRun) Reported() bool {
	return s.Status != RunStatusRunning
}

// SubRunStart records a sub-run with its run. Task 0 starts a new task;
// otherwise it is the next attempt of Task, whose latest attempt gets the
// redo verdict with Feedback. Input is what the sub-run starts from. A run
// that has delegated Limit attempts gets ErrSubRunLimit.
type SubRunStart struct {
	RunID       string
	ParentRunID string
	Task        int
	Brief       SubRunBrief
	Input       string
	Feedback    string
	Limit       int
	CreatedAt   time.Time
}

type SubRunStore interface {
	// StartSubRun creates the sub-run's run in the parent's conversation,
	// running in this process, and its record.
	StartSubRun(context.Context, SubRunStart) (SubRun, error)
	// AcceptSubRun gives the latest attempt of a task the accepted verdict.
	// It fails with ErrSubRunRunning before the attempt reported and with
	// ErrSubRunReviewed once it has a verdict.
	AcceptSubRun(ctx context.Context, parentRunID string, task int) (SubRun, error)
	// SubmitSubRun records what a running sub-run hands back, replacing an
	// earlier submission.
	SubmitSubRun(ctx context.Context, runID string, submission SubRunSubmission) error
	// SubRuns lists the attempts a run delegated, by task and attempt.
	SubRuns(ctx context.Context, parentRunID string) ([]SubRun, error)
	// LatestSubRun is the latest attempt of a task.
	LatestSubRun(ctx context.Context, parentRunID string, task int) (SubRun, error)
	// TakeSubRunReports marks the reported attempts the parent has not
	// received as delivered and returns them, oldest first.
	TakeSubRunReports(ctx context.Context, parentRunID string) ([]SubRun, error)
}
