package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrMemoryNotFound      = errors.New("memory not found")
	ErrMemoryConflict      = errors.New("memory revision conflict")
	ErrMemoryExcluded      = errors.New("memory source excluded")
	ErrMemoryLeaseLost     = errors.New("memory job lease lost")
	ErrMemoryIndexMismatch = errors.New("memory index model mismatch")
	ErrMemoryBudget        = errors.New("memory daily token budget exhausted")
	ErrMemoryNotFact       = errors.New("only a fact can be corrected")
	ErrMemoryCredential    = errors.New("memory text contains a credential")
)

// MemoryRequestSource is the kind of source that records a request the owner
// made in the app, outside any conversation. Its body is the request; it is
// evidence for the change it asked for and is never extracted on its own.
const MemoryRequestSource = "request"

// MemorySource identifies a version of a message, event, note or explicit thought.
// Content is resolved from its owning record; Body is persisted only for sources
// that have no other owner, such as an imported entry or an explicit thought.
type MemorySource struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	ObjectID   string    `json:"object_id"`
	Version    string    `json:"version"`
	Speaker    string    `json:"speaker"`
	SessionID  string    `json:"session_id,omitempty"`
	RunID      string    `json:"run_id,omitempty"`
	Content    string    `json:"content"`
	Body       string    `json:"-"`
	RecordedAt time.Time `json:"recorded_at"`
	OccurredAt time.Time `json:"occurred_at,omitempty"`
	Excluded   bool      `json:"excluded"`
	ToolName   string    `json:"tool_name,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
}

type MemoryEvidence struct {
	SourceID string `json:"source_id"`
	Quote    string `json:"quote"`
	Relation string `json:"relation"`
}

type MemoryLink struct {
	FromID   string `json:"from_id"`
	ToID     string `json:"to_id"`
	Relation string `json:"relation"`
}

// MemoryEntityAlias is an explicit identity assertion with its exact evidence.
// Scope distinguishes names shared by different people or projects.
type MemoryEntityAlias struct {
	Canonical string `json:"canonical"`
	Alias     string `json:"alias"`
	Scope     string `json:"scope,omitempty"`
	SourceID  string `json:"source_id"`
	Quote     string `json:"quote"`
}

// MemoryRecord separates validity from attention. A fact does not expire
// because it has not recently been mentioned.
type MemoryRecord struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"`
	Content     string              `json:"content"`
	Scope       string              `json:"scope,omitempty"`
	State       string              `json:"state"`
	Basis       string              `json:"basis"`
	Revision    int64               `json:"revision"`
	ValidFrom   time.Time           `json:"valid_from,omitempty"`
	ValidTo     time.Time           `json:"valid_to,omitempty"`
	RecordedAt  time.Time           `json:"recorded_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	NeedsReview bool                `json:"needs_review"`
	Pinned      bool                `json:"pinned"`
	Entities    []string            `json:"entities"`
	Aliases     []MemoryEntityAlias `json:"aliases,omitempty"`
	Evidence    []MemoryEvidence    `json:"evidence"`
	Parents     []string            `json:"parents"`
	SourceIDs   []string            `json:"source_ids"`
}

type MemoryDraft struct {
	ID        string              `json:"id,omitempty"`
	Kind      string              `json:"kind"`
	Content   string              `json:"content"`
	Scope     string              `json:"scope,omitempty"`
	State     string              `json:"state,omitempty"`
	Entities  []string            `json:"entities,omitempty"`
	Aliases   []MemoryEntityAlias `json:"aliases,omitempty"`
	Evidence  []MemoryEvidence    `json:"evidence,omitempty"`
	Parents   []string            `json:"parents,omitempty"`
	ValidFrom string              `json:"valid_from,omitempty"`
	ValidTo   string              `json:"valid_to,omitempty"`
	Pinned    bool                `json:"pinned,omitempty"`
}

// MemoryChange revises or creates Draft. Supersedes names a fact the draft
// replaces; Merges names a duplicate fact whose evidence the draft, an
// existing fact, absorbs.
type MemoryChange struct {
	Draft              MemoryDraft `json:"draft"`
	ExpectedRevision   int64       `json:"expected_revision"`
	Reason             string      `json:"reason"`
	Supersedes         string      `json:"supersedes,omitempty"`
	SupersedesRevision int64       `json:"supersedes_revision,omitempty"`
	Merges             string      `json:"merges,omitempty"`
	MergesRevision     int64       `json:"merges_revision,omitempty"`
}

// MemoryMutation commits changes. Lease is the job doing the work; Batch holds
// further jobs whose work the same changes complete, finished with Lease.
type MemoryMutation struct {
	Changes    []MemoryChange
	Links      []MemoryLink
	Lease      *MemoryJob
	Batch      []MemoryJob
	Epoch      int64
	Now        time.Time
	More       bool
	NextCursor int
}

type MemoryQuery struct {
	ChangedSince time.Time `json:"-"`
	Kind         string    `json:"kind,omitempty"`
	Query        string    `json:"query"`
	Mode         string    `json:"mode,omitempty"`
	Depth        string    `json:"depth,omitempty"`
	Entities     []string  `json:"entities,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	From         time.Time `json:"from,omitempty"`
	To           time.Time `json:"to,omitempty"`
	AsOf         time.Time `json:"as_of,omitempty"`
	KnownAt      time.Time `json:"known_at,omitempty"`
	MaxTokens    int       `json:"max_tokens,omitempty"`
	Limit        int       `json:"limit,omitempty"`
}

