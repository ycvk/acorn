package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/memory"
)

// TestActiveFactsInjectedWithoutSkillsOrEntries verifies the fix for a bug
// where ActiveFacts were silently dropped when there were no Nudges,
// Entries, or SkillTree — the exact scenario where Active Memory matters
// most (new deployment, facts but no skills yet).
func TestActiveFactsInjectedWithoutSkillsOrEntries(t *testing.T) {
	prepared := &memory.PrepareResult{
		ActiveFacts: []memory.Entry{
			{Ref: "facts/user/owner.md#owner", Content: "Owner: Alice"},
		},
	}
	// No Nudges, no Entries, no SkillTree — only ActiveFacts.
	if !hasPreparedMemory(prepared) {
		t.Fatal("hasPreparedMemory must return true when ActiveFacts is non-empty")
	}
	section, refs, err := fitPreparedMemoryToBudget(context.Background(), &stubCounter{}, prepared, 1000)
	if err != nil {
		t.Fatalf("fitPreparedMemoryToBudget: %v", err)
	}
	if section == "" {
		t.Fatal("ActiveFacts must be injected even without skills/entries/nudges")
	}
	if !strings.Contains(section, "Active Memory") {
		t.Errorf("section missing Active Memory header: %q", section)
	}
	if !strings.Contains(section, "Owner: Alice") {
		t.Errorf("section missing fact content: %q", section)
	}
	if len(refs) != 0 {
		t.Errorf("expected no attached refs for active facts, got %v", refs)
	}
}

type stubCounter struct{}

func (stubCounter) CountText(_ context.Context, text string) (int, error) {
	return len(text) / 4, nil // rough token estimate
}

func (stubCounter) CountMessages(_ context.Context, _ []adk.Message, _ []*schema.ToolInfo) (int, error) {
	return 0, nil
}
