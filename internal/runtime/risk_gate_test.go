package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestApprovalRequiredToolMessageContainsToolName(t *testing.T) {
	msg := approvalRequiredToolMessage("call_42", "run_command")
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.ToolCallID != "call_42" {
		t.Fatalf("ToolCallID = %q, want 'call_42'", msg.ToolCallID)
	}
	if msg.Role != schema.Tool {
		t.Fatalf("Role = %q, want Tool", msg.Role)
	}
	// Content must mention the tool name so the model knows what to do.
	if !contains(msg.Content, "run_command") {
		t.Fatalf("content = %q, want contains 'run_command'", msg.Content)
	}
	if !contains(msg.Content, "ask_operator") {
		t.Fatalf("content = %q, want contains 'ask_operator' instruction", msg.Content)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > 0 && containsStr(s, substr)))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDirectResponseBeforeToolCallAppliesRiskGate(t *testing.T) {
	state := &ToolLifecycleState{
		RunID:     "run_1",
		SessionID: "session_1",
		LoadedTools: map[string]LoadedToolRecord{
			"run_command": {},
			"read_file":   {},
		},
	}
	ctx := WithToolLifecycleContext(context.Background(), state, nil, nil)

	err := directResponseBeforeToolCall(ctx, schema.ToolCall{ID: "call_1", Function: schema.FunctionCall{Name: "run_command"}})
	var are *ApprovalRequiredError
	if !errors.As(err, &are) || are.ToolName != "run_command" || are.CallID != "call_1" {
		t.Fatalf("run_command error = %v, want ApprovalRequiredError for call_1", err)
	}
	if err := directResponseBeforeToolCall(ctx, schema.ToolCall{ID: "call_2", Function: schema.FunctionCall{Name: "read_file"}}); err != nil {
		t.Fatalf("read_file error = %v, want nil", err)
	}
}
