package contextplane

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestCompressionOutcomeIncludesLayersApplied(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("summary")},
			SummaryText: "summary",
			Outcome: CompressionOutcome{
				BoundaryID:     "ctxb_test",
				TokensBefore:   100,
				TokensAfter:    40,
				Summary:        "summary",
				SummarySnippet: "summary",
			},
		},
	}
	pipeline := NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
		Governor:         testBudgetGovernor{pressure: testPressure(PressureBlocking), dynamic: true},
		CompactionEngine: engine,
		TokenCounter:     testTokenCounter(t),
	})

	var captured CompressionOutcome
	session := NewDefaultContextSession(ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(PressureBlocking), dynamic: true},
		Pipeline:       pipeline,
		PreservePolicy: PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
		EmitCompressed: func(_ context.Context, outcome CompressionOutcome) error {
			captured = outcome
			return nil
		},
	})
	_, err := session.Bootstrap(context.Background(), BootstrapRequest{
		SessionID: "session_1",
		RunID:     "run_1",
		Mode:      "direct_response",
		InitialMessages: []adk.Message{
			schema.UserMessage("old 1"),
			schema.AssistantMessage("old resp 1", nil),
			schema.UserMessage("old 2"),
			schema.AssistantMessage("old resp 2", nil),
			schema.UserMessage("recent"),
			schema.AssistantMessage("recent resp", nil),
		},
		ModelProfile: testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_, err = session.BeforeModelCall(context.Background(), ModelCallRequest{
		CallID:       "call_1",
		AllowCompact: true,
	})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	if captured.BoundaryID != "ctxb_test" {
		t.Fatalf("boundary id = %q, want ctxb_test", captured.BoundaryID)
	}
	if len(captured.LayersApplied) == 0 {
		t.Fatal("LayersApplied is empty")
	}
	if captured.LayersApplied[len(captured.LayersApplied)-1] != CompactLayerAutocompact {
		t.Fatalf("last layer = %v, want autocompact", captured.LayersApplied)
	}
}
