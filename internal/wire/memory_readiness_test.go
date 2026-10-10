package wire

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func TestMemoryReadinessKeepsOperatorContainerAvailable(t *testing.T) {
	for _, name := range []string{"missing embedding key", "unconfigured provider"} {
		t.Run(name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Runtime.StorageDir = t.TempDir()
			cfg.Skills.Dir = t.TempDir()
			cfg.Providers[0].APIKey = "model-test"
			cfg.Memory.Embedding.APIKey = ""
			expected := "memory.embedding.api_key"
			if name == "unconfigured provider" {
				cfg.Providers = nil
				expected = "provider"
			}
			ctx := context.Background()
			container, err := NewContainer(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer container.Close()
			if err := cfg.ValidateExecutionReady(); err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("readiness %v", err)
			}
			if _, err := container.MemoryStatus(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := container.KnowledgeStatus(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
