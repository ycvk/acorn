package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/stream"
)

func mustProjectStreamItemToEvent(t *testing.T, item stream.StreamItem) (string, any) {
	t.Helper()
	kind, payload, err := stream.ProjectStreamItemToEvent(item)
	if err != nil {
		t.Fatalf("stream.ProjectStreamItemToEvent(%q): %v", item.Kind, err)
	}
	return kind, payload
}
