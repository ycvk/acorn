package runprojection

import (
	"strings"
	"testing"
)

func TestProjectStreamItemToEventFailsOnInvalidStructuredPayload(t *testing.T) {
	for _, kind := range []StreamItemKind{
		StreamKindElicitationPending,
		StreamKindSamplingStarted,
		StreamKindProviderDegraded,
		StreamKindMCPToolCatalogRefreshFailed,
	} {
		t.Run(string(kind), func(t *testing.T) {
			_, _, err := ProjectStreamItemToEvent(StreamItem{
				RunID:   "run_projection_failure",
				Kind:    kind,
				Payload: &ElicitationPayload{RequestedSchema: map[string]any{"not_json": make(chan int)}},
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
