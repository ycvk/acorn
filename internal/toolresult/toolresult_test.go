package toolresult

import (
	"testing"
	"time"
)

func TestBuildRef(t *testing.T) {
	got := BuildRef("run-1", "call-1")
	want := "tool_result:run-1:call-1"
	if got != want {
		t.Fatalf("BuildRef(\"run-1\", \"call-1\") = %q, want %q", got, want)
	}
}

func TestBuildRefTrimsSpaces(t *testing.T) {
	got := BuildRef(" run-1 ", " call-1 ")
	want := "tool_result:run-1:call-1"
	if got != want {
		t.Fatalf("BuildRef with spaces = %q, want %q", got, want)
	}
}

func TestPreview(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{"short text", "hello", 10, "hello"},
		{"exact limit", "hello world", 11, "hello world"},
		{"long text", "hello world this is a long text", 10, "hello worl..."},
		{"zero limit", "hello", 0, "hello"},
		{"negative limit", "hello", -1, "hello"},
		{"trims spaces", "  hello  ", 10, "hello"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Preview(tc.text, tc.limit)
			if got != tc.want {
				t.Fatalf("Preview(%q, %d) = %q, want %q", tc.text, tc.limit, got, tc.want)
			}
		})
	}
}

func TestNormalizeAppendRequest(t *testing.T) {
	now := time.Now().UTC()
	req := AppendRequest{
		RunID:         "run-1",
		CallID:        "call-1",
		ToolName:      "test-tool",
		Status:        StatusSucceeded,
		TokenEstimate: 100,
		CreatedAt:     now,
	}

	got, err := NormalizeAppendRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RunID != "run-1" || got.CallID != "call-1" || got.ToolName != "test-tool" {
		t.Fatalf("fields not preserved: %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt not UTC-normalized")
	}
}

func TestNormalizeAppendRequestZeroTime(t *testing.T) {
	req := AppendRequest{
		RunID:    "run-1",
		CallID:   "call-1",
		ToolName: "test-tool",
		Status:   StatusSucceeded,
	}

	got, err := NormalizeAppendRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt should be set to current time")
	}
}

func TestNormalizeAppendRequestValidation(t *testing.T) {
	tests := []struct {
		name string
		req  AppendRequest
	}{
		{"empty run_id", AppendRequest{RunID: "", CallID: "c", ToolName: "t", Status: StatusSucceeded}},
		{"empty call_id", AppendRequest{RunID: "r", CallID: "", ToolName: "t", Status: StatusSucceeded}},
		{"empty tool_name", AppendRequest{RunID: "r", CallID: "c", ToolName: "", Status: StatusSucceeded}},
		{"invalid status", AppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: "invalid"}},
		{"negative token", AppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: StatusSucceeded, TokenEstimate: -1}},
		{"side effect missing kind", AppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: StatusSucceeded, SideEffects: []SideEffectRef{{Path: "README.md"}}}},
		{"artifact side effect missing ref", AppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: StatusSucceeded, SideEffects: []SideEffectRef{{Kind: SideEffectKindArtifact}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeAppendRequest(tc.req)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestNormalizeAppendRequestAcceptsNativeDeveloperToolSideEffects(t *testing.T) {
	got, err := NormalizeAppendRequest(AppendRequest{
		RunID:    "run_1",
		CallID:   "call_1",
		ToolName: "artifact_write",
		Status:   StatusSucceeded,
		SideEffects: []SideEffectRef{
			{Kind: SideEffectKindArtifact, Ref: "artifact_1"},
			{Kind: SideEffectKindOperatorAction, Ref: "action_1"},
		},
	})
	if err != nil {
		t.Fatalf("normalize append request: %v", err)
	}
	if len(got.SideEffects) != 2 {
		t.Fatalf("side effect count = %d, want 2", len(got.SideEffects))
	}
}

func TestNormalizeEvidenceRef(t *testing.T) {
	ref := EvidenceRef{Kind: "fact", Ref: "ref-1"}
	got, err := NormalizeEvidenceRef(ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Kind != "fact" || got.Ref != "ref-1" {
		t.Fatalf("fields not preserved: %+v", got)
	}
}

func TestNormalizeEvidenceRefValidation(t *testing.T) {
	tests := []struct {
		name string
		ref  EvidenceRef
	}{
		{"empty kind", EvidenceRef{Kind: "", Ref: "r"}},
		{"empty ref", EvidenceRef{Kind: "k", Ref: ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeEvidenceRef(tc.ref)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}
