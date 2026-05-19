package contextplane

import (
	"testing"

	"github.com/ycvk/acorn/internal/decision"
)

func TestBudgetAllocatorDefaultAllocatesAllSections(t *testing.T) {
	status, err := NewBudgetAllocator().Allocate(BudgetRequest{TotalTokens: 100})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if status.TotalTokens != 100 {
		t.Fatalf("TotalTokens = %d, want 100", status.TotalTokens)
	}
	if len(status.Allocations) != 4 {
		t.Fatalf("allocations = %d, want 4", len(status.Allocations))
	}
	for section, want := range map[Section]int{
		SectionSkill:        20,
		SectionMemory:       30,
		SectionToolDef:      20,
		SectionConversation: 30,
	} {
		if got := budgetFor(status, section); got != want {
			t.Fatalf("%s budget = %d, want %d", section, got, want)
		}
	}
	if sumBudget(status) != 100 {
		t.Fatalf("allocation sum = %d, want 100", sumBudget(status))
	}
}

func budgetFor(status BudgetStatus, section Section) int {
	for _, allocation := range status.Allocations {
		if allocation.Section == section {
			return allocation.Tokens
		}
	}
	return 0
}

func sumBudget(status BudgetStatus) int {
	total := 0
	for _, allocation := range status.Allocations {
		total += allocation.Tokens
	}
	return total
}

func TestBudgetAllocator_SkillHintIncreasesSkillBudget(t *testing.T) {
	allocator := NewBudgetAllocator()
	baseline, _ := allocator.Allocate(BudgetRequest{TotalTokens: 100})
	hinted, err := allocator.Allocate(BudgetRequest{
		TotalTokens: 100,
		Hint:        &DecisionContextHint{ContextPriority: decision.PrioritySkill},
	})
	if err != nil {
		t.Fatalf("hinted Allocate: %v", err)
	}
	if budgetFor(hinted, SectionSkill) <= budgetFor(baseline, SectionSkill) {
		t.Fatalf("skill budget = %d, want > baseline %d", budgetFor(hinted, SectionSkill), budgetFor(baseline, SectionSkill))
	}
	if sumBudget(hinted) != 100 {
		t.Fatalf("hinted allocation sum = %d, want 100", sumBudget(hinted))
	}
}

func TestBudgetAllocator_ConversationHintIncreasesConversationBudget(t *testing.T) {
	allocator := NewBudgetAllocator()
	baseline, _ := allocator.Allocate(BudgetRequest{TotalTokens: 100})
	hinted, err := allocator.Allocate(BudgetRequest{
		TotalTokens: 100,
		Hint:        &DecisionContextHint{ContextPriority: decision.PriorityConversation},
	})
	if err != nil {
		t.Fatalf("hinted Allocate: %v", err)
	}
	if budgetFor(hinted, SectionConversation) <= budgetFor(baseline, SectionConversation) {
		t.Fatalf("conversation budget = %d, want > baseline %d", budgetFor(hinted, SectionConversation), budgetFor(baseline, SectionConversation))
	}
}

func TestBuildHint_IntegrationWithBudget(t *testing.T) {
	allocator := NewBudgetAllocator()
	hint := decision.BuildHint(decision.ActionInspectFirst)
	hinted, err := allocator.Allocate(BudgetRequest{
		TotalTokens: 100,
		Hint:        hint,
	})
	if err != nil {
		t.Fatalf("Allocate with BuildHint: %v", err)
	}
	if budgetFor(hinted, SectionConversation) != 30 {
		t.Fatalf("conversation budget = %d, want balanced baseline 30", budgetFor(hinted, SectionConversation))
	}
}
