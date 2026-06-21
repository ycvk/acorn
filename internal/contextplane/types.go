package contextplane

import (
	"context"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
)

type ContextPlane interface {
	Assemble(context.Context, AssembleRequest) (*AssembleResult, error)
}

type AssembleRequest struct {
	RunID          string
	SessionID      string
	Input          string
	SelectedSkill  *SelectedSkill
	SkillSnapshot  *skills.Snapshot
	MemoryPrepared *memorymodule.PrepareResult
	ToolCatalog    *tooling.Catalog
}

type AssembleResult struct {
	Messages          []*schema.Message
	LifecycleState    *ToolLifecycleState
	EagerToolNames    []string
	DeferredToolNames []string
}

type ToolCallEvent struct {
	RunID     string
	SessionID string
	TurnIndex int
	CallID    string
	ToolName  string
	Arguments string
}

type ToolResultEvent struct {
	RunID        string
	SessionID    string
	TurnIndex    int
	CallID       string
	ToolName     string
	Arguments    string
	Result       string
	IsError      bool
	ErrorReason  string
	ResultTokens int
}

type DeferredLoadRequest struct {
	RunID     string
	SessionID string
	Query     string
	ToolNames []string
	Limit     int
}

type DeferredLoadResult struct {
	Messages        []*schema.Message
	LoadedToolNames []string
	AlreadyLoaded   []string
}

type DefaultOptions struct {
	MemoryContextTokenBudget int
	MaxContextTokens         int
	TokenCounter             TokenCounter
}

type defaultContextPlane struct {
	memoryContextTokenBudget int
	maxContextTokens         int
	tokenCounter             TokenCounter
	memoryBudget             int
}

type ToolLifecycleState struct {
	RunID         string
	SessionID     string
	LoadedTools   map[string]LoadedToolRecord
	DeferredTools map[string]DeferredToolRecord
	MaxAgeTurns   int
	mu            sync.Mutex
}

func (s *ToolLifecycleState) Mu() *sync.Mutex {
	return &s.mu
}

type LoadedToolRecord struct {
	Name       string
	LoadedAt   time.Time
	LoadSource string
}

type DeferredToolRecord struct {
	Name        string
	Reason      string
	Description string
}

func NewDefaultContextPlane(opts DefaultOptions) ContextPlane {
	p := &defaultContextPlane{
		memoryContextTokenBudget: opts.MemoryContextTokenBudget,
		maxContextTokens:         opts.MaxContextTokens,
		tokenCounter:             opts.TokenCounter,
		memoryBudget:             opts.MemoryContextTokenBudget,
	}
	return p
}
