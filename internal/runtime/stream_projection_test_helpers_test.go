package runtime

import "testing"

func mustProjectStreamItemToEvent(t *testing.T, item StreamItem) (string, any) {
	t.Helper()
	kind, payload, err := projectStreamItemToEvent(item)
	if err != nil {
		t.Fatalf("projectStreamItemToEvent(%q): %v", item.Kind, err)
	}
	return kind, payload
}
