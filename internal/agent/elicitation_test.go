package agent

import (
	"bytes"
	"encoding/gob"
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

func TestElicitationInterruptStateGobRoundTrip(t *testing.T) {
	original := ElicitationInterruptState{
		ActionID: "action_1234567890",
	}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(original); err != nil {
		t.Fatalf("encode ElicitationInterruptState: %v", err)
	}
	dec := gob.NewDecoder(&buf)
	var decoded ElicitationInterruptState
	if err := dec.Decode(&decoded); err != nil {
		t.Fatalf("decode ElicitationInterruptState: %v", err)
	}
	if decoded.ActionID != original.ActionID {
		t.Errorf("ActionID = %q, want %q", decoded.ActionID, original.ActionID)
	}
}

func TestStreamKindElicitationConstants(t *testing.T) {
	tests := []struct {
		constant domain.StreamItemKind
		want     string
	}{
		{domain.StreamKindElicitationPending, "elicitation.pending"},
		{domain.StreamKindElicitationDecided, "elicitation.decided"},
	}
	for _, tt := range tests {
		if tt.constant != domain.StreamItemKind(tt.want) {
			t.Errorf("constant = %q, want %q", tt.constant, tt.want)
		}
	}
}
