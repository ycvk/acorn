package contextplane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/model"
)

func TestContextSessionBootstrapOrdersAssemblyBeforeInitialMessages(t *testing.T) {
	session := newTestContextSession(t)
	input, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "direct_response",
		InitialMessages: []adk.Message{schema.UserMessage("user request")},
		Assembly: &AssembleResult{Messages: []*schema.Message{
			schema.UserMessage("memory context"),
			schema.UserMessage("skill context"),
		}},
		ModelProfile: testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	got := messageContents(input.Messages)
	want := []string{"memory context", "skill context", "user request"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("messages = %v, want %v", got, want)
	}
	if session.ID().SessionID != "session_1" || session.ID().RunID != "run_1" || session.ID().Mode != "direct_response" {
		t.Fatalf("unexpected session id: %+v", session.ID())
	}
}

func TestContextSessionModelInputReturnsCopies(t *testing.T) {
	session := newTestContextSession(t)
	input, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "single_agent",
		InitialMessages: []adk.Message{schema.UserMessage("original")},
		ModelProfile:    testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	input.Messages[0].Content = "mutated outside"
	next, err := session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_1"})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	if got := next.Messages[0].Content; got != "original" {
		t.Fatalf("session message = %q, want original", got)
	}
}

func TestContextSessionRecordsAssistantAndToolResults(t *testing.T) {
	session := newTestContextSession(t)
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "direct_response",
		InitialMessages: []adk.Message{schema.UserMessage("request")},
		ModelProfile:    testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if err := session.RecordAssistant(context.Background(), schema.AssistantMessage("assistant", nil)); err != nil {
		t.Fatalf("RecordAssistant: %v", err)
	}
	if err := session.RecordToolResults(context.Background(), []adk.Message{schema.ToolMessage("result", "call_1")}); err != nil {
		t.Fatalf("RecordToolResults: %v", err)
	}
	input, err := session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_2"})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	got := messageContents(input.Messages)
	want := []string{"request", "assistant", "result"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("messages = %v, want %v", got, want)
	}
}

func TestContextSessionBeforeModelCallEmitsPressure(t *testing.T) {
	var pressures []BudgetPressure
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(PressureWarning)},
		EmitPressure: func(_ context.Context, pressure BudgetPressure) error {
			pressures = append(pressures, pressure)
			return nil
		},
	})
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "direct_response",
		InitialMessages: []adk.Message{schema.UserMessage("request")},
		ModelProfile:    testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_, err = session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_1"})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	if len(pressures) != 1 || pressures[0].State != PressureWarning {
		t.Fatalf("pressures = %+v, want one warning pressure", pressures)
	}
}

func TestContextSessionContextBinding(t *testing.T) {
	session := newTestContextSession(t)
	ctx := WithContextSession(context.Background(), session)
	if got := ContextSessionFromContext(ctx); got != session {
		t.Fatalf("ContextSessionFromContext = %v, want bound session", got)
	}
	if got := ContextSessionFromContext(context.Background()); got != nil {
		t.Fatalf("ContextSessionFromContext without binding = %v, want nil", got)
	}
}

