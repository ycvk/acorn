package runtime

import (
	"bytes"
	"encoding/gob"
	"testing"
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

func TestStreamKindElicitationAndSamplingConstants(t *testing.T) {
	tests := []struct {
		constant StreamItemKind
		want     string
	}{
		{StreamKindElicitationPending, "elicitation.pending"},
		{StreamKindElicitationDecided, "elicitation.decided"},
		{StreamKindSamplingStarted, "sampling.started"},
		{StreamKindSamplingCompleted, "sampling.completed"},
		{StreamKindSamplingFailed, "sampling.failed"},
	}
	for _, tt := range tests {
		if tt.constant != StreamItemKind(tt.want) {
			t.Errorf("constant = %q, want %q", tt.constant, tt.want)
		}
	}
}

func TestProjectStreamItemToEventElicitationAndSamplingKinds(t *testing.T) {
	tests := []struct {
		name     string
		kind     StreamItemKind
		wantKind string
	}{
		{"elicitation pending maps to elicitation.pending", StreamKindElicitationPending, "elicitation.pending"},
		{"elicitation decided maps to elicitation.decided", StreamKindElicitationDecided, "elicitation.decided"},
		{"sampling started maps to sampling.started", StreamKindSamplingStarted, "sampling.started"},
		{"sampling completed maps to sampling.completed", StreamKindSamplingCompleted, "sampling.completed"},
		{"sampling failed maps to sampling.failed", StreamKindSamplingFailed, "sampling.failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := StreamItem{
				Kind:    tt.kind,
				Payload: &ElicitationPayload{},
			}
			gotKind, _ := mustProjectStreamItemToEvent(t, item)
			if gotKind != tt.wantKind {
				t.Errorf("projectStreamItemToEvent(%q) kind = %q, want %q", tt.kind, gotKind, tt.wantKind)
			}
		})
	}
}

func TestElicitationPayloadFromStream(t *testing.T) {
	t.Run("extracts typed ElicitationPayload", func(t *testing.T) {
		item := StreamItem{
			Kind: StreamKindElicitationPending,
			Payload: ElicitationPayload{
				ActionID: "action_123",
				Message:  "Please approve this action",
			},
		}
		p, err := ElicitationPayloadFromStream(item)
		if err != nil {
			t.Fatalf("ElicitationPayloadFromStream: %v", err)
		}
		if p.ActionID != "action_123" {
			t.Fatalf("ActionID = %q, want action_123", p.ActionID)
		}
		if p.Message != "Please approve this action" {
			t.Fatalf("Message = %q, want 'Please approve this action'", p.Message)
		}
		if p.StreamKind() != StreamKindElicitationPending {
			t.Fatalf("StreamKind = %q, want %q", p.StreamKind(), StreamKindElicitationPending)
		}
	})

	t.Run("extracts from pointer payload", func(t *testing.T) {
		item := StreamItem{
			Kind: StreamKindElicitationPending,
			Payload: &ElicitationPayload{
				ActionID: "action_456",
				Message:  "Operator input needed",
			},
		}
		p, err := ElicitationPayloadFromStream(item)
		if err != nil {
			t.Fatalf("ElicitationPayloadFromStream: %v", err)
		}
		if p.ActionID != "action_456" {
			t.Fatalf("ActionID = %q, want action_456", p.ActionID)
		}
		if p.Message != "Operator input needed" {
			t.Fatalf("Message = %q, want 'Operator input needed'", p.Message)
		}
		if p.StreamKind() != StreamKindElicitationPending {
			t.Fatalf("StreamKind = %q, want %q", p.StreamKind(), StreamKindElicitationPending)
		}
	})

	t.Run("preserves decided stream kind", func(t *testing.T) {
		item := StreamItem{
			Kind: StreamKindElicitationDecided,
			Payload: &ElicitationPayload{
				ActionID: "action_789",
				Message:  "Operator decided",
			},
		}
		p, err := ElicitationPayloadFromStream(item)
		if err != nil {
			t.Fatalf("ElicitationPayloadFromStream: %v", err)
		}
		if p.StreamKind() != StreamKindElicitationDecided {
			t.Fatalf("StreamKind = %q, want %q", p.StreamKind(), StreamKindElicitationDecided)
		}
	})

	t.Run("returns error for nil payload", func(t *testing.T) {
		item := StreamItem{Kind: StreamKindElicitationPending}
		_, err := ElicitationPayloadFromStream(item)
		if err == nil {
			t.Fatal("expected error for nil payload")
		}
	})

	t.Run("returns error for empty action_id", func(t *testing.T) {
		item := StreamItem{
			Kind:    StreamKindElicitationPending,
			Payload: &ElicitationPayload{Message: "hello"},
		}
		_, err := ElicitationPayloadFromStream(item)
		if err == nil {
			t.Fatal("expected error when action_id is empty")
		}
	})
}

