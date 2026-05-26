package runtime

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/stream"
)

func TestProjectStreamItemToEventFailsOnInvalidStructuredPayload(t *testing.T) {
	for _, kind := range []stream.StreamItemKind{
		stream.StreamKindElicitationPending,
		stream.StreamKindSamplingStarted,
		stream.StreamKindProviderDegraded,
		stream.StreamKindMCPToolCatalogRefreshFailed,
	} {
		t.Run(string(kind), func(t *testing.T) {
			_, _, err := stream.ProjectStreamItemToEvent(stream.StreamItem{
				RunID:   "run_projection_failure",
				Kind:    kind,
				Payload: &stream.ElicitationPayload{RequestedSchema: map[string]any{"not_json": make(chan int)}},
			})
			if err == nil {
				t.Fatal("stream.ProjectStreamItemToEvent returned nil error, want payload projection error")
			}
			if !strings.Contains(err.Error(), "stream "+string(kind)+" payload") {
				t.Fatalf("error = %q, want kind-specific projection context", err.Error())
			}
		})
	}
}
