package store

import (
	"testing"
	"time"
)

func TestBuildToolResultRef(t *testing.T) {
	got := BuildToolResultRef(" run-1 ", " call-1 ")
	want := "tool_result:run-1:call-1"
	if got != want {
		t.Fatalf("BuildToolResultRef with spaces = %q, want %q", got, want)
	}
}

func TestPreviewToolResult(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		limit int
		want  string
	}{
		{name: "short text", text: "hello", limit: 10, want: "hello"},
		{name: "exact limit", text: "hello world", limit: 11, want: "hello world"},
		{name: "long text", text: "hello world this is a long text", limit: 10, want: "hello worl..."},
		{name: "zero limit", text: "hello", limit: 0, want: "hello"},
		{name: "negative limit", text: "hello", limit: -1, want: "hello"},
		{name: "trims spaces", text: "  hello  ", limit: 10, want: "hello"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PreviewToolResult(tc.text, tc.limit)
			if got != tc.want {
				t.Fatalf("PreviewToolResult(%q, %d) = %q, want %q", tc.text, tc.limit, got, tc.want)
			}
		})
	}
}

func TestNormalizeToolResultAppendRequest(t *testing.T) {
	now := time.Now().UTC()
	got, err := NormalizeToolResultAppendRequest(ToolResultAppendRequest{
		RunID:         " run-1 ",
		SessionID:     " session-1 ",
		CallID:        " call-1 ",
		ToolName:      " test-tool ",
		ArgumentsJSON: ` {"ok":true} `,
		Status:        ToolResultStatusSucceeded,
		ErrorReason:   " ",
		TokenEstimate: 100,
		CreatedAt:     now,
	})
	if err != nil {
		t.Fatalf("NormalizeToolResultAppendRequest: %v", err)
	}
	if got.RunID != "run-1" || got.SessionID != "session-1" || got.CallID != "call-1" || got.ToolName != "test-tool" {
		t.Fatalf("fields not normalized: %+v", got)
	}
	if got.ArgumentsJSON != `{"ok":true}` {
		t.Fatalf("ArgumentsJSON = %q, want compact trimmed JSON text", got.ArgumentsJSON)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
}

func TestNormalizeToolResultAppendRequestZeroTime(t *testing.T) {
	got, err := NormalizeToolResultAppendRequest(ToolResultAppendRequest{
		RunID:    "run-1",
		CallID:   "call-1",
		ToolName: "test-tool",
		Status:   ToolResultStatusSucceeded,
	})
	if err != nil {
		t.Fatalf("NormalizeToolResultAppendRequest: %v", err)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set to current time")
	}
}

func TestNormalizeToolResultAppendRequestValidation(t *testing.T) {
	tests := []struct {
		name string
		req  ToolResultAppendRequest
	}{
		{name: "empty run_id", req: ToolResultAppendRequest{CallID: "c", ToolName: "t", Status: ToolResultStatusSucceeded}},
		{name: "empty call_id", req: ToolResultAppendRequest{RunID: "r", ToolName: "t", Status: ToolResultStatusSucceeded}},
		{name: "empty tool_name", req: ToolResultAppendRequest{RunID: "r", CallID: "c", Status: ToolResultStatusSucceeded}},
		{name: "invalid status", req: ToolResultAppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: "invalid"}},
		{name: "negative token", req: ToolResultAppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: ToolResultStatusSucceeded, TokenEstimate: -1}},
		{name: "side effect missing kind", req: ToolResultAppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: ToolResultStatusSucceeded, SideEffects: []SideEffectRef{{Path: "README.md"}}}},
		{name: "artifact side effect missing ref", req: ToolResultAppendRequest{RunID: "r", CallID: "c", ToolName: "t", Status: ToolResultStatusSucceeded, SideEffects: []SideEffectRef{{Kind: SideEffectKindArtifact}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeToolResultAppendRequest(tc.req)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestNormalizeToolResultAppendRequestAcceptsNativeDeveloperToolSideEffects(t *testing.T) {
	got, err := NormalizeToolResultAppendRequest(ToolResultAppendRequest{
		RunID:    "run_1",
		CallID:   "call_1",
		ToolName: "artifact_write",
		Status:   ToolResultStatusSucceeded,
		SideEffects: []SideEffectRef{
			{Kind: SideEffectKindArtifact, Ref: "artifact_1"},
			{Kind: SideEffectKindOperatorAction, Ref: "action_1"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeToolResultAppendRequest: %v", err)
	}
	if len(got.SideEffects) != 2 {
		t.Fatalf("side effect count = %d, want 2", len(got.SideEffects))
	}
}

func TestNormalizeEvidenceRef(t *testing.T) {
	got, err := NormalizeEvidenceRef(EvidenceRef{Kind: " fact ", Ref: " ref-1 "})
	if err != nil {
		t.Fatalf("NormalizeEvidenceRef: %v", err)
	}
	if got.Kind != "fact" || got.Ref != "ref-1" {
		t.Fatalf("fields not normalized: %+v", got)
	}
}

func TestNormalizeEvidenceRefValidation(t *testing.T) {
	tests := []struct {
		name string
		ref  EvidenceRef
	}{
		{name: "empty kind", ref: EvidenceRef{Kind: "", Ref: "r"}},
		{name: "empty ref", ref: EvidenceRef{Kind: "k", Ref: ""}},
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
