package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
)

// presenceMiddleware appends the rendered present as a system message to the
// input of every model call. The message is not written back to agent state:
// it never reaches history or summarization, and the unchanged prefix keeps
// prompt caching across iterations. Each distinct rendering is saved as a
// context snapshot and referenced by a presence.snapshot event.
type presenceMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	phones      core.PhoneNotificationStore
	store       core.ActivityStore
	memory      core.MemoryStore
	commitments core.CommitmentStore
	events      core.EventAppender
	clock       func() time.Time
	location    *time.Location
	maxTokens   int
	inputLimit  int
	counter     TokenCounter
	instruction string
	runID       string
	runMemory   *runMemory

	mu       sync.Mutex
	lastHash string
}

func newPresenceMiddleware(deps RuntimeDeps, counter TokenCounter, runID string) (*presenceMiddleware, error) {
	if deps.PhoneNotifications == nil || deps.Activity == nil || deps.MemoryStore == nil || deps.Commitments == nil || deps.Clock == nil || deps.Location == nil {
		return nil, fmt.Errorf("presence middleware requires PhoneNotifications, Presence, Clock and Location")
	}
	inputBudget, err := deps.Config.InputTokenBudget()
	if err != nil {
		return nil, err
	}
	return &presenceMiddleware{
		inputLimit:                        inputBudget + deps.Config.Presence.MaxTokens + deps.Config.Context.CompactMarginTokens,
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		store:                             deps.Activity,
		memory:                            deps.MemoryStore,
		commitments:                       deps.Commitments,
		phones:                            deps.PhoneNotifications,
		events:                            deps.Store,
		clock:                             deps.Clock,
		location:                          deps.Location,
		maxTokens:                         deps.Config.Presence.MaxTokens,
		counter:                           counter,
		runID:                             runID,
	}, nil
}

// BeforeAgent records the instruction as earlier handlers left it, so the
// snapshot holds exactly what the model receives.
func (m *presenceMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	m.mu.Lock()
	m.instruction = runCtx.Instruction
	if m.runMemory != nil {
		m.runMemory.instruction = runCtx.Instruction
	}
	m.mu.Unlock()
	return ctx, runCtx, nil
}

func (m *presenceMiddleware) WrapModel(_ context.Context, model einomodel.AgenticModel, _ *adk.TypedModelContext[*schema.AgenticMessage]) (einomodel.AgenticModel, error) {
	return &presenceModel{inner: model, mw: m}, nil
}

type presenceModel struct {
	inner einomodel.AgenticModel
	mw    *presenceMiddleware
}

func (p *presenceModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...einomodel.Option) (*schema.AgenticMessage, error) {
	withPresence, err := p.mw.withPresence(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := p.mw.checkBudget(ctx, withPresence, opts); err != nil {
		return nil, err
	}
	return p.inner.Generate(ctx, withPresence, opts...)
}

func (p *presenceModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...einomodel.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	withPresence, err := p.mw.withPresence(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := p.mw.checkBudget(ctx, withPresence, opts); err != nil {
		return nil, err
	}
	return p.inner.Stream(ctx, withPresence, opts...)
}

// withPresence returns a new slice: input followed by the present.
func (m *presenceMiddleware) withPresence(ctx context.Context, input []*schema.AgenticMessage) ([]*schema.AgenticMessage, error) {
	rendered, err := m.render(ctx)
	if err != nil {
		return nil, fmt.Errorf("render presence: %w", err)
	}
	if err := m.recordSnapshot(ctx, rendered); err != nil {
		return nil, err
	}
	out := make([]*schema.AgenticMessage, 0, len(input)+2)
	out = append(out, input...)
	if m.runMemory != nil {
		memoryText, err := m.runMemory.render(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, schema.SystemAgenticMessage(memoryText))
	}
	return append(out, schema.SystemAgenticMessage(rendered)), nil
}

func (m *presenceMiddleware) render(ctx context.Context) (string, error) {
	now := m.clock()
	commitments, err := m.commitments.ListCommitments(ctx, true)
	if err != nil {
		return "", err
	}
	occurrences, err := m.commitments.ListDueOccurrences(ctx)
	if err != nil {
		return "", err
	}
	thoughts, err := m.memory.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", Kind: "thought", Limit: 20})
	if err != nil {
		return "", err
	}
	concerns, err := m.memory.ListConcerns(ctx, true)
	if err != nil {
		return "", err
	}
	phones, err := m.phones.ListPhoneNotifications(ctx, now.Add(-6*time.Hour), now.Add(time.Nanosecond), 10)
	if err != nil {
		return "", err
	}
	return presence.Render(presence.RenderInput{
		PhoneNotifications: phones.Items,
		Now:                now,
		Location:           m.location,
		Wake:               wakeOrDefault(ctx),
		Commitments:        commitments,
		Occurrences:        occurrences,
		Thoughts:           thoughts,
		Concerns:           concerns,
		MaxTokens:          m.maxTokens,
		Count:              func(text string) (int, error) { return m.counter.CountText(ctx, text) },
	})
}

func (m *presenceMiddleware) recordSnapshot(ctx context.Context, rendered string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sum := sha256.Sum256([]byte(m.instruction + "\n" + rendered))
	hash := hex.EncodeToString(sum[:])
	if hash == m.lastHash {
		return nil
	}
	if err := m.store.SaveContextSnapshot(ctx, hash, m.instruction+"\n\n"+rendered); err != nil {
		return err
	}
	if _, err := m.events.AppendEvent(ctx, m.runID, "presence.snapshot", map[string]any{"hash": hash}); err != nil {
		return fmt.Errorf("append presence.snapshot event: %w", err)
	}
	m.lastHash = hash
	return nil
}

func wakeOrDefault(ctx context.Context) string {
	if wake := core.GetWake(ctx); wake != "" {
		return wake
	}
	return "owner message"
}

func (m *presenceMiddleware) checkBudget(ctx context.Context, messages []*schema.AgenticMessage, opts []einomodel.Option) error {
	options := einomodel.GetCommonOptions(nil, opts...)
	tools := append(append([]*schema.ToolInfo(nil), options.Tools...), options.DeferredTools...)
	n, err := m.counter.CountMessages(ctx, messages, tools)
	if err != nil {
		return err
	}
	if n > m.inputLimit {
		return fmt.Errorf("model input exceeds available context: %d tokens, limit %d", n, m.inputLimit)
	}
	return nil
}
