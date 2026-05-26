package runtime

import (
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/stream"
)

func TestActiveProviderInMeta(t *testing.T) {
	msg := schema.AssistantMessage("hello", nil)
	message := stream.StreamMessageFromSchema(msg, "primary")
	if message == nil {
		t.Fatal("expected stream message")
	}
	if message.Meta == nil {
		t.Fatal("expected meta map")
	}
	if message.Meta["active_provider"] != "primary" {
		t.Fatalf("active_provider = %v, want primary", message.Meta["active_provider"])
	}
}
