package store

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestNormalizePendingActionKindElicitation(t *testing.T) {
	tests := []core.PendingActionKind{
		core.PendingActionKindElicitation,
		core.PendingActionKindOperatorQuestion,
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
