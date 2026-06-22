package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/runtime/eventstream"
)

func mustProjectStreamItemToEvent(t *testing.T, item eventstream.StreamItem) (string, any) {
	t.Helper()
	kind, payload, err := eventstream.ProjectStreamItemToEvent(item)
	if err != nil {
		t.Fatalf("eventstream.ProjectStreamItemToEvent(%q): %v", item.Kind, err)
	}
	return kind, payload
}
