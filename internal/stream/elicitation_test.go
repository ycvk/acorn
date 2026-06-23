package stream

import (
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

func TestProjectStreamItemToEventElicitationKinds(t *testing.T) {
	tests := []struct {
		name     string
		kind     domain.StreamItemKind
		wantKind string
	}{
		{"elicitation pending maps to elicitation.pending", domain.StreamKindElicitationPending, "elicitation.pending"},
		{"elicitation decided maps to elicitation.decided", domain.StreamKindElicitationDecided, "elicitation.decided"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := domain.StreamItem{
				Kind:    tt.kind,
				Payload: map[string]any{},
			}
			gotKind, _, err := ProjectStreamItemToEvent(item)
			if err != nil {
				t.Fatalf("ProjectStreamItemToEvent: %v", err)
			}
			if gotKind != tt.wantKind {
				t.Errorf("ProjectStreamItemToEvent(%q) kind = %q, want %q", tt.kind, gotKind, tt.wantKind)
			}
		})
	}
}
