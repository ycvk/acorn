package api

import (
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/core"
)

func TestBuildChatMessagesReadsWakeAndCaptureAsUser(t *testing.T) {
	messages := buildChatMessages([]core.SessionMessageRecord{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: core.MessageRoleWake, Content: "[commitment #1] check X"},
		{Role: core.MessageRoleCapture, Content: "[capture] https://example.com"},
		{Role: "tool", Content: "ignored"},
	})
	want := []schema.RoleType{schema.User, schema.Assistant, schema.User, schema.User}
	if len(messages) != len(want) {
		t.Fatalf("messages = %d, want %d", len(messages), len(want))
	}
	for i, role := range want {
		if messages[i].Role != role {
			t.Fatalf("messages[%d].Role = %s, want %s", i, messages[i].Role, role)
		}
	}
	if _, err := projectMessage(core.SessionMessageRecord{ID: 4, Role: core.MessageRoleCapture, Content: "x"}); err != nil {
		t.Fatalf("project capture message: %v", err)
	}
}
