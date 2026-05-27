package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/localit-io/tiktoken-go"
	"github.com/ycvk/acorn/internal/contextplane"

	"github.com/ycvk/acorn/internal/config"
)

func TestCompressionTokenCounterUsesConfiguredEncoding(t *testing.T) {
	cfg := config.ContextConfig{
		TokenEncoding: "cl100k_base",
	}
	counter, err := contextplane.NewCompressionTokenCounter(cfg)
	if err != nil {
		t.Fatalf("contextplane.NewCompressionTokenCounter: %v", err)
	}

	messages := []adk.Message{
		schema.UserMessage("请务必保留这个需求：不要删掉最近的调试上下文。func main() { fmt.Println(\"hello 世界 🚀\") }"),
		schema.AssistantMessage("收到，我会保留最近的调试上下文。", nil),
	}
	tools := []*schema.ToolInfo{
		{Name: "read_file", Desc: "Read a local file from disk."},
	}

	got, err := counter.CountMessages(context.Background(), messages, tools)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	expectedCL := compressionExpectedTokens(t, "cl100k_base", messages, tools)
	expectedO200K := compressionExpectedTokens(t, "o200k_base", messages, tools)
	if expectedCL == expectedO200K {
		t.Fatal("test fixture must produce distinct token counts across encodings")
	}
	if got != expectedCL {
		t.Fatalf("token count = %d, want cl100k_base count %d", got, expectedCL)
	}
	if got == expectedO200K {
		t.Fatalf("token count should not match o200k_base count %d when cl100k_base is configured", expectedO200K)
	}
}

func TestCompressionTokenCounterRejectsUnknownEncoding(t *testing.T) {
	_, err := contextplane.NewCompressionTokenCounter(config.ContextConfig{
		TokenEncoding: "unknown_encoding",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown_encoding") {
		t.Fatalf("contextplane.NewCompressionTokenCounter error = %v, want unknown encoding", err)
	}
}

func TestCompressionPipelineOrdersStackBeforeCustomHandler(t *testing.T) {
	middlewares, err := NewCompressionMiddlewareBuilder().Build(context.Background(), compressionEnabledTestConfig(), &fakeCompressionChatModel{
		response: structuredCompressionSummary("condensed summary"),
	}, contextplane.CompressionBuildOptions{
		RuntimeStorageDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CompressionPipeline.Build: %v", err)
	}
	if got, want := len(middlewares), 2; got != want {
		t.Fatalf("middleware count = %d, want %d", got, want)
	}

	handlers := append([]adk.ChatModelAgentMiddleware(nil), middlewares...)
	handlers = append(handlers, &adk.BaseChatModelAgentMiddleware{})
	wantOrder := []string{
		"patchtoolcalls",
		"toolLifecycleMiddleware",
		"BaseChatModelAgentMiddleware",
	}
	for i, want := range wantOrder {
		if got := reflect.TypeOf(handlers[i]).String(); !strings.Contains(got, want) {
			t.Fatalf("handler[%d] type = %q, want substring %q", i, got, want)
		}
	}
}

func containsLayer(layers []contextplane.CompactLayer, want contextplane.CompactLayer) bool {
	for _, l := range layers {
		if l == want {
			return true
		}
	}
	return false
}

func TestContextCompressionPipelineAutoTrigger(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("condensed summary")},
			SummaryText: "condensed summary",
			Outcome: contextplane.CompressionOutcome{
				BoundaryID:     "ctxb_auto",
				TokensBefore:   100,
				TokensAfter:    40,
				Summary:        "condensed summary",
				SummarySnippet: "condensed summary",
			},
		},
	}
	pipeline := NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
		Governor:         testBudgetGovernor{pressure: testPressure(contextplane.PressureAutoCompact)},
		CompactionEngine: engine,
		TokenCounter:     testTokenCounter(t),
	})

	req := contextplane.PipelineRequest{
		Trigger:         contextplane.CompactTriggerAuto,
		TurnIndex:       10,
		LastCompactTurn: 3,
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("old message 1"),
			schema.AssistantMessage("old response 1", nil),
			schema.UserMessage("old message 2"),
			schema.AssistantMessage("old response 2", nil),
			schema.UserMessage("recent message"),
			schema.AssistantMessage("Recent response 1", nil),
		},
	}
	result, err := pipeline.Compress(context.Background(), req)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Messages) == 0 {
		t.Fatal("result.Messages is empty")
	}
	if !containsLayer(result.LayersApplied, contextplane.CompactLayerAutocompact) {
		t.Fatalf("LayersApplied = %v, want auto compact", result.LayersApplied)
	}
}