type MemoryHit struct {
	Record  MemoryRecord `json:"record"`
	Score   float64      `json:"score"`
	Reasons []string     `json:"reasons"`
}

type MemoryRecall struct {
	PendingJobs int            `json:"pending_jobs"`
	FailedJobs  int            `json:"failed_jobs"`
	Hits        []MemoryHit    `json:"hits"`
	Sources     []MemorySource `json:"unprocessed_sources"`
	Pending     int            `json:"pending_sources"`
	Epoch       int64          `json:"exclusion_version"`
	Generation  int64          `json:"index_generation"`
	Tokens      int            `json:"tokens"`
}

type MemoryRead struct {
	Record   MemoryRecord   `json:"record"`
	Sources  []MemorySource `json:"sources"`
	Versions []MemoryRecord `json:"versions"`
	Links    []MemoryLink   `json:"links"`
}

type MemoryForget struct {
	RecordIDs       []string  `json:"record_ids"`
	SourceIDs       []string  `json:"source_ids,omitempty"`
	RequestSourceID string    `json:"request_source_id"`
	Reason          string    `json:"reason"`
	Now             time.Time `json:"-"`
}

type MemoryExclusion struct {
	RequestRunID string                   `json:"request_run_id,omitempty"`
	Epoch        int64                    `json:"version"`
	SourceIDs    []string                 `json:"source_ids"`
	RecordIDs    []string                 `json:"record_ids"`
	RunIDs       []string                 `json:"run_ids"`
	Quotes       []string                 `json:"-"`
	Fragments    []MemoryExcludedFragment `json:"-"`
}

// MemoryForgotten reports one forget: the records it excluded, the sources
// it excluded wholly or in part, and the exclusion state after it, which the
// visibility barrier uses to stop runs that read what was excluded.
type MemoryForgotten struct {
	RecordIDs []string
	SourceIDs []string
	Exclusion MemoryExclusion
}

type MemoryExcludedFragment struct {
	SourceID string
	Quote    string
}

