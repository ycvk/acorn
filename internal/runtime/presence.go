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

// presenceStatuses are the items rendered into the present.
var presenceStatuses = []core.MemoryStatus{core.MemoryActive, core.MemoryResting, core.MemoryWoken}

// presenceMiddleware appends the rendered present as a system message to the
// input of every model call. The message is not written back to agent state:
// it never reaches history or summarization, and the unchanged prefix keeps
// prompt caching across iterations. Each distinct rendering is saved as a
// context snapshot and referenced by a presence.snapshot event.
type presenceMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	phones      core.PhoneNotificationStore
	store       core.PresenceStore
	events      core.EventAppender
	clock       func() time.Time
	location    *time.Location
	maxTokens   int
	counter     TokenCounter
	instruction string
	runID       string

	mu       sync.Mutex
	lastHash string
}

func newPresenceMiddleware(deps RuntimeDeps, counter TokenCounter, runID string) (*presenceMiddleware, error) {
	if deps.PhoneNotifications == nil || deps.Presence == nil || deps.Clock == nil || deps.Location == nil {
		return nil, fmt.Errorf("presence middleware requires PhoneNotifications, Presence, Clock and Location")
	}
	return &presenceMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		store:                             deps.Presence,
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
	return p.inner.Generate(ctx, withPresence, opts...)
}

func (p *presenceModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...einomodel.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	withPresence, err := p.mw.withPresence(ctx, input)
	if err != nil {
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
	out := make([]*schema.AgenticMessage, 0, len(input)+1)
	out = append(out, input...)
	return append(out, schema.SystemAgenticMessage(rendered)), nil
}

func (m *presenceMiddleware) render(ctx context.Context) (string, error) {
	now := m.clock()
	items, err := m.store.ListMemoryItems(ctx, presenceStatuses)
	if err != nil {
		return "", err
	}
	items, err = applyDecay(ctx, m.store, items, now)
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
		Items:              items,
		MaxTokens:          m.maxTokens,
		Count:              func(text string) (int, error) { return m.counter.CountText(ctx, text) },
	})
}

// applyDecay persists the decayed items and returns the items still rendered.
func applyDecay(ctx context.Context, store core.PresenceStore, items []core.MemoryItem, now time.Time) ([]core.MemoryItem, error) {
	changed := presence.Decay(items, now)
	if len(changed) == 0 {
		return items, nil
	}
	byID := make(map[int64]core.MemoryItem, len(changed))
	for _, item := range changed {
		if err := store.UpdateMemoryItem(ctx, item); err != nil {
			return nil, fmt.Errorf("decay memory item %d: %w", item.ID, err)
		}
		byID[item.ID] = item
	}
	out := make([]core.MemoryItem, 0, len(items))
	for _, item := range items {
		if updated, ok := byID[item.ID]; ok {
			item = updated
		}
		if item.Status == core.MemorySunk {
			continue
		}
		out = append(out, item)
	}
	return out, nil
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
