package runtime

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMessageToMapPreservesToolContent(t *testing.T) {
	msg := &schema.Message{
		Role:    schema.Tool,
		Content: strings.Repeat("a", 1200),
	}
	stream := streamMessageFromSchema(msg, "")
	content := stream.Content
	if len(content) != 1200 {
		t.Fatalf("expected full tool content, got len=%d", len(content))
	}
	if len(stream.Meta) > 0 {
		t.Fatalf("expected no meta, got %#v", stream.Meta)
	}
}

func TestCompactInterruptInfoKeepsUsefulKeysOnly(t *testing.T) {
	payload := compactInterruptInfo(map[string]any{
		"kind":           "elicitation_request",
		"message":        "resume to continue",
		"tool_name":      "run_command",
		"arguments_json": `{"command":["pwd"]}`,
		"reason":         "elicitation_required",
		"command":        []string{"pwd"},
		"cwd":            "/tmp",
		"huge_state":     strings.Repeat("x", 5000),
	})
	data, ok := payload.(map[string]any)
	if !ok {
		t.Fatalf("expected compact interrupt info map, got %#v", payload)
	}
	if _, ok := data["huge_state"]; ok {
		t.Fatalf("unexpected huge_state in compact interrupt info: %#v", data)
	}
	if got := data["kind"]; got != "elicitation_request" {
		t.Fatalf("unexpected kind: %#v", got)
	}
}

func TestCompactText(t *testing.T) {
	short, truncated := compactText("  hello  ", 10)
	if short != "hello" || truncated {
		t.Fatalf("unexpected compactText short result: %q %v", short, truncated)
	}
	long, truncated := compactText(strings.Repeat("b", 20), 5)
	if !truncated || long != "bbbbb..." {
		t.Fatalf("unexpected compactText long result: %q %v", long, truncated)
	}
}
