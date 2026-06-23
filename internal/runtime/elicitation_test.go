package runtime

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