func TestContextCompressionPipelineReactiveTrigger(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("reactive compact summary")},
			SummaryText: "reactive compact summary",
			Outcome: contextplane.CompressionOutcome{
				BoundaryID:     "ctxb_reactive",
				TokensBefore:   120,
				TokensAfter:    40,
				Summary:        "reactive compact summary",
				SummarySnippet: "reactive compact summary",
			},
		},
	}
	pipeline := NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
		Governor:         testBudgetGovernor{pressure: testPressure(contextplane.PressureBlocking), dynamic: true},
		CompactionEngine: engine,
		TokenCounter:     testTokenCounter(t),
	})

	req := contextplane.PipelineRequest{
		Trigger:   contextplane.CompactTriggerReactive,
		TurnIndex: 15,
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("old message"),
			schema.AssistantMessage("old response", nil),
			schema.UserMessage("large prompt"),
		},
	}
	result, err := pipeline.Compress(context.Background(), req)
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if engine.request.Trigger != contextplane.CompactTriggerReactive {
		t.Fatalf("trigger = %q, want reactive", engine.request.Trigger)
	}
}

func TestContextCompressionPipelineReactiveTriggerHalvesRecentTurns(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("reactive compact summary")},
			SummaryText: "reactive compact summary",
			Outcome: contextplane.CompressionOutcome{
				BoundaryID:     "ctxb_reactive",
				TokensBefore:   120,
				TokensAfter:    110,
				Summary:        "reactive compact summary",
				SummarySnippet: "reactive compact summary",
			},
		},
	}
	pipeline := NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
		Governor:         testBudgetGovernor{pressure: testPressure(contextplane.PressureBlocking)},
		CompactionEngine: engine,
		TokenCounter:     testTokenCounter(t),
	})

	_, err := pipeline.Compress(context.Background(), contextplane.PipelineRequest{
		Trigger:        contextplane.CompactTriggerReactive,
		TurnIndex:      15,
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 4, PreserveToolPairs: true},
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("old 1"),
			schema.AssistantMessage("old resp 1", nil),
			schema.UserMessage("old 2"),
			schema.AssistantMessage("old resp 2", nil),
			schema.UserMessage("recent"),
			schema.AssistantMessage("recent resp", nil),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "context pressure remains blocking") {
		t.Fatalf("Compress error = %v, want blocking pressure after reactive layers", err)
	}
	if engine.request.PreservePolicy.RecentTurns != 2 {
		t.Fatalf("recent turns = %d, want 2 (halved from 4)", engine.request.PreservePolicy.RecentTurns)
	}
	if engine.request.Trigger != contextplane.CompactTriggerReactive {
		t.Fatalf("trigger = %q, want reactive", engine.request.Trigger)
	}
}

func TestContextCompressionPipelineRequiresTokenCounter(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages: []adk.Message{schema.SystemMessage("system")},
			Outcome:  contextplane.CompressionOutcome{TokensBefore: 10, TokensAfter: 5},
		},
	}
	pipeline := NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
		Governor:         testBudgetGovernor{pressure: testPressure(contextplane.PressureAutoCompact)},
		CompactionEngine: engine,
	})

	_, err := pipeline.Compress(context.Background(), contextplane.PipelineRequest{
		Trigger:        contextplane.CompactTriggerAuto,
		TurnIndex:      10,
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
		Messages:       []adk.Message{schema.UserMessage("large prompt")},
	})
	if !errors.Is(err, ErrPipelineTokenCounterRequired) {
		t.Fatalf("Compress error = %v, want ErrPipelineTokenCounterRequired", err)
	}
}