func TestSamplingPayloadFromStream(t *testing.T) {
	t.Run("extracts typed SamplingPayload", func(t *testing.T) {
		item := StreamItem{
			Kind: StreamKindSamplingStarted,
			Payload: SamplingPayload{
				RunID: "run_sampling_1",
				Depth: 1,
				Model: "gpt-4",
			},
		}
		p, err := SamplingPayloadFromStream(item)
		if err != nil {
			t.Fatalf("SamplingPayloadFromStream: %v", err)
		}
		if p.RunID != "run_sampling_1" {
			t.Fatalf("RunID = %q, want run_sampling_1", p.RunID)
		}
		if p.Depth != 1 {
			t.Fatalf("Depth = %d, want 1", p.Depth)
		}
		if p.Model != "gpt-4" {
			t.Fatalf("Model = %q, want gpt-4", p.Model)
		}
		if p.StreamKind() != StreamKindSamplingStarted {
			t.Fatalf("StreamKind = %q, want %q", p.StreamKind(), StreamKindSamplingStarted)
		}
	})

	t.Run("extracts from pointer payload", func(t *testing.T) {
		item := StreamItem{
			Kind: StreamKindSamplingStarted,
			Payload: &SamplingPayload{
				RunID: "run_789",
				Depth: 2,
			},
		}
		p, err := SamplingPayloadFromStream(item)
		if err != nil {
			t.Fatalf("SamplingPayloadFromStream: %v", err)
		}
		if p.RunID != "run_789" {
			t.Fatalf("RunID = %q, want run_789", p.RunID)
		}
		if p.Depth != 2 {
			t.Fatalf("Depth = %d, want 2", p.Depth)
		}
		if p.StreamKind() != StreamKindSamplingStarted {
			t.Fatalf("StreamKind = %q, want %q", p.StreamKind(), StreamKindSamplingStarted)
		}
	})

	t.Run("preserves completed stream kind", func(t *testing.T) {
		item := StreamItem{
			Kind: StreamKindSamplingCompleted,
			Payload: &SamplingPayload{
				RunID: "run_sampling_3",
				Depth: 2,
				Model: "gpt-4",
			},
		}
		p, err := SamplingPayloadFromStream(item)
		if err != nil {
			t.Fatalf("SamplingPayloadFromStream: %v", err)
		}
		if p.StreamKind() != StreamKindSamplingCompleted {
			t.Fatalf("StreamKind = %q, want %q", p.StreamKind(), StreamKindSamplingCompleted)
		}
	})

	t.Run("returns error for nil payload", func(t *testing.T) {
		item := StreamItem{Kind: StreamKindSamplingStarted}
		_, err := SamplingPayloadFromStream(item)
		if err == nil {
			t.Fatal("expected error for nil payload")
		}
	})

	t.Run("returns error for empty run_id", func(t *testing.T) {
		item := StreamItem{
			Kind:    StreamKindSamplingStarted,
			Payload: &SamplingPayload{Depth: 1},
		}
		_, err := SamplingPayloadFromStream(item)
		if err == nil {
			t.Fatal("expected error when run_id is empty")
		}
	})
}
