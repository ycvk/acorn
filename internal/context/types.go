package context

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/port"
)

type Plane interface {
	Assemble(context.Context, AssembleRequest) (*AssembleResult, error)
}

type AssembleRequest struct {
	RunID          string
	SessionID      string
	Input          string
	SelectedSkill  *SelectedSkill
	SkillSnapshot  *skills.Snapshot
	MemoryPrepared *memory.PrepareResult
	ToolCatalog    port.Catalog
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

type defaultPlane struct {
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

func NewDefaultPlane(opts DefaultOptions) Plane {
	p := &defaultPlane{
		memoryContextTokenBudget: opts.MemoryContextTokenBudget,
		maxContextTokens:         opts.MaxContextTokens,
		tokenCounter:             opts.TokenCounter,
		memoryBudget:             opts.MemoryContextTokenBudget,
	}
	return p
}

func (p *defaultPlane) Assemble(ctx context.Context, req AssembleRequest) (*AssembleResult, error) {
	if p.tokenCounter == nil {
		return nil, errors.New("context plane token counter is required")
	}

	memoryPacket, err := buildMemoryContextPacket(ctx, p.tokenCounter, p.memoryBudget, "", req.MemoryPrepared)
	if err != nil {
		return nil, err
	}
	memoryMessage := buildMemoryMessageFromPacket(memoryPacket)
	messages, err := budgetedContextMessages(ctx, p.tokenCounter, p.maxContextTokens, filterMessages(
		buildSkillContextMessage(req.SelectedSkill),
		buildSkillCatalogMessage(req.SkillSnapshot),
		memoryMessage,
	))
	if err != nil {
		return nil, err
	}
	lifecycleState := newToolLifecycleState(ctx, req)
	deferredNames := sortedDeferredToolNames(lifecycleState)

	return &AssembleResult{
		Messages:          messages,
		LifecycleState:    lifecycleState,
		EagerToolNames:    sortedLoadedToolNames(lifecycleState),
		DeferredToolNames: deferredNames,
	}, nil
}

// filterMessages drops nil entries.
func filterMessages(messages ...*schema.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			result = append(result, msg)
		}
	}
	return result
}

// budgetedContextMessages clones messages and verifies they fit within the
// token budget. When maxTokens <= 0 the budget check is skipped.
func budgetedContextMessages(ctx context.Context, counter TokenCounter, maxTokens int, messages []*schema.Message) ([]*schema.Message, error) {
	if counter == nil {
		return nil, errors.New("context message token counter is required")
	}
	if maxTokens <= 0 {
		return CloneMessages(messages), nil
	}
	cloned := CloneMessages(messages)
	adkMessages := make([]adk.Message, 0, len(cloned))
	for _, msg := range cloned {
		if msg != nil {
			adkMessages = append(adkMessages, msg)
		}
	}
	total, err := counter.CountMessages(ctx, adkMessages, nil)
	if err != nil {
		return nil, fmt.Errorf("count context message tokens: %w", err)
	}
	if total > maxTokens {
		return nil, fmt.Errorf("assembled context requires %d tokens over budget %d", total, maxTokens)
	}
	return cloned, nil
}

// CloneMessages returns a copy of the slice with cloned message structs,
// dropping nil entries.
func CloneMessages(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		clone := *msg
		out = append(out, &clone)
	}
	return out
}
