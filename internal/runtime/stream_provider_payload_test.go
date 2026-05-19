package runtime

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestActiveProviderInMeta(t *testing.T) {
	msg := schema.AssistantMessage("hello", nil)
	stream := streamMessageFromSchema(msg, "primary")
	if stream == nil {
		t.Fatal("expected stream message")
	}
	if stream.Meta == nil {
		t.Fatal("expected meta map")
	}
	if stream.Meta["active_provider"] != "primary" {
		t.Fatalf("active_provider = %v, want primary", stream.Meta["active_provider"])
	}
}
