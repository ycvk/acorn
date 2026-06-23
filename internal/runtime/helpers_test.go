package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

// TestBuildExecutionContextPropagatesTurnIndexToReader guards the turn-index wiring
// bug: buildExecutionContext must set the turn index under the SAME context key that
// the tool lifecycle reads via domain.TurnIndexFromContext. Previously the executor
// used a runtime-local setter writing a different key type, so the reader always saw 0
// and tool-result TurnIndex was silently 0 in production for multi-turn sessions.
func TestBuildExecutionContextPropagatesTurnIndexToReader(t *testing.T) {
	ctx := buildExecutionContext(context.Background(), "run_x", "sess_x", 7, nil)
	if got := domain.TurnIndexFromContext(ctx); got != 7 {
		t.Fatalf("turn index from context = %d, want 7 (executor must set the key the lifecycle reader reads)", got)
	}
	if got := domain.GetRunID(ctx); got != "run_x" {
		t.Fatalf("run id from context = %q, want run_x", got)
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