func TestCompactionEngineRejectsInvalidStructuredSummary(t *testing.T) {
	cfg := compressionEnabledTestConfig()
	counter, err := contextplane.NewCompressionTokenCounter(cfg)
	if err != nil {
		t.Fatalf("contextplane.NewCompressionTokenCounter: %v", err)
	}
	engine := NewDefaultCompactionEngine(CompactionEngineOptions{
		Model:        &fakeCompressionChatModel{response: "too short"},
		TokenCounter: counter,
	})

	_, err = engine.Compact(context.Background(), CompactRequest{
		Trigger: contextplane.CompactTriggerAuto,
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("Old request"),
			schema.AssistantMessage("Old response", nil),
			schema.UserMessage("Recent request"),
		},
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
	})
	if err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("Compact error = %v, want too short", err)
	}
}

func TestCompactionEngineRejectsMalformedStructuredSummary(t *testing.T) {
	cfg := compressionEnabledTestConfig()
	counter, err := contextplane.NewCompressionTokenCounter(cfg)
	if err != nil {
		t.Fatalf("contextplane.NewCompressionTokenCounter: %v", err)
	}
	engine := NewDefaultCompactionEngine(CompactionEngineOptions{
		Model:        &fakeCompressionChatModel{response: "This is a long unstructured paragraph that contains no continuation headings at all, but it is over fifty characters."},
		TokenCounter: counter,
	})

	_, err = engine.Compact(context.Background(), CompactRequest{
		Trigger: contextplane.CompactTriggerAuto,
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("Old request"),
			schema.AssistantMessage("Old response", nil),
			schema.UserMessage("Recent request"),
		},
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
	})
	if err == nil || !strings.Contains(err.Error(), "missing required section") {
		t.Fatalf("Compact error = %v, want missing required section", err)
	}
}

func TestCompactionEngineRejectsSummaryToolCalls(t *testing.T) {
	cfg := compressionEnabledTestConfig()
	counter, err := contextplane.NewCompressionTokenCounter(cfg)
	if err != nil {
		t.Fatalf("contextplane.NewCompressionTokenCounter: %v", err)
	}
	engine := NewDefaultCompactionEngine(CompactionEngineOptions{
		Model: &fakeCompressionChatModel{
			response: structuredCompressionSummary("attempted tool call"),
			toolCalls: []schema.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: schema.FunctionCall{
					Name: "read_file",
				},
			}},
		},
		TokenCounter: counter,
	})

	_, err = engine.Compact(context.Background(), CompactRequest{
		Trigger: contextplane.CompactTriggerAuto,
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("Old request"),
			schema.AssistantMessage("Old response", nil),
			schema.UserMessage("Recent request"),
		},
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
	})
	if err == nil || !strings.Contains(err.Error(), "tool calls") {
		t.Fatalf("Compact error = %v, want tool call rejection", err)
	}
}

