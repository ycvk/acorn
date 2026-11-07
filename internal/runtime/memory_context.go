package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
)

type MemoryContextService interface {
	MeterCompaction(einomodel.AgenticModel) einomodel.AgenticModel
	// History is the thread history through the run's input; after leaves
	// out the messages up to it.
	History(ctx context.Context, session string, after, through int64, budget int) (memory.History, error)
	Recall(context.Context, core.MemoryQuery) (core.MemoryRecall, error)
	RefreshRecall(context.Context, core.MemoryRecall) (core.MemoryRecall, error)
}

// runMemory belongs to one runner attempt. Its epoch is also the checkpoint
// visibility version. It never changes the source messages owned by callers.
type runMemory struct {
	instruction string
	epoch       atomic.Int64
	history     memory.History
	images      map[int64][]*schema.UserInputImage
	owner       ownerFiles
	recall      core.MemoryRecall
	budget      int
	service     MemoryContextService
	store       core.MemoryStore
	activity    core.ActivityStore
	runID       string
	counter     TokenCounter
	clock       func() time.Time
}

func prepareRunMemory(ctx context.Context, deps RuntimeDeps, req RunnerBuildRequest, instruction string, catalogTokens int, owner ownerFiles) (*runMemory, error) {
	counter, err := NewTokenCounter()
	if err != nil {
		return nil, err
	}
	available, err := deps.Config.InputTokenBudget()
	if err != nil {
		return nil, err
	}
	instructionTokens, err := counter.CountText(ctx, instruction)
	if err != nil {
		return nil, err
	}
	available -= instructionTokens + catalogTokens + 512
	sources, err := deps.MemoryStore.RunMemorySources(ctx, req.RunID)
	if err != nil {
		return nil, err
	}
	var through int64
	var current []core.MemorySource
	for _, s := range sources {
		if s.Kind == "message" && s.Speaker != "assistant" {
			id, err := strconv.ParseInt(s.ObjectID, 10, 64)
			if err != nil {
				return nil, err
			}
			if id > through {
				through = id
				current = []core.MemorySource{s}
			}
		}
	}
	if through == 0 {
		return nil, errors.New("run has no bound memory input source")
	}
	inputTokens, err := counter.CountText(ctx, current[0].Content)
	if err != nil {
		return nil, err
	}
	for _, file := range owner.byMessage[through] {
		if file.InlineImage() {
			inputTokens += core.ImageInputTokens
		}
	}
	memoryBudget := min(memory.ContextTokens, max(256, (available-inputTokens-512)/3))
	historyBudget := min(available-memoryBudget, max(memory.HistoryTokens, inputTokens+128))
	if historyBudget < 256 || available <= inputTokens+512 {
		return nil, fmt.Errorf("run input and tools exceed context budget (%d available)", available)
	}
	var after int64
	if req.Standalone {
		after = through - 1
	}
	history, err := deps.Memory.History(ctx, req.SessionID, after, through, historyBudget)
	if err != nil {
		return nil, fmt.Errorf("thread history: %w", err)
	}
	images, err := ownerFileImages(ctx, deps.Files, req.SessionID, owner, history.Messages)
	if err != nil {
		return nil, err
	}
	query := current[0].Content
	// The names of the files the owner sent are part of what they asked about,
	// and all there is when the message has no text.
	for _, file := range owner.byMessage[through] {
		query = strings.TrimSpace(query + "\n" + file.Name)
	}
	if req.Wake != "" {
		query = req.Wake + "\n" + query
	}
	// Query input is bounded independently from complete recent history.
	runes := []rune(query)
	if len(runes) > 2048 {
		query = string(runes[:2048])
	}
	recall, err := deps.Memory.Recall(ctx, core.MemoryQuery{Query: query, Mode: "current", Depth: "standard", MaxTokens: memoryBudget})
	if err != nil {
		return nil, fmt.Errorf("automatic recall: %w", err)
	}
	m := &runMemory{instruction: instruction, history: history, images: images, owner: owner, recall: recall, budget: memoryBudget, service: deps.Memory, store: deps.MemoryStore, activity: deps.Activity, runID: req.RunID, counter: counter, clock: deps.Clock}
	if recall.Epoch != history.Epoch {
		return nil, core.ErrMemoryExcluded
	}
	m.epoch.Store(history.Epoch)
	if _, err := m.render(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *runMemory) messages() []*schema.AgenticMessage {
	var messages []*schema.AgenticMessage
	if m.history.Summary != nil {
		msg := schema.SystemAgenticMessage("<thread_summary>\n" + m.history.Summary.Content + "\n</thread_summary>")
		msg.Extra = map[string]any{"memory_summary": true}
		messages = append(messages, msg)
	}
	for _, item := range m.history.Messages {
		if item.Role != "user" && item.Role != "assistant" && item.Role != core.MessageRoleWake {
			continue
		}
		text := item.Content
		if note := m.owner.note(item.ID); note != "" {
			text = strings.TrimSpace(text + "\n\n" + note)
		}
		msg := schema.UserAgenticMessage(text)
		for _, image := range m.images[item.ID] {
			msg.ContentBlocks = append(msg.ContentBlocks, schema.NewContentBlock(image))
		}
		if item.Role == "assistant" {
			msg = &schema.AgenticMessage{Role: schema.AgenticRoleTypeAssistant, ContentBlocks: []*schema.ContentBlock{{Type: schema.ContentBlockTypeAssistantGenText, AssistantGenText: &schema.AssistantGenText{Text: item.Content}}}}
		}
		msg.Extra = map[string]any{"memory_source_id": core.MessageSourceID(item.ID)}
		messages = append(messages, msg)
	}
	return messages
}

func (m *runMemory) render(ctx context.Context) (string, error) {
	refreshed, err := m.service.RefreshRecall(ctx, m.recall)
	if err != nil {
		return "", err
	}
	m.recall = refreshed
	lines, err := m.store.Portrait(ctx)
	if err != nil {
		return "", err
	}
	var portrait []string
	for _, line := range lines {
		if line.State == core.PortraitLineShown {
			portrait = append(portrait, line.Content)
		}
	}
	// Publish current-run source IDs for synchronous keep/correct/settle calls.
	current, err := m.store.RunMemorySources(ctx, m.runID)
	if err != nil {
		return "", err
	}
	type sourceRef struct {
		ID      string `json:"id"`
		Speaker string `json:"speaker"`
		Tool    string `json:"tool,omitempty"`
	}
	refs := make([]sourceRef, 0, len(current))
	for _, s := range current {
		refs = append(refs, sourceRef{s.ID, s.Speaker, s.ToolName})
	}
	value := struct {
		Portrait []string          `json:"owner_portrait,omitempty"`
		Recall   core.MemoryRecall `json:"recall"`
		Current  []sourceRef       `json:"current_run_sources"`
	}{portrait, m.recall, refs}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	for {
		n, err := m.counter.CountText(ctx, string(data))
		if err != nil {
			return "", err
		}
		if n <= m.budget {
			break
		}
		if len(value.Recall.Hits) > 0 {
			value.Recall.Hits = value.Recall.Hits[:len(value.Recall.Hits)-1]
		} else if len(value.Recall.Sources) > 0 {
			value.Recall.Sources = value.Recall.Sources[:len(value.Recall.Sources)-1]
		} else if len(value.Portrait) > 0 {
			value.Portrait = value.Portrait[:len(value.Portrait)-1]
		} else {
			return "", fmt.Errorf("memory source references exceed %d-token budget", m.budget)
		}
		data, err = json.Marshal(value)
		if err != nil {
			return "", err
		}
	}
	// The run's replies and tool results derive from what the model reads:
	// history, the recall kept within budget and the run's own sources.
	// Forgetting rewrites the portrait itself, so its sources stay out.
	sourceIDs := append([]string{}, m.history.SourceIDs...)
	var recordIDs []string
	for _, hit := range value.Recall.Hits {
		sourceIDs = append(sourceIDs, hit.Record.SourceIDs...)
		recordIDs = append(recordIDs, hit.Record.ID)
	}
	for _, s := range value.Recall.Sources {
		sourceIDs = append(sourceIDs, s.ID)
	}
	for _, s := range current {
		sourceIDs = append(sourceIDs, s.ID)
	}
	rendered := "<memory_context>\n" + string(data) + "\n</memory_context>"
	sum := sha256.Sum256([]byte(rendered))
	hash := hex.EncodeToString(sum[:])
	if err = m.activity.SaveContextSnapshot(ctx, hash, rendered); err != nil {
		return "", err
	}
	if err = m.store.SaveMemorySnapshotRefs(ctx, hash, m.runID, sourceIDs, recordIDs, m.epoch.Load()); err != nil {
		return "", err
	}
	return rendered, nil
}

type memoryVisibilityMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	memory *runMemory
}

