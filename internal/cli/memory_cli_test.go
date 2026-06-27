package cli

import (
	"strings"
	"testing"
)

func TestUsageIncludesMemoryReindex(t *testing.T) {
	body := usageText()
	if !strings.Contains(body, "acorn memory reindex") {
		t.Fatalf("usageText should advertise `acorn memory reindex`, got:\n%s", body)
	}
}

func TestMemoryRequiresSubcommand(t *testing.T) {
	err := runMemory(t.Context(), nil)
	if err == nil || !strings.Contains(err.Error(), "requires a subcommand") {
		t.Fatalf("expected subcommand error, got %v", err)
	}
}

func TestMemoryUnknownSubcommand(t *testing.T) {
	err := runMemory(t.Context(), []string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown memory subcommand") {
		t.Fatalf("expected unknown subcommand error, got %v", err)
	}
}
