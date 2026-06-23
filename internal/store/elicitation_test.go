package store

import (
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

func TestNormalizePendingActionKindElicitation(t *testing.T) {
	tests := []domain.PendingActionKind{
		domain.PendingActionKindElicitation,
		domain.PendingActionKindOperatorQuestion,
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