// The visibility hook precedes Eino summarization. A changed exclusion epoch
// reconstructs source-backed history and removes generated state derived from
// the older input, including compacted summaries and prior tool arguments.
func (m *memoryVisibilityMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.TypedChatModelAgentState[*schema.AgenticMessage], _ *adk.TypedModelContext[*schema.AgenticMessage]) (context.Context, *adk.TypedChatModelAgentState[*schema.AgenticMessage], error) {
	epoch, err := m.memory.store.MemoryEpoch(ctx)
	if err != nil {
		return ctx, nil, err
	}
	if epoch == m.memory.epoch.Load() {
		return ctx, state, nil
	}
	history := m.memory.history
	history.Summary = nil
	history.SourceIDs = nil
	history.Messages = nil
	for _, msg := range m.memory.history.Messages {
		id := core.MessageSourceID(msg.ID)
		source, err := m.memory.store.LoadThreadSource(ctx, id)
		if errors.Is(err, core.ErrMemoryExcluded) {
			continue
		}
		if err != nil {
			return ctx, nil, err
		}
		msg.Content = source.Content
		history.Messages = append(history.Messages, msg)
		history.SourceIDs = append(history.SourceIDs, id)
	}
	history.Epoch = epoch
	m.memory.history = history
	m.memory.epoch.Store(epoch)
	clone := *state
	clone.Messages = m.memory.messages()
	if m.memory.instruction != "" {
		clone.Messages = append([]*schema.AgenticMessage{schema.SystemAgenticMessage(m.memory.instruction)}, clone.Messages...)
	}
	// Preserve the verified outcome while removing source-bearing tool history.
	status := "Cancellation completion is unconfirmed; report that limitation and do not claim full completion."
	if memoryForgetConfirmed(state.Messages, epoch) {
		status = "Affected executions have stopped; acknowledge the completed memory update."
	}
	clone.Messages = append(clone.Messages, schema.UserAgenticMessage(fmt.Sprintf("Memory exclusion version %d is committed. %s Continue from the visible conversation without repeating excluded information.", epoch, status)))
	return ctx, &clone, nil
}

func memoryScope(ctx context.Context, runID string, autonomous bool) context.Context {
	return memory.WithRun(ctx, runID, autonomous)
}

func (m *memoryVisibilityMiddleware) WrapInvokableToolCall(_ context.Context, next adk.InvokableToolCallEndpoint, _ *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
		epoch, err := m.memory.store.MemoryEpoch(ctx)
		if err != nil {
			return "", err
		}
		if epoch != m.memory.epoch.Load() {
			return "", core.ErrMemoryExcluded
		}
		return next(ctx, args, opts...)
	}, nil
}

func memoryForgetConfirmed(messages []*schema.AgenticMessage, epoch int64) bool {
	for _, message := range messages {
		for _, block := range message.ContentBlocks {
			result := block.FunctionToolResult
			if result == nil || result.Name != "memory_forget" {
				continue
			}
			for _, content := range result.Content {
				if content.Text == nil {
					continue
				}
				var exclusion core.MemoryForgetResult
				if err := json.Unmarshal([]byte(content.Text.Text), &exclusion); err == nil && exclusion.Version == epoch && (len(exclusion.SourceIDs) > 0 || len(exclusion.RecordIDs) > 0) {
					return true
				}
			}
		}
	}
	return false
}
