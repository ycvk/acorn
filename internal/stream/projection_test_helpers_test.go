package stream

import (
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

func mustProjectStreamItemToEvent(t *testing.T, item domain.StreamItem) (string, any) {
	t.Helper()
	kind, payload, err := ProjectStreamItemToEvent(item)
	if err != nil {
		t.Fatalf("ProjectStreamItemToEvent(%q): %v", item.Kind, err)
	}
	return kind, payload
}
