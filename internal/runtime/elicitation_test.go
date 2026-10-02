package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestStreamKindElicitationConstants(t *testing.T) {
	tests := []struct {
		constant core.StreamItemKind
		want     string
	}{
		{core.StreamKindElicitationPending, "elicitation.pending"},
		{core.StreamKindElicitationDecided, "elicitation.decided"},
	}
	for _, tt := range tests {
		if tt.constant != core.StreamItemKind(tt.want) {
			t.Errorf("constant = %q, want %q", tt.constant, tt.want)
		}
	}
}
