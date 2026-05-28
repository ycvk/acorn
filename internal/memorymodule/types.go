package memorymodule

import (
	"context"
	"fmt"
	"strings"
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

type ProcedureOrigin string

const (
	ProcedureOriginHuman          ProcedureOrigin = "human"
	ProcedureOriginAgentDraft     ProcedureOrigin = "agent_draft"
	ProcedureOriginActionVerified ProcedureOrigin = "action_verified"
)

type RelationType string

const (
	RelationSupports    RelationType = "supports"
	RelationDerivedFrom RelationType = "derived_from"
	RelationSupersedes  RelationType = "supersedes"
	RelationContradicts RelationType = "contradicts"
)

type RecordRelation struct {
	Type   RelationType
	Target string
	Reason string
}

type Service interface {
	Root() string
	ListFacts(ctx context.Context, selection RecordSelection) ([]Record, error)
	ListSkills(ctx context.Context, selection RecordSelection) ([]Record, error)
	ListHistory(ctx context.Context, selection RecordSelection) ([]Record, error)
	Prepare(ctx context.Context, req PrepareRequest) (*PrepareResult, error)
	Search(ctx context.Context, req SearchRequest) (*SearchResult, error)
	RebuildSemanticIndex(ctx context.Context, opts SemanticRebuildOptions) (*SemanticRebuildResult, error)
	AppendHistory(ctx context.Context, event HistoryEvent) error
	PlanMemoryMutation(ctx context.Context, req PlanMemoryMutationRequest) (*MemoryMutationPlan, error)
	ApplyMemoryMutation(ctx context.Context, req PlanMemoryMutationRequest) (*MemoryMutationResult, error)
	CreateProcedure(ctx context.Context, req CreateProcedureRequest) (*ProcedureRecord, error)
	BuildMemoryInstruction(ctx context.Context, workspaceSlug string) (string, error)
}

type Config struct {
	Root string
}

type LocalService struct {
	root            string
	semanticRuntime *SemanticRuntimeOptions
	mu              sync.RWMutex
	index           *MemoryIndex
}

type SemanticRuntimeOptions struct {
	Index      SemanticIndex
	Embedder   Embedder
	Model      string
	Dimensions int
	BatchSize  int
	Schema     string
	IndexName  string
	Mode       string
}

type PrepareRequest struct {
	RunID         string
	SessionID     string
	WorkspaceSlug string
	UserInput     string
	Mode          string
	MaxNudges     int
	MaxEntries    int
	Explain       bool
}

type PrepareResult struct {
	Nudges               []Nudge
	Entries              []Entry
	SkillTree            *SkillTreeIndex
	ProcedureActivations []ProcedureActivation
	Explain              *SearchExplain
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

type ProcedureRecord struct {
	Ref          string
	Title        string
	Status       Status
	TaskPattern  string
	Body         string
	Origin       ProcedureOrigin
	SourceRun    string
	SourceRefs   []string
	EvidenceRefs []string
	Tags         []string
	Created      string
	Updated      string
	MutationPlan *MemoryMutationPlan
}

type CreateProcedureRequest struct {
	Title        string
	TaskPattern  string
	Body         string
	SourceRun    string
	SourceRefs   []string
	EvidenceRefs []string
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
	Message               string                 `json:"message"`
	MutationPlan          *MemoryMutationPlan    `json:"mutation_plan"`
	Path                  string                 `json:"path"`
	Bytes                 int                    `json:"bytes"`
	VerifiedBytes         int                    `json:"verified_bytes"`
	VerifiedContent       string                 `json:"verified_content,omitempty"`
	VerificationTruncated bool                   `json:"verification_truncated,omitempty"`
	SemanticRebuild       *SemanticRebuildResult `json:"semantic_rebuild,omitempty"`
}

type ProcedureActivationPhase string

const (
	ProcedureActivationMatched  ProcedureActivationPhase = "matched"
	ProcedureActivationSelected ProcedureActivationPhase = "selected"
	ProcedureActivationInjected ProcedureActivationPhase = "injected"
	ProcedureActivationUsed     ProcedureActivationPhase = "used"
	ProcedureActivationRejected ProcedureActivationPhase = "rejected"
)

type ProcedureActivation struct {
	RunID        string
	SessionID    string
	ProcedureRef string
	Title        string
	Kind         string
	Phase        ProcedureActivationPhase
	Reason       string
	Score        float64
	Status       Status
	Origin       ProcedureOrigin
	SourceRefs   []string
	EvidenceRefs []string
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
	Ref          string
	Kind         string
	Title        string
	Status       string
	Scope        string
	Tags         []string
	Origin       string
	TaskPattern  string
	Path         string
	Snippet      string
	Score        float64
	SourceRun    string
	SourceRefs   []string
	EvidenceRefs []string
	Relations    []RecordRelation
	Created      string
	Updated      string
	ValidFrom    string
	ValidUntil   string
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
	Ref           string
	FinalScore    float64
	Contributions []ScoreContribution
}

type ScoreContribution struct {
	Stage      string
	Delta      float64
	Reason     string
	SourceRefs []string
}

type HistoryEvent struct {
	SessionID    string
	RunID        string
	Status       string
	Summary      string
	FilesChanged []string
	Timestamp    time.Time
}

type Record struct {
	Ref          string
	Kind         Kind
	RootPath     string
	RelPath      string
	Title        string
	Status       Status
	Scope        string
	Tags         []string
	Origin       string
	TaskPattern  string
	SourceRefs   []string
	EvidenceRefs []string
	Relations    []RecordRelation
	Body         string
	Created      string
	Updated      string
	ValidFrom    string
	ValidUntil   string
	SourceRun    string
}

func ProcedureRecordFromMemoryRecord(record Record) (*ProcedureRecord, error) {
	if record.Kind != KindSkill {
		return nil, fmt.Errorf("memory record %q is %q, not skill procedure", record.Ref, record.Kind)
	}
	if err := validateProcedureRecord(record); err != nil {
		return nil, err
	}
	return &ProcedureRecord{
		Ref:          record.Ref,
		Title:        record.Title,
		Status:       record.Status,
		TaskPattern:  record.TaskPattern,
		Body:         record.Body,
		Origin:       ProcedureOrigin(record.Origin),
		SourceRun:    record.SourceRun,
		SourceRefs:   append([]string(nil), record.SourceRefs...),
		EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
		Tags:         append([]string(nil), record.Tags...),
		Created:      record.Created,
		Updated:      record.Updated,
	}, nil
}

func ProcedureActivationFromRecord(runID string, sessionID string, record Record, phase ProcedureActivationPhase, reason string, score float64) ProcedureActivation {
	return ProcedureActivation{
		RunID:        strings.TrimSpace(runID),
		SessionID:    strings.TrimSpace(sessionID),
		ProcedureRef: strings.TrimSpace(record.Ref),
		Title:        strings.TrimSpace(record.Title),
		Kind:         string(record.Kind),
		Phase:        phase,
		Reason:       strings.TrimSpace(reason),
		Score:        score,
		Status:       record.Status,
		Origin:       ProcedureOrigin(record.Origin),
		SourceRefs:   append([]string(nil), record.SourceRefs...),
		EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
	}
}

func procedureInjectable(record Record) bool {
	if record.Kind != KindSkill || record.Status != StatusVerified {
		return false
	}
	switch ProcedureOrigin(record.Origin) {
	case ProcedureOriginHuman:
		return true
	case ProcedureOriginActionVerified:
		return strings.TrimSpace(record.SourceRun) != "" && len(record.EvidenceRefs) > 0
	default:
		return false
	}
}

func validateProcedureRecord(record Record) error {
	if record.Kind != KindSkill {
		return nil
	}
	switch ProcedureOrigin(record.Origin) {
	case ProcedureOriginHuman:
	case ProcedureOriginAgentDraft:
		if record.Status != StatusUnverified {
			return fmt.Errorf("agent_draft procedure status must be unverified")
		}
		if strings.TrimSpace(record.SourceRun) == "" {
			return fmt.Errorf("agent_draft procedure source_run is required")
		}
	case ProcedureOriginActionVerified:
		if record.Status != StatusVerified {
			return fmt.Errorf("action_verified procedure status must be verified")
		}
		if strings.TrimSpace(record.SourceRun) == "" {
			return fmt.Errorf("action_verified procedure source_run is required")
		}
		if len(record.EvidenceRefs) == 0 {
			return fmt.Errorf("action_verified procedure evidence_refs are required")
		}
	default:
		return fmt.Errorf("procedure origin must be human, agent_draft, or action_verified")
	}
	if strings.TrimSpace(record.TaskPattern) == "" {
		return fmt.Errorf("skill task_pattern is required")
	}
	return nil
}
