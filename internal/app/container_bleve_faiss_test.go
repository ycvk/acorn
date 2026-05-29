//go:build bleve_faiss && vectors && cgo

package app

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func testContainer(t *testing.T, cfg *config.Config) *Container {
	t.Helper()
	container, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("new container: %v", err)
	}
	t.Cleanup(func() { _ = container.Close() })
	return container
}

func TestContainerBuildsCoreServices(t *testing.T) {
	container := testContainer(t, testContainerConfig(t))

	for name, service := range map[string]any{
		"chat":         container.Chat(),
		"run":          container.Run(),
		"resume":       container.Resume(),
		"capabilities": container.Capabilities(),
		"skills":       container.Skills(),
	} {
		if service == nil {
			t.Fatalf("expected %s service to be wired", name)
		}
	}
}

func TestContainerMCPServerNilWithoutAllowlist(t *testing.T) {
	cfg := testContainerConfig(t)
	container := testContainer(t, cfg)

	if container.MCPServer() != nil {
		t.Fatal("expected MCPServer() to return nil when allowlist is empty")
	}
}

func TestContainerMCPServerNonNilWithAllowlist(t *testing.T) {
	cfg := testContainerConfig(t)
	cfg.Serve = config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"read_file"},
		},
	}
	container := testContainer(t, cfg)

	if container.MCPServer() == nil {
		t.Fatal("expected MCPServer() to return non-nil when allowlist is configured")
	}
}
