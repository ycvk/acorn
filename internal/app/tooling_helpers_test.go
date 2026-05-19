package app

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

func TestStaticSkillEligibilityContextIncludesBuiltInTools(t *testing.T) {
	ctx := staticSkillEligibilityContext(&config.Config{})
	available := make(map[string]struct{}, len(ctx.AvailableTools))
	for _, name := range ctx.AvailableTools {
		available[name] = struct{}{}
	}
	for _, want := range []string{
		"read_file",
		"memory_read_file",
		"memory_search_text",
		"memory_create_file",
		"memory_replace_span",
		"run_command",
	} {
		if _, ok := available[want]; !ok {
			t.Fatalf("expected %q in static skill eligibility context, got %v", want, ctx.AvailableTools)
		}
	}
}