// MemoryConcern is a continuing matter. ThreadID is the conversation it was
// created in, fixed for all its revisions. Outcome is what it hands the owner
// when done, empty for a matter that is only followed.
type MemoryConcern struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Outcome   string    `json:"outcome,omitempty"`
	State     string    `json:"state"`
	Reason    string    `json:"reason"`
	SourceID  string    `json:"source_id"`
	ThreadID  string    `json:"thread_id"`
	RecordIDs []string  `json:"record_ids"`
	Revision  int64     `json:"revision"`
	ReviewAt  time.Time `json:"review_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryStore owns atomic memory mutations and persistent processing progress.
type MemoryStore interface {
	MemoryEpoch(context.Context) (int64, error)
	LinkRunInputSources(context.Context, string, []string) error
	SaveMemoryCheckpoint(context.Context, string, []byte, int64, time.Time) error
	MemoryMessages(context.Context, string, int64, int64, bool, int) ([]SessionMessageRecord, int64, error)
	RegisterMemorySource(context.Context, MemorySource) (MemorySource, error)
	// LoadMemorySource reads a source as memory sees it, with credentials
	// redacted; LoadThreadSource reads it as thread history, which keeps them.
	LoadMemorySource(context.Context, string) (MemorySource, error)
	LoadThreadSource(context.Context, string) (MemorySource, error)
	RunMemorySources(context.Context, string) ([]MemorySource, error)
	PendingMemorySources(context.Context, MemoryQuery) ([]MemorySource, int, error)
	CommitMemory(context.Context, MemoryMutation) ([]MemoryRecord, error)
	ReadMemory(context.Context, string) (MemoryRead, error)
	ListMemoryRecords(context.Context, MemoryQuery) ([]MemoryRecord, error)
	MemoryRecordsByIDs(context.Context, []string, MemoryQuery) ([]MemoryRecord, error)
	// MemorySourceFacts lists the current facts whose current revision cites
	// the source as supporting evidence.
	MemorySourceFacts(context.Context, string) ([]MemoryRecord, error)
	SearchMemoryText(context.Context, MemoryQuery) ([]MemoryRecord, error)
	MemoryNeighbors(context.Context, []string, int) ([]MemoryRecord, error)
	ForgetMemory(context.Context, MemoryForget) (MemoryForgotten, error)
	MemoryExclusions(context.Context) (MemoryExclusion, error)
	// ThreadMemorySourceIDs lists the sources of a thread's messages and tool
	// results.
	ThreadMemorySourceIDs(context.Context, string) ([]string, error)
	// KnowledgeMemorySourceIDs lists the sources of every revision of the
	// note at a path.
	KnowledgeMemorySourceIDs(context.Context, string) ([]string, error)
	SaveConcern(context.Context, MemoryConcern, int64) (MemoryConcern, error)
	// ListConcerns leaves out concerns whose source or linked records were
	// forgotten and those created in a deleted conversation.
	ListConcerns(context.Context, bool) ([]MemoryConcern, error)
	ClaimMemoryJob(context.Context, time.Time, time.Duration, string) (*MemoryJob, error)
	ReadyMemoryJobs(context.Context, time.Time, string, int) ([]MemoryJob, error)
	ClaimMemoryJobs(context.Context, []int64, time.Time, time.Duration) ([]MemoryJob, error)
	FinishMemoryJob(context.Context, MemoryJob, string, time.Time) error
	FailMemoryJob(context.Context, MemoryJob, string, time.Time) error
	DeferMemoryJob(context.Context, MemoryJob, string, time.Time) error
	MemoryProcessingStatus(context.Context) (MemoryProcessingStatus, error)
	ConfigureMemoryIndex(context.Context, MemoryIndex, bool) (MemoryIndex, error)
	MemoryIndex(context.Context) (MemoryIndex, error)
	CompleteMemoryIndex(context.Context, int64) error
	SaveMemoryVectors(context.Context, MemoryJob, []MemoryVector, time.Time) error
	MemoryVectorSketches(context.Context, int64, string, int, time.Time) ([]MemoryVectorSketch, error)
	MemoryVectorsByIDs(context.Context, int64, []string, time.Time) ([]MemoryVector, error)
	SaveMemorySnapshotRefs(context.Context, string, string, []string, []string, int64) error
	SaveThreadSummary(context.Context, ThreadSummary) error
	LoadThreadSummary(context.Context, string) (*ThreadSummary, error)
	RecordMemoryUsage(context.Context, MemoryUsage) error
	ReserveMemoryUsage(context.Context, MemoryUsage, int, time.Time) error
	MemoryUsageSince(context.Context, time.Time, string) (int, error)
}

type MemoryForgetResult struct {
	Version   int64    `json:"exclusion_version"`
	RecordIDs []string `json:"excluded_record_ids"`
	SourceIDs []string `json:"affected_source_ids"`
	Scope     string   `json:"scope"`
}
