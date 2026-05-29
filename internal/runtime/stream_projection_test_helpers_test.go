package runtime

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ycvk/acorn/internal/events"
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

func mustProjectEventToStreamItem(t *testing.T, event events.EventRecord) stream.StreamItem {
	t.Helper()
	item, err := stream.ProjectEventToStreamItem(event)
	if err != nil {
		t.Fatalf("stream.ProjectEventToStreamItem(%q): %v", event.Kind, err)
	}
	return item
}

// assertStreamItemsEqualJSON compares two StreamItems by serializing both to JSON,
// then unmarshaling back to maps and comparing the semantic tree. This normalizes
// away Go-level type differences (struct vs map ordering, float64 vs int, etc.).
func assertStreamItemsEqualJSON(t *testing.T, expected, actual stream.StreamItem) {
	t.Helper()

	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("marshal expected stream.StreamItem: %v", err)
	}
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatalf("marshal actual stream.StreamItem: %v", err)
	}

	var expectedMap, actualMap map[string]any
	if err := json.Unmarshal(expectedJSON, &expectedMap); err != nil {
		t.Fatalf("unmarshal expected JSON: %v", err)
	}
	if err := json.Unmarshal(actualJSON, &actualMap); err != nil {
		t.Fatalf("unmarshal actual JSON: %v", err)
	}

	if !jsonTreesEqual(expectedMap, actualMap) {
		expectedFmt, _ := json.MarshalIndent(expectedMap, "", "  ")
		actualFmt, _ := json.MarshalIndent(actualMap, "", "  ")
		t.Fatalf("roundtrip mismatch:\nexpected:\n%s\n\nactual:\n%s", expectedFmt, actualFmt)
	}
}

// jsonTreesEqual recursively compares two JSON-decoded values, treating
// float64 and int as equal when they represent the same numeric value.
func jsonTreesEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, av := range av {
			bv, ok := bv[k]
			if !ok || !jsonTreesEqual(av, bv) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonTreesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case float64:
		switch bv := b.(type) {
		case float64:
			return av == bv
		case int:
			return av == float64(bv)
		case int64:
			return av == float64(bv)
		default:
			return false
		}
	case int:
		switch bv := b.(type) {
		case float64:
			return float64(av) == bv
		case int:
			return av == bv
		case int64:
			return int64(av) == bv
		default:
			return false
		}
	case int64:
		switch bv := b.(type) {
		case float64:
			return float64(av) == bv
		case int:
			return av == int64(bv)
		case int64:
			return av == bv
		default:
			return false
		}
	default:
		return reflect.DeepEqual(a, b)
	}
}
