package compaction

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/store/storetest"
)

func TestCompressionOutcomeIncludesLayersApplied(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("summary")},
			SummaryText: "summary",
			Outcome: contextplane.CompressionOutcome{
				BoundaryID:     "ctxb_test",
				TokensBefore:   100,
				TokensAfter:    40,
				Summary:        "summary",
				SummarySnippet: "summary",
			},
		},
	}
	pipeline := NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
		Governor:         testBudgetGovernor{pressure: testPressure(contextplane.PressureBlocking), dynamic: true},
		CompactionEngine: engine,
		TokenCounter:     testTokenCounter(t),
	})

	store := storetest.NewFakeContextStore()
	session := contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		BudgetGovernor: testBudgetGovernor{pressure: testPressure(contextplane.PressureBlocking), dynamic: true},
		Pipeline:       pipeline,
		BoundaryStore:  store,
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
	})
	_, err := session.Bootstrap(context.Background(), contextplane.BootstrapRequest{
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
	_, err = session.BeforeModelCall(context.Background(), contextplane.ModelCallRequest{
		CallID:       "call_1",
		AllowCompact: true,
	})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	latest, err := store.LoadLatestContextBoundary(context.Background(), "session_1")
	if err != nil {
		t.Fatalf("LoadLatestContextBoundary: %v", err)
	}
	if latest == nil || latest.BoundaryID != "ctxb_run_1_0001" {
		t.Fatalf("latest boundary = %+v, want ctxb_run_1_0001", latest)
	}
}
