package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

// TestBuildExecutionContextPropagatesTurnIndexToReader guards the turn-index wiring
// bug: buildExecutionContext must set the turn index under the SAME context key that
// the tool lifecycle reads via runtimeapi.TurnIndexFromContext. Previously the executor
// used a runtime-local setter writing a different key type, so the reader always saw 0
// and tool-result TurnIndex was silently 0 in production for multi-turn sessions.
func TestBuildExecutionContextPropagatesTurnIndexToReader(t *testing.T) {
	ctx := buildExecutionContext(context.Background(), "run_x", "sess_x", 7, nil)
	if got := runtimeapi.TurnIndexFromContext(ctx); got != 7 {
		t.Fatalf("turn index from context = %d, want 7 (executor must set the key the lifecycle reader reads)", got)
	}
	if got := runtimeapi.GetRunID(ctx); got != "run_x" {
		t.Fatalf("run id from context = %q, want run_x", got)
	}
}

func TestMessageToMapPreservesToolContent(t *testing.T) {
	msg := &schema.Message{
		Role:    schema.Tool,
		Content: strings.Repeat("a", 1200),
	}
	message := stream.StreamMessageFromSchema(msg, "")
	content := message.Content
	if len(content) != 1200 {
		t.Fatalf("expected full tool content, got len=%d", len(content))
	}
	if len(message.Meta) > 0 {
		t.Fatalf("expected no meta, got %#v", message.Meta)
	}
}

func TestCompactText(t *testing.T) {
	short, truncated := compactText("  hello  ", 10)
	if short != "hello" || truncated {
		t.Fatalf("unexpected compactText short result: %q %v", short, truncated)
	}
	long, truncated := compactText(strings.Repeat("b", 20), 5)
	if !truncated || long != "bbbbb..." {
		t.Fatalf("unexpected compactText long result: %q %v", long, truncated)
	}
}