func TestContextSessionBeforeModelCallCompactsOnPressure(t *testing.T) {
	state := NewCompressionState()
	pipeline := &testCompressionPipeline{
		result: &PipelineResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("summary checkpoint")},
			TokensFreed: 80,
			Outcome: &CompressionOutcome{
				BoundaryID:     "ctxb_1",
				TokensBefore:   100,
				TokensAfter:    20,
				Summary:        "summary checkpoint",
				SummarySnippet: "summary checkpoint",
			},
		},
	}
	var outcomes []CompressionOutcome
	store := newFakeContextStore()
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(PressureAutoCompact)},
		Pipeline:       pipeline,
		BoundaryStore:  store,
		PreservePolicy: PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
		State:          state,
		EmitCompressed: func(_ context.Context, outcome CompressionOutcome) error {
			outcomes = append(outcomes, outcome)
			return nil
		},
	})
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "direct_response",
		InitialMessages: []adk.Message{schema.UserMessage("large request")},
		ModelProfile:    testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	input, err := session.BeforeModelCall(context.Background(), ModelCallRequest{
		CallID:       "call_1",
		QuerySource:  "direct_response",
		AllowCompact: true,
		ToolInfos: []*schema.ToolInfo{
			{Name: "lookup"},
		},
		ToolState: &ToolLifecycleState{RunID: "run_1", SessionID: "session_1"},
	})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	if !pipeline.called {
		t.Fatal("compression pipeline was not called")
	}
	if pipeline.request.Trigger != CompactTriggerAuto {
		t.Fatalf("trigger = %q, want auto", pipeline.request.Trigger)
	}
	if len(pipeline.request.ToolInfos) != 1 || pipeline.request.ToolInfos[0].Name != "lookup" {
		t.Fatalf("tool infos = %#v, want lookup", pipeline.request.ToolInfos)
	}
	if pipeline.request.ToolState == nil || pipeline.request.ToolState.RunID != "run_1" {
		t.Fatalf("tool state = %#v, want run_1", pipeline.request.ToolState)
	}
	if got := messageContents(input.Messages); strings.Join(got, "|") != "system|summary checkpoint" {
		t.Fatalf("messages = %v, want compacted checkpoint", got)
	}
	if state.CompressionCount != 1 || state.LastSummary != "summary checkpoint" {
		t.Fatalf("compression state = %+v, want recorded summary", state)
	}
	wantBoundaryID := "ctxb_run_1_0001"
	if len(outcomes) != 1 || outcomes[0].BoundaryID != wantBoundaryID {
		t.Fatalf("outcomes = %+v, want emitted boundary", outcomes)
	}
	boundaries, err := store.ListContextBoundaries(context.Background(), "session_1")
	if err != nil {
		t.Fatalf("ListContextBoundaries: %v", err)
	}
	if len(boundaries) != 1 || boundaries[0].BoundaryID != wantBoundaryID || boundaries[0].Summary != "summary checkpoint" {
		t.Fatalf("boundaries = %+v, want persisted compact boundary", boundaries)
	}
}

func TestContextSessionBeforeModelCallFailsWhenCompactionRequiredButDisabled(t *testing.T) {
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(PressureBlocking)},
	})
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "direct_response",
		InitialMessages: []adk.Message{schema.UserMessage("large request")},
		ModelProfile:    testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_, err = session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_1", AllowCompact: false})
	if err == nil || !strings.Contains(err.Error(), "requires compaction but compact is disabled") {
		t.Fatalf("error = %v, want compact disabled error", err)
	}
}

func TestContextSessionBeforeModelCallFailsWhenEngineMissing(t *testing.T) {
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(PressureAutoCompact)},
	})
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "direct_response",
		InitialMessages: []adk.Message{schema.UserMessage("large request")},
		ModelProfile:    testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_, err = session.BeforeModelCall(context.Background(), ModelCallRequest{CallID: "call_1", AllowCompact: true})
	if err == nil || !strings.Contains(err.Error(), "compression pipeline is required") {
		t.Fatalf("error = %v, want missing pipeline error", err)
	}
}