func TestPreservedTailKeepsAssistantToolCallPair(t *testing.T) {
	toolCall := schema.AssistantMessage("", []schema.ToolCall{{
		ID:   "call_1",
		Type: "function",
		Function: schema.FunctionCall{
			Name:      "lookup",
			Arguments: `{"query":"acorn"}`,
		},
	}})
	toolResult := schema.ToolMessage("tool output must stay out of summary input", "call_1", schema.WithToolName("lookup"))
	messages := []adk.Message{
		schema.SystemMessage("system"),
		schema.AssistantMessage("older assistant message to summarize", nil),
		toolCall,
		toolResult,
	}
	policy := contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true}

	tail := preservedConversationTail(stripLeadingSystemMessages(messages), policy)
	if got, want := len(tail), 2; got != want {
		t.Fatalf("tail length = %d, want assistant tool call + tool result", got)
	}
	if tail[0].Role != schema.Assistant || len(tail[0].ToolCalls) != 1 {
		t.Fatalf("tail[0] = %#v, want assistant tool call", tail[0])
	}
	if tail[1].Role != schema.Tool || tail[1].ToolCallID != "call_1" {
		t.Fatalf("tail[1] = %#v, want matching tool result", tail[1])
	}

	input, err := buildSummarizerInput("", messages, policy)
	if err != nil {
		t.Fatalf("buildSummarizerInput: %v", err)
	}
	if strings.Contains(input[1].Content, "tool output must stay out of summary input") {
		t.Fatalf("preserved tool result leaked into summary input:\n%s", input[1].Content)
	}
}

func TestCompactionEngineInjectsRehydratedMessagesBeforePreservedTail(t *testing.T) {
	cfg := compressionEnabledTestConfig()
	cfg.PreserveRecentTurns = 1
	counter, err := contextplane.NewCompressionTokenCounter(cfg)
	if err != nil {
		t.Fatalf("contextplane.NewCompressionTokenCounter: %v", err)
	}
	engine := NewDefaultCompactionEngine(CompactionEngineOptions{
		Model:        &fakeCompressionChatModel{response: structuredCompressionSummary("rehydrated")},
		TokenCounter: counter,
	})

	result, err := engine.Compact(context.Background(), CompactRequest{
		Trigger: contextplane.CompactTriggerAuto,
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("<skill-context>\nSelected skill: cs-feat-impl\n</skill-context>"),
			schema.UserMessage("<memory-context>\n<working-checkpoint>\nfinish roadmap\n</working-checkpoint>\n</memory-context>"),
			schema.UserMessage("Old request"),
			schema.AssistantMessage("Old response", nil),
			schema.UserMessage("Recent request"),
			schema.AssistantMessage("Recent response", nil),
		},
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
	})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if got, want := len(result.RehydratePlan.Packets), 2; got != want {
		t.Fatalf("rehydrate packets = %d, want %d", got, want)
	}
	if got, want := len(result.Rehydrated), 2; got != want {
		t.Fatalf("rehydrated messages = %d, want %d", got, want)
	}
	if result.Messages[0].Role != schema.System {
		t.Fatalf("message[0] = %#v, want system", result.Messages[0])
	}
	if !isCompressionSummary(result.Messages[1]) {
		t.Fatalf("message[1] should be compression summary: %#v", result.Messages[1])
	}
	if !strings.Contains(result.Messages[2].Content, "Kind: working_checkpoint") {
		t.Fatalf("message[2] should be first rehydrated packet: %q", result.Messages[2].Content)
	}
	if !strings.Contains(result.Messages[3].Content, "Kind: selected_skill") {
		t.Fatalf("message[3] should be second rehydrated packet: %q", result.Messages[3].Content)
	}
	if result.Messages[4].Content != "Recent request" || result.Messages[5].Content != "Recent response" {
		t.Fatalf("tail not preserved after rehydration: %#v", result.Messages[4:])
	}
}

