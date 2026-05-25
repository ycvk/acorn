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

func TestBudgetAllocator_SkillPriorityIncreasesSkillBudget(t *testing.T) {
	allocator := NewBudgetAllocator()
	baseline, _ := allocator.Allocate(BudgetRequest{TotalTokens: 100})
	prioritized, err := allocator.Allocate(BudgetRequest{
		TotalTokens:     100,
		ContextPriority: decision.PrioritySkill,
	})
	if err != nil {
		t.Fatalf("prioritized Allocate: %v", err)
	}
	if budgetFor(prioritized, SectionSkill) <= budgetFor(baseline, SectionSkill) {
		t.Fatalf("skill budget = %d, want > baseline %d", budgetFor(prioritized, SectionSkill), budgetFor(baseline, SectionSkill))
	}
	if sumBudget(prioritized) != 100 {
		t.Fatalf("prioritized allocation sum = %d, want 100", sumBudget(prioritized))
	}
}

func TestBudgetAllocator_ConversationPriorityIncreasesConversationBudget(t *testing.T) {
	allocator := NewBudgetAllocator()
	baseline, _ := allocator.Allocate(BudgetRequest{TotalTokens: 100})
	prioritized, err := allocator.Allocate(BudgetRequest{
		TotalTokens:     100,
		ContextPriority: decision.PriorityConversation,
	})
	if err != nil {
		t.Fatalf("prioritized Allocate: %v", err)
	}
	if budgetFor(prioritized, SectionConversation) <= budgetFor(baseline, SectionConversation) {
		t.Fatalf("conversation budget = %d, want > baseline %d", budgetFor(prioritized, SectionConversation), budgetFor(baseline, SectionConversation))
	}
}

func TestContextPriorityForAction_IntegrationWithBudget(t *testing.T) {
	allocator := NewBudgetAllocator()
	priority := decision.ContextPriorityForAction(decision.ActionInspectFirst)
	prioritized, err := allocator.Allocate(BudgetRequest{
		TotalTokens:     100,
		ContextPriority: priority,
	})
	if err != nil {
		t.Fatalf("Allocate with ContextPriorityForAction: %v", err)
	}
	if budgetFor(prioritized, SectionConversation) != 30 {
		t.Fatalf("conversation budget = %d, want balanced baseline 30", budgetFor(prioritized, SectionConversation))
	}
}
