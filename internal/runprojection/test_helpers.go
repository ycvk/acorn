package runprojection

import "testing"

func MustProjectStreamItemToEvent(t *testing.T, item StreamItem) (string, any) {
	t.Helper()
	kind, payload, err := ProjectStreamItemToEvent(item)
	if err != nil {
		t.Fatalf("ProjectStreamItemToEvent(%q): %v", item.Kind, err)
	}
	return kind, payload
}