func TestCompressionIncrementalInputExcludesPriorSummaryFromNewTurns(t *testing.T) {
	const previousSummary = "previous summary should appear once"
	previousSummaryMsg := contextplane.MarkCompressionSummary(schema.UserMessage(previousSummary))

	input, err := buildSummarizerInput(previousSummary, []adk.Message{
		schema.SystemMessage("system"),
		previousSummaryMsg,
		schema.UserMessage("new work to incorporate"),
		schema.AssistantMessage("new result to incorporate", nil),
		schema.UserMessage("recent tail stays verbatim"),
		schema.AssistantMessage("recent tail response", nil),
	}, contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true})
	if err != nil {
		t.Fatalf("buildSummarizerInput: %v", err)
	}
	if got, want := len(input), 2; got != want {
		t.Fatalf("input messages = %d, want %d", got, want)
	}
	prompt := input[1].Content
	if got := strings.Count(prompt, previousSummary); got != 1 {
		t.Fatalf("previous summary occurrence count = %d, want 1 in prompt:\n%s", got, prompt)
	}
	if !strings.Contains(prompt, "new work to incorporate") {
		t.Fatalf("new turn missing from incremental prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "recent tail stays verbatim") {
		t.Fatalf("recent tail leaked into incremental summarizer prompt:\n%s", prompt)
	}
}

func TestRedactSecretsNeverEchoesCapturedValues(t *testing.T) {
	input := strings.Join([]string{
		"password=secretpass",
		"api_key: sk-proj-secretvalue",
		"Authorization: Bearer bearer-secret",
		"postgres://user:dbsecret@example.test/db?token=querysecret",
		"-----BEGIN PRIVATE KEY-----\nprivate-secret\n-----END PRIVATE KEY-----",
	}, "\n")

	got := contextplane.RedactSecrets(input)
	assertNoSecretText(t, "redacted text", got)
	for _, want := range []string{"password=[REDACTED]", "api_key: [REDACTED]", "Authorization: [REDACTED]", "postgres://user:[REDACTED]@example.test/db?token=[REDACTED]", "[REDACTED:private-key]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted text missing %q:\n%s", want, got)
		}
	}
}

func assertNoSecretText(t *testing.T, label, value string) {
	t.Helper()
	for _, secret := range []string{"secretpass", "token123", "bearer-secret", "dbsecret", "querysecret", "private-secret", "sk-proj-secretvalue"} {
		if strings.Contains(value, secret) {
			t.Fatalf("%s contains secret %q:\n%s", label, secret, value)
		}
	}
}

func compressionEnabledTestConfig() config.ContextConfig {
	return config.ContextConfig{
		WindowTokens:         200000,
		CompactMarginTokens:  13000,
		PreserveRecentTurns:  2,
		SummaryMaxTokens:     2048,
		ReservedOutputTokens: 4096,
		TokenEncoding:        "o200k_base",
	}
}

func structuredCompressionSummary(marker string) string {
	return strings.Join([]string{
		"### Primary Request / Intent",
		"Continue the current task. " + marker,
		"",
		"### Current Work",
		"Context protocol, compaction, structured continuation summary. Files: internal/contextplane/compression.go. Errors: none. Problem Solving: conversation history was compacted into a continuation checkpoint. Pending: continue verification.",
		"",
		"### Next Step",
		"Run the next validation command.",
	}, "\n")
}

func compressionExpectedTokens(t *testing.T, encoding string, messages []adk.Message, tools []*schema.ToolInfo) int {
	t.Helper()
	if err := ensureCompressionTokenLoader(); err != nil {
		t.Fatalf("ensureCompressionTokenLoader: %v", err)
	}
	encoder, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		t.Fatalf("GetEncoding(%q): %v", encoding, err)
	}
	total := 0
	for _, msg := range messages {
		payload, err := json.Marshal(normalizeCompressionMessage(msg))
		if err != nil {
			t.Fatalf("marshal message: %v", err)
		}
		total += len(encoder.Encode(string(payload), nil, nil))
	}
	for _, tool := range tools {
		payload, err := json.Marshal(normalizeCompressionTool(tool))
		if err != nil {
			t.Fatalf("marshal tool: %v", err)
		}
		total += len(encoder.Encode(string(payload), nil, nil))
	}
	return total
}

type fakeCompressionChatModel struct {
	response      string
	toolCalls     []schema.ToolCall
	generateCalls int
	onGenerate    func(input []*schema.Message)
}

