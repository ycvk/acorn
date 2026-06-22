package runtime

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/runtime/eventstream"
)

func TestProjectStreamItemToEventFailsOnInvalidStructuredPayload(t *testing.T) {
	for _, kind := range []eventstream.StreamItemKind{
		eventstream.StreamKindElicitationPending,
	} {
		t.Run(string(kind), func(t *testing.T) {
			_, _, err := eventstream.ProjectStreamItemToEvent(eventstream.StreamItem{
				RunID:   "run_projection_failure",
				Kind:    kind,
				Payload: map[string]any{"requested_schema": map[string]any{"not_json": make(chan int)}},
			})
			if err == nil {
				t.Fatal("eventstream.ProjectStreamItemToEvent returned nil error, want payload projection error")
			}
			if !strings.Contains(err.Error(), "stream "+string(kind)+" payload") {
				t.Fatalf("error = %q, want kind-specific projection context", err.Error())
			}
		})
	}
}
