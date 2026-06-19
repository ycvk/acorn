package runtime

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/stream"
)

func TestMessageToMapPreservesToolContent(t *testing.T) {
	msg := &schema.Message{
		Role:    schema.Tool,
		Content: strings.Repeat("a", 1200),
	}
	message := stream.StreamMessageFromSchema(msg, "")
	content := message.Content
	if len(content) != 1200 {
		t.Fatalf("expected full tool content, got len=%d", len(content))
	}
	if len(message.Meta) > 0 {
		t.Fatalf("expected no meta, got %#v", message.Meta)
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