func (f *fakeCompressionChatModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	f.generateCalls++
	if f.onGenerate != nil {
		f.onGenerate(input)
	}
	return schema.AssistantMessage(f.response, f.toolCalls), nil
}

func (f *fakeCompressionChatModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("stream is not implemented in fakeCompressionChatModel")
}

func TestBuildHandoffFrame(t *testing.T) {
	messages := []adk.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("Fix the login bug in auth.go"),
		schema.AssistantMessage("I'll look into the login bug.", nil),
		schema.UserMessage("Also check the session timeout issue"),
	}

	frame := buildHandoffFrame(messages)
	if frame == "" {
		t.Fatal("expected non-empty handoff frame")
	}
	if !strings.Contains(frame, "<handoff-frame>") {
		t.Fatalf("frame missing <handoff-frame> tag: %q", frame)
	}
	if !strings.Contains(frame, "<current-intent>") {
		t.Fatalf("frame missing <current-intent> tag: %q", frame)
	}
	if !strings.Contains(frame, "session timeout issue") {
		t.Fatalf("frame should contain last user message: %q", frame)
	}
	if !strings.Contains(frame, "</handoff-frame>") {
		t.Fatalf("frame missing closing tag: %q", frame)
	}
}

func TestBuildHandoffFrameWithPendingItems(t *testing.T) {
	messages := []adk.Message{
		schema.UserMessage("implement the feature"),
		schema.AssistantMessage("I started the implementation. TODO: add error handling. FIXME: race condition in handler.", nil),
		schema.UserMessage("continue"),
	}

	frame := buildHandoffFrame(messages)
	if frame == "" {
		t.Fatal("expected non-empty handoff frame")
	}
	if !strings.Contains(frame, "<pending-items>") {
		t.Fatalf("frame missing <pending-items> tag: %q", frame)
	}
	if !strings.Contains(frame, "TODO") {
		t.Fatalf("frame should contain TODO: %q", frame)
	}
	if !strings.Contains(frame, "FIXME") {
		t.Fatalf("frame should contain FIXME: %q", frame)
	}
}

func TestBuildHandoffFrameKeyVariables(t *testing.T) {
	messages := []adk.Message{
		schema.UserMessage("debug the crash in /app/main.go"),
		schema.AssistantMessage("The crash happens in processRequest(). error: nil pointer dereference at line 42", nil),
	}

	frame := buildHandoffFrame(messages)
	if frame == "" {
		t.Fatal("expected non-empty handoff frame")
	}
	if !strings.Contains(frame, "<key-variables>") {
		t.Fatalf("frame missing <key-variables> tag: %q", frame)
	}
	if !strings.Contains(frame, "/app/main.go") {
		t.Fatalf("frame should contain file path: %q", frame)
	}
	if !strings.Contains(frame, "processRequest()") {
		t.Fatalf("frame should contain function name: %q", frame)
	}
	if !strings.Contains(frame, "nil pointer dereference") {
		t.Fatalf("frame should contain error message: %q", frame)
	}
}

func TestBuildHandoffFrameEmptyConversation(t *testing.T) {
	frame := buildHandoffFrame(nil)
	if frame != "" {
		t.Fatalf("expected empty string for nil messages, got %q", frame)
	}

	frame = buildHandoffFrame([]adk.Message{})
	if frame != "" {
		t.Fatalf("expected empty string for empty messages, got %q", frame)
	}
}

func TestBuildHandoffFrameOnlySystemMessages(t *testing.T) {
	messages := []adk.Message{
		schema.SystemMessage("system instruction"),
	}
	frame := buildHandoffFrame(messages)
	if frame != "" {
		t.Fatalf("expected empty string when only system messages present, got %q", frame)
	}
}

