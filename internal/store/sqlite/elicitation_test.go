package sqlite

import (
	"testing"

	"github.com/ycvk/acorn/internal/events"
)

func TestNormalizePendingActionKindElicitation(t *testing.T) {
	tests := []events.PendingActionKind{
		events.PendingActionKindElicitation,
		events.PendingActionKindOperatorQuestion,
	}
	for _, want := range tests {
		got, err := normalizePendingActionKind(want)
		if err != nil {
			t.Fatalf("normalizePendingActionKind(%q) error: %v", want, err)
		}
		if got != want {
			t.Errorf("normalizePendingActionKind(%q) = %q, want %q", want, got, want)
		}
	}
}