func TestContextSessionReactiveCompactUsesReactiveTrigger(t *testing.T) {
	state := NewCompressionState()
	pipeline := &testCompressionPipeline{
		result: &PipelineResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("reactive summary")},
			TokensFreed: 80,
			Outcome: &CompressionOutcome{
				BoundaryID:     "ctxb_reactive",
				TokensBefore:   120,
				TokensAfter:    40,
				Summary:        "reactive summary",
				SummarySnippet: "reactive summary",
			},
		},
	}
	var outcomes []CompressionOutcome
	governor := testBudgetGovernor{pressure: testPressure(PressureBlocking), dynamic: true}
	store := newFakeContextStore()
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: governor,
		Pipeline:       pipeline,
		BoundaryStore:  store,
		PreservePolicy: PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
		State:          state,
		EmitCompressed: func(_ context.Context, outcome CompressionOutcome) error {
			outcomes = append(outcomes, outcome)
			return nil
		},
	})
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID: "session_1",
		RunID:     "run_1",
		Mode:      "direct_response",
		InitialMessages: []adk.Message{
			schema.UserMessage("old request 1"),
			schema.AssistantMessage("old response 1", nil),
			schema.UserMessage("old request 2"),
			schema.AssistantMessage("old response 2", nil),
			schema.UserMessage("request"),
		},
		ModelProfile: testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	input, err := session.ReactiveCompact(context.Background(), ModelCallRequest{
		CallID:       "call_1",
		QuerySource:  "direct_response",
		AllowCompact: true,
	}, errors.New("model_context_window_exceeded"))
	if err != nil {
		t.Fatalf("ReactiveCompact: %v", err)
	}
	if !pipeline.called {
		t.Fatal("compression pipeline was not called")
	}
	if pipeline.request.Trigger != CompactTriggerReactive {
		t.Fatalf("trigger = %q, want reactive", pipeline.request.Trigger)
	}
	if got := messageContents(input.Messages); strings.Join(got, "|") != "system|reactive summary" {
		t.Fatalf("messages = %v, want reactive summary", got)
	}
	if state.CompressionCount != 1 || state.LastSummary != "reactive summary" {
		t.Fatalf("compression state = %+v, want reactive summary", state)
	}
	wantBoundaryID := "ctxb_run_1_0001"
	if len(outcomes) != 1 || outcomes[0].BoundaryID != wantBoundaryID {
		t.Fatalf("outcomes = %+v, want reactive boundary", outcomes)
	}
}

func TestContextSessionReactiveCompactRejectsNonOverflowCause(t *testing.T) {
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(PressureOK)},
	})
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID:       "session_1",
		RunID:           "run_1",
		Mode:            "direct_response",
		InitialMessages: []adk.Message{schema.UserMessage("request")},
		ModelProfile:    testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_, err = session.ReactiveCompact(context.Background(), ModelCallRequest{
		CallID:       "call_1",
		AllowCompact: true,
	}, errors.New("rate limit exceeded"))
	if err == nil || !strings.Contains(err.Error(), "requires context overflow") {
		t.Fatalf("error = %v, want non-overflow rejection", err)
	}
}

func TestIsContextOverflowError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("context_length_exceeded: maximum context length is 128000 tokens"), true},
		{fmtWrapped("provider failed: %w", errors.New("model_context_window_exceeded")), true},
		{errors.New("prompt too long for this model"), true},
		{errors.New("rate limit exceeded"), false},
		{errors.New("token budget exceeded"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := IsContextOverflowError(tc.err); got != tc.want {
			t.Fatalf("IsContextOverflowError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestContextSessionBootstrapRejectsMissingIdentity(t *testing.T) {
	_, err := newTestContextSession(t).Bootstrap(context.Background(), BootstrapRequest{
		RunID:        "run_1",
		Mode:         "direct_response",
		ModelProfile: testContextSessionProfile(),
	})
	if err == nil || !strings.Contains(err.Error(), "context session id is required") {
		t.Fatalf("error = %v, want session id required", err)
	}
}

func TestContextSessionRequiresBudgetGovernor(t *testing.T) {
	_, err := NewDefaultContextSession(ContextSessionOptions{}).Bootstrap(context.Background(), BootstrapRequest{
		SessionID:    "session_1",
		RunID:        "run_1",
		Mode:         "direct_response",
		ModelProfile: testContextSessionProfile(),
	})
	if err == nil || !strings.Contains(err.Error(), "budget governor is required") {
		t.Fatalf("error = %v, want budget governor required", err)
	}
}

func TestContextSessionResumeLoadsPersistedBoundary(t *testing.T) {
	store := newFakeContextStore()
	boundary := testContextBoundary("ctxb_run_1_0001", "session_1", "run_1", 1, "resume summary checkpoint")
	if err := store.SaveContextBoundary(context.Background(), boundary); err != nil {
		t.Fatalf("SaveContextBoundary: %v", err)
	}
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(PressureOK)},
		BoundaryStore:  store,
	})
	input, err := session.Resume(context.Background(), ResumeContextRequest{
		SessionID:    "session_1",
		RunID:        "run_1",
		Mode:         "direct_response",
		BoundaryID:   boundary.BoundaryID,
		ModelProfile: testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if session.ID().SessionID != "session_1" || session.ID().RunID != "run_1" || session.ID().Mode != "direct_response" {
		t.Fatalf("unexpected session id after resume: %+v", session.ID())
	}
	if len(input.Messages) != 1 || !strings.Contains(summaryMessageText(input.Messages[0]), "resume summary checkpoint") {
		t.Fatalf("resume messages = %+v, want persisted summary checkpoint", input.Messages)
	}
}

func summaryMessageText(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	for _, part := range msg.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			return strings.TrimSpace(part.Text)
		}
	}
	return strings.TrimSpace(msg.Content)
}

