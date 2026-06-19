package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/store/storetest"
)

func TestBuildSummarizerInputUsesPreviousSummary(t *testing.T) {
	messages := []adk.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("old request"),
		schema.AssistantMessage("old response", nil),
		schema.UserMessage("new request"),
		schema.AssistantMessage("new response", nil),
		schema.UserMessage("most recent request"),
		schema.AssistantMessage("most recent response", nil),
	}

	input1, err := buildSummarizerInput("", messages, contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true})
	if err != nil {
		t.Fatalf("buildSummarizerInput without previous: %v", err)
	}
	if len(input1) != 2 {
		t.Fatalf("input without previous = %d messages, want 2", len(input1))
	}
	if strings.Contains(input1[1].Content, "previous summary") {
		t.Fatal("full summary prompt should not mention previous summary")
	}

	previousSummary := "previous work: completed task A"
	input2, err := buildSummarizerInput(previousSummary, messages, contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true})
	if err != nil {
		t.Fatalf("buildSummarizerInput with previous: %v", err)
	}
	if len(input2) != 2 {
		t.Fatalf("input with previous = %d messages, want 2", len(input2))
	}
	if !strings.Contains(input2[1].Content, previousSummary) {
		t.Fatalf("incremental prompt missing previous summary: %q", input2[1].Content)
	}
	if !strings.Contains(input2[1].Content, "new request") {
		t.Fatalf("incremental prompt missing new content: %q", input2[1].Content)
	}
}

func TestContextSessionPassesPreviousSummary(t *testing.T) {
	state := contextplane.NewCompressionState()
	state.LastSummary = "previous summary checkpoint"
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("updated summary")},
			SummaryText: "updated summary",
			Outcome: contextplane.CompressionOutcome{
				BoundaryID:     "ctxb_2",
				TokensBefore:   120,
				TokensAfter:    40,
				Summary:        "updated summary",
				SummarySnippet: "updated summary",
			},
		},
	}
	governor := testBudgetGovernor{pressure: testPressure(contextplane.PressureAutoCompact), dynamic: true}
	session := contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		BudgetGovernor: governor,
		Pipeline: NewDefaultContextCompressionPipeline(CompressionPipelineOptions{
			Governor:         governor,
			CompactionEngine: engine,
			TokenCounter:     testTokenCounter(t),
		}),
		BoundaryStore:  storetest.NewFakeContextStore(),
		PreservePolicy: contextplane.PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
		State:          state,
	})
	_, err := session.Bootstrap(context.Background(), contextplane.BootstrapRequest{
		SessionID: "session_1",
		RunID:     "run_1",
		Mode:      "direct_response",
		InitialMessages: []adk.Message{
			schema.UserMessage("old request 1"),
			schema.AssistantMessage("old response 1", nil),
			schema.UserMessage("old request 2"),
			schema.AssistantMessage("old response 2", nil),
			schema.UserMessage("recent request"),
			schema.AssistantMessage("recent response", nil),
		},
		ModelProfile: testContextSessionProfile(),
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	_, err = session.BeforeModelCall(context.Background(), contextplane.ModelCallRequest{
		CallID:       "call_1",
		QuerySource:  "direct_response",
		AllowCompact: true,
	})
	if err != nil {
		t.Fatalf("BeforeModelCall: %v", err)
	}
	if !engine.called {
		t.Fatal("compaction engine was not called")
	}
	if engine.request.PreviousSummary != "previous summary checkpoint" {
		t.Fatalf("previous summary = %q, want 'previous summary checkpoint'", engine.request.PreviousSummary)
	}
}
