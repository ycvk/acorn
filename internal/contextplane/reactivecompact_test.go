package contextplane

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestReactiveCompactEngineHalvesRecentTurns(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("reactive summary")},
			SummaryText: "reactive summary",
			Outcome: CompressionOutcome{
				BoundaryID:     "ctxb_reactive",
				TokensBefore:   120,
				TokensAfter:    40,
				Summary:        "reactive summary",
				SummarySnippet: "reactive summary",
			},
		},
	}
	reactive := newReactiveCompactEngine(engine)

	result, err := reactive.Recover(context.Background(), ReactiveCompactRequest{
		Messages: []adk.Message{
			schema.SystemMessage("system"),
			schema.UserMessage("old 1"),
			schema.AssistantMessage("old resp 1", nil),
			schema.UserMessage("old 2"),
			schema.AssistantMessage("old resp 2", nil),
			schema.UserMessage("recent"),
			schema.AssistantMessage("recent resp", nil),
		},
		PreservePolicy: PreservePolicy{RecentTurns: 4, PreserveToolPairs: true},
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !result.Recovered {
		t.Fatal("expected Recovered=true")
	}
	if engine.request.PreservePolicy.RecentTurns != 2 {
		t.Fatalf("recent turns = %d, want 2 (halved from 4)", engine.request.PreservePolicy.RecentTurns)
	}
	if engine.request.Trigger != CompactTriggerReactive {
		t.Fatalf("trigger = %q, want reactive", engine.request.Trigger)
	}
}

func TestReactiveCompactEngineKeepsAtLeastOneRecentTurn(t *testing.T) {
	engine := &testCompactionEngine{
		result: &CompactionResult{
			Messages:    []adk.Message{schema.SystemMessage("system"), schema.UserMessage("summary")},
			SummaryText: "summary",
		},
	}
	reactive := newReactiveCompactEngine(engine)

	_, err := reactive.Recover(context.Background(), ReactiveCompactRequest{
		Messages: []adk.Message{
			schema.UserMessage("recent"),
			schema.AssistantMessage("recent resp", nil),
		},
		PreservePolicy: PreservePolicy{RecentTurns: 1, PreserveToolPairs: true},
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if engine.request.PreservePolicy.RecentTurns != 1 {
		t.Fatalf("recent turns = %d, want 1 (min enforced)", engine.request.PreservePolicy.RecentTurns)
	}
}

func TestReactiveCompactEngineReturnsErrorWhenNil(t *testing.T) {
	var r *defaultReactiveCompactEngine
	_, err := r.Recover(context.Background(), ReactiveCompactRequest{})
	if err == nil {
		t.Fatal("expected error for nil engine")
	}
}