func testContextBoundary(boundaryID, sessionID, runID string, sequence int, summary string) model.ContextBoundary {
	return model.ContextBoundary{
		BoundaryID:               boundaryID,
		SessionID:                sessionID,
		RunID:                    runID,
		Sequence:                 sequence,
		TurnIndex:                sequence,
		Mode:                     "direct_response",
		Trigger:                  string(CompactTriggerAuto),
		FirstIndex:               0,
		LastIndex:                1,
		CoveredFirstMessageID:    runID + ":message:0000",
		CoveredLastMessageID:     runID + ":message:0001",
		SummaryMessageID:         boundaryID + ":summary",
		TranscriptRef:            runID + ":messages:0-1",
		PreservedFromIndex:       1,
		PreservedToIndex:         1,
		PreservedHeadMessageID:   runID + ":message:0001",
		PreservedAnchorMessageID: runID + ":message:0001",
		PreservedTailMessageID:   runID + ":message:0001",
		TokensBefore:             100,
		TokensAfter:              40,
		EffectiveWindowTokens:    1000,
		Summary:                  summary,
		SummarySnippet:           summary,
		CreatedAt:                time.Now().UTC(),
	}
}

func newTestContextSession(t *testing.T) ContextSession {
	t.Helper()
	counter, err := NewCompressionTokenCounter(config.ContextConfig{TokenEncoding: "o200k_base"})
	if err != nil {
		t.Fatalf("NewCompressionTokenCounter: %v", err)
	}
	return NewDefaultContextSession(ContextSessionOptions{BudgetGovernor: NewBudgetGovernor(counter)})
}

func testContextSessionProfile() ModelProfile {
	return ModelProfile{
		ContextWindowTokens:         200000,
		ReservedOutputTokens:        4096,
		ReservedSummaryOutputTokens: 2048,
		StaticOverheadTokens:        4096,
		WarningBufferTokens:         20000,
		AutoCompactBufferTokens:     13000,
		BlockingBufferTokens:        3000,
	}
}

func messageContents(messages []adk.Message) []string {
	result := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			result = append(result, msg.Content)
		}
	}
	return result
}

type testBudgetGovernor struct {
	pressure BudgetPressure
	dynamic  bool
}

func (g testBudgetGovernor) Evaluate(_ context.Context, req BudgetEvaluateRequest) (BudgetPressure, error) {
	if g.dynamic && len(req.Messages) <= 3 {
		p := g.pressure
		p.State = PressureOK
		return p, nil
	}
	return g.pressure, nil
}

func (g testBudgetGovernor) AutoCompactThreshold(ModelProfile) (int, error) {
	return g.pressure.AutoCompactThresholdTokens, nil
}

func testPressure(state BudgetPressureState) BudgetPressure {
	return BudgetPressure{
		EstimatedInputTokens:       100,
		EffectiveWindowTokens:      1000,
		WarningThresholdTokens:     800,
		AutoCompactThresholdTokens: 900,
		BlockingThresholdTokens:    990,
		PercentUsed:                10,
		State:                      state,
	}
}

type testCompressionPipeline struct {
	called  bool
	request PipelineRequest
	result  *PipelineResult
	err     error
}

func (p *testCompressionPipeline) Compress(_ context.Context, req PipelineRequest) (*PipelineResult, error) {
	p.called = true
	p.request = req
	if p.err != nil {
		return nil, p.err
	}
	return p.result, nil
}

func fmtWrapped(format string, err error) error {
	return &wrappedTestError{format: format, err: err}
}

type wrappedTestError struct {
	format string
	err    error
}

func (e *wrappedTestError) Error() string {
	return strings.Replace(e.format, "%w", e.err.Error(), 1)
}

func (e *wrappedTestError) Unwrap() error {
	return e.err
}