func TestCompressionFinalizerAppendsHandoffFrame(t *testing.T) {
	cfg := compressionEnabledTestConfig()
	cfg.PreserveRecentTurns = 1

	originalMessages := []adk.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("Fix the bug in /src/app.go"),
		schema.AssistantMessage("Looking at the bug. TODO: add tests.", nil),
		schema.UserMessage("Also check the timeout"),
		schema.AssistantMessage("Checking timeout now.", nil),
	}
	result := compactTestMessages(t, cfg, originalMessages, structuredCompressionSummary("Summary of previous conversation"))

	summaryText := summaryMessageText(result.Summary)
	if !strings.Contains(summaryText, "<handoff-frame>") {
		t.Fatalf("summary should contain handoff frame (default enabled), got: %q", summaryText)
	}
	if !strings.Contains(summaryText, "Also check the timeout") {
		t.Fatalf("handoff frame should contain current intent, got: %q", summaryText)
	}
	if !strings.Contains(summaryText, "TODO") {
		t.Fatalf("handoff frame should contain pending items, got: %q", summaryText)
	}
}

func TestCompressionFinalizerNoHandoffWhenDisabled(t *testing.T) {
	cfg := compressionEnabledTestConfig()
	cfg.HandoffFrameDisabled = true
	cfg.PreserveRecentTurns = 1

	originalMessages := []adk.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("Fix the bug in /src/app.go"),
		schema.AssistantMessage("Looking at the bug. TODO: add tests.", nil),
		schema.UserMessage("Also check the timeout"),
		schema.AssistantMessage("Checking timeout now.", nil),
	}
	result := compactTestMessages(t, cfg, originalMessages, structuredCompressionSummary("Summary of previous conversation"))

	summaryText := summaryMessageText(result.Summary)
	if strings.Contains(summaryText, "<handoff-frame>") {
		t.Fatalf("summary should NOT contain handoff frame when disabled, got: %q", summaryText)
	}
	if !strings.Contains(summaryText, "Summary of previous conversation") {
		t.Fatalf("summary should keep model content when handoff disabled, got: %q", summaryText)
	}
}

func TestCompressionFinalizerHandoffDefaultEnabled(t *testing.T) {
	cfg := compressionEnabledTestConfig()
	cfg.PreserveRecentTurns = 1
	// HandoffFrameDisabled is false (zero value) by default, meaning enabled.

	originalMessages := []adk.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("Initial bug report"),
		schema.AssistantMessage("Initial investigation started.", nil),
		schema.UserMessage("Fix the bug"),
		schema.AssistantMessage("Working on it. TODO: add tests.", nil),
	}
	result := compactTestMessages(t, cfg, originalMessages, structuredCompressionSummary("Summary"))

	summaryText := summaryMessageText(result.Summary)
	if !strings.Contains(summaryText, "<handoff-frame>") {
		t.Fatalf("zero-value HandoffFrameDisabled should produce handoff frame (default enabled), got: %q", summaryText)
	}
}

func compactTestMessages(t *testing.T, cfg config.ContextConfig, messages []adk.Message, response string) *CompactionResult {
	t.Helper()
	counter, err := contextplane.NewCompressionTokenCounter(cfg)
	if err != nil {
		t.Fatalf("contextplane.NewCompressionTokenCounter: %v", err)
	}
	engine := NewDefaultCompactionEngine(CompactionEngineOptions{
		Model:                &fakeCompressionChatModel{response: response},
		ModelOptions:         []einomodel.Option{einomodel.WithMaxTokens(cfg.SummaryMaxTokens)},
		TokenCounter:         counter,
		HandoffFrameDisabled: cfg.HandoffFrameDisabled,
	})
	result, err := engine.Compact(context.Background(), CompactRequest{
		Trigger:  contextplane.CompactTriggerAuto,
		Messages: messages,
		Pressure: contextplane.BudgetPressure{
			State: contextplane.PressureAutoCompact,
		},
		PreservePolicy: contextplane.PreservePolicy{
			RecentTurns:       cfg.PreserveRecentTurns,
			PreserveToolPairs: true,
		},
	})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	return result
}
