package stream

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
	message := StreamMessageFromSchema(msg, "")
	content := message.Content
	if len(content) != 1200 {
		t.Fatalf("expected full tool content, got len=%d", len(content))
	}
	if len(message.Meta) > 0 {
		t.Fatalf("expected no meta, got %#v", message.Meta)
	}
}
