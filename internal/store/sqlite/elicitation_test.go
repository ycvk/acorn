package sqlite

import (
	"testing"

	"github.com/ycvk/acorn/internal/events"
)

func TestNormalizePendingActionKindElicitation(t *testing.T) {
	got, err := normalizePendingActionKind(events.PendingActionKindElicitation)
	if err != nil {
		t.Fatalf("normalizePendingActionKind(%q) error: %v", events.PendingActionKindElicitation, err)
	}
	if got != events.PendingActionKindElicitation {
		t.Errorf("normalizePendingActionKind(%q) = %q, want %q", events.PendingActionKindElicitation, got, events.PendingActionKindElicitation)
	}
}
