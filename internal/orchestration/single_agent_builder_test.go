package orchestration

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/tooling"
)

func TestBuildSingleAgentRejectsMissingAssistantStreamer(t *testing.T) {
	ctx := context.Background()
	catalog, err := tooling.NewCatalog(ctx, nil)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	plane := NewDefaultPlane(DefaultPlaneOptions{})
	assembly, err := plane.BuildSingleAgent(ctx, SingleAgentRequest{
		Catalog: catalog,
	})
	if err == nil {
		t.Fatal("BuildSingleAgent succeeded, want missing assistant streamer error")
	}
	if err.Error() != "assistant streamer is required" {
		t.Fatalf("BuildSingleAgent error = %q, want %q", err.Error(), "assistant streamer is required")
	}
	if assembly != nil {
		t.Fatalf("BuildSingleAgent assembly = %#v, want nil", assembly)
	}
}
