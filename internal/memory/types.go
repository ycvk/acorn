package memory

import (
	"context"
	"sync"
	"time"
)

const (
	defaultMaxNudges   = 5
	defaultMaxEntries  = 2
	defaultSearchLimit = 10
	maxSearchLimit     = 50
	maxPrepareNudges   = 25
	maxPrepareEntries  = 20
)

type Kind string

const (
	KindFact    Kind = "fact"
	KindSkill   Kind = "skill"
	KindHistory Kind = "history"
)

type Status string

const (
	StatusUnverified Status = "unverified"
	StatusVerified   Status = "verified"
	StatusRetired    Status = "retired"
)

type Service interface {
	Root() string
	ListFacts(ctx context.Context, selection RecordSelection) ([]Record, error)
	ListSkills(ctx context.Context, selection RecordSelection) ([]Record, error)
	ListHistory(ctx context.Context, selection RecordSelection) ([]Record, error)
	Prepare(ctx context.Context, req PrepareRequest) (*PrepareResult, error)
	Search(ctx context.Context, req SearchRequest) (*SearchResult, error)
	AppendHistory(ctx context.Context, event HistoryEvent) error
	PlanMemoryMutation(ctx context.Context, req PlanMemoryMutationRequest) (*MemoryMutationPlan, error)
	ApplyMemoryMutation(ctx context.Context, req PlanMemoryMutationRequest) (*MemoryMutationResult, error)
	CreateFact(ctx context.Context, req CreateFactRequest) (*Record, error)
	BuildMemoryInstruction(ctx context.Context, workspaceSlug string) (string, error)
}

// Config holds the parameters for constructing a LocalService. Embedding is
// optional: a nil EmbeddingClient disables semantic retrieval (search falls
// back to keyword-only).
type Config struct {
	Root      string
	Embedding *EmbeddingClient
}

type LocalService struct {
	root      string
	mu        sync.RWMutex
	index     *MemoryIndex
	embedding *EmbeddingClient
	vectors   *VectorIndex
}

type PrepareRequest struct {
	RunID           string
	SessionID       string
	WorkspaceSlug   string
	UserInput       string
	Mode            string
	MaxNudges       int
	MaxEntries      int
	Explain         bool
	ActiveCharLimit int
}

type PrepareResult struct {
	Nudges      []Nudge
	Entries     []Entry
	SkillTree   *SkillTreeIndex
	Explain     *SearchExplain
	ActiveFacts []Entry // non-retired user-scoped facts injected as a frozen snapshot, independent of query matching
}

type Nudge struct {
	Ref    string
	Kind   string
	Title  string
	Status string
	Reason string
}

type Entry struct {
	Ref     string
	Kind    string
	Title   string
	Content string
}

// CreateFactRequest is the minimal structured input for writing a fact. The
// backend generates Record V2 frontmatter and auto-stamps created/updated/status/
// scope, so callers never hand-author YAML, dates, or status. Tags are optional;
// an empty Scope defaults to "user".
type CreateFactRequest struct {
	Title string
	Body  string
	Tags  []string
	Scope string
}

type MemoryMutationAction string

const (
	MemoryMutationCreate          MemoryMutationAction = "create"
	MemoryMutationReplaceExisting MemoryMutationAction = "replace_existing"
	MemoryMutationRetireExisting  MemoryMutationAction = "retire_existing"
	MemoryMutationNoopDuplicate   MemoryMutationAction = "noop_duplicate"
	MemoryMutationRejectInvalid   MemoryMutationAction = "reject_invalid"
)

type PlanMemoryMutationRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type MemoryMutationPlan struct {
	Action      MemoryMutationAction `json:"action"`
	Path        string               `json:"path"`
	Ref         string               `json:"ref,omitempty"`
	ExistingRef string               `json:"existing_ref,omitempty"`
	Kind        Kind                 `json:"kind,omitempty"`
	Reason      string               `json:"reason,omitempty"`
}

type MemoryMutationResult struct {
	Message               string              `json:"message"`
	MutationPlan          *MemoryMutationPlan `json:"mutation_plan"`
	Path                  string              `json:"path"`
	Bytes                 int                 `json:"bytes"`
	VerifiedBytes         int                 `json:"verified_bytes"`
	VerifiedContent       string              `json:"verified_content,omitempty"`
	VerificationTruncated bool                `json:"verification_truncated,omitempty"`
}

type SearchRequest struct {
	Query           string
	Scope           string
	Kinds           []Kind
	Limit           int
	IncludeInactive bool
	IncludeRetired  bool
	Explain         bool
}

type SearchResult struct {
	Items   []SearchItem
	Explain *SearchExplain
}

type SearchItem struct {
	Ref         string
	Kind        string
	Title       string
	Status      string
	Scope       string
	Tags        []string
	Origin      string
	TaskPattern string
	Path        string
	Snippet     string
	Score       float64
	SourceRun   string
	SourceRefs  []string
	Created     string
	Updated     string
}

type SearchExplain struct {
	Query  string
	Scope  string
	Stages []SearchStageExplain
	Items  []SearchItemExplain
}

type SearchStageExplain struct {
	Name           string
	CandidateCount int
}

type SearchItemExplain struct {
	Ref        string
	FinalScore float64
}

type HistoryEvent struct {
	SessionID    string
	RunID        string
	Status       string
	Summary      string
	FilesChanged []string
	Timestamp    time.Time
}

// Record is the simplified V2 memory record. Procedure records, relations,
// evidence_refs, and validity windows have been removed.
type Record struct {
	Ref         string
	Kind        Kind
	RootPath    string
	RelPath     string
	Title       string
	Status      Status
	Scope       string
	Tags        []string
	Origin      string
	TaskPattern string
	SourceRefs  []string
	Body        string
	Created     string
	Updated     string
	SourceRun   string
}
