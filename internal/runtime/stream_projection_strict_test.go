package runtime

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

func TestProjectStreamItemToEventFailsOnInvalidStructuredPayload(t *testing.T) {
	for _, kind := range []domain.StreamItemKind{
		domain.StreamKindElicitationPending,
	} {
		t.Run(string(kind), func(t *testing.T) {
			_, _, err := ProjectStreamItemToEvent(domain.StreamItem{
				RunID:   "run_projection_failure",
				Kind:    kind,
				Payload: map[string]any{"requested_schema": map[string]any{"not_json": make(chan int)}},
			})
			if err == nil {
				t.Fatal("ProjectStreamItemToEvent returned nil error, want payload projection error")
			}
			if !strings.Contains(err.Error(), "stream "+string(kind)+" payload") {
				t.Fatalf("error = %q, want kind-specific projection context", err.Error())
			}
		})
	}
}
