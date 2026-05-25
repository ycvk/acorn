package contextplane

import (
	"context"
	"fmt"
)

type Section string

const (
	SectionSkill        Section = "skill"
	SectionMemory       Section = "memory"
	SectionToolDef      Section = "tool_def"
	SectionConversation Section = "conversation"
)

type BudgetRequest struct {
	TotalTokens     int
	PresentSections []Section
	ContextPriority ContextPriority
}

type SectionBudget struct {
	Section Section
	Tokens  int
}

type BudgetStatus struct {
	TotalTokens int
	Allocations []SectionBudget
}

type BudgetAllocator struct {
	weights map[Section]int
}

func NewBudgetAllocator() BudgetAllocator {
	return BudgetAllocator{weights: map[Section]int{
		SectionSkill:        20,
		SectionMemory:       30,
		SectionToolDef:      20,
		SectionConversation: 30,
	}}
}

func (a BudgetAllocator) Allocate(req BudgetRequest) (BudgetStatus, error) {
	if req.TotalTokens < 0 {
		return BudgetStatus{}, fmt.Errorf("budget total must be non-negative")
	}
	present := normalizeSections(req.PresentSections)
	if len(present) == 0 {
		present = []Section{SectionSkill, SectionMemory, SectionToolDef, SectionConversation}
	}
	if req.TotalTokens == 0 {
		return BudgetStatus{TotalTokens: req.TotalTokens, Allocations: zeroAllocations(present)}, nil
	}

	weightSum := 0
	for _, section := range present {
		weight := a.weights[section]
		if weight <= 0 {
			return BudgetStatus{}, fmt.Errorf("budget section %q is not recognized", section)
		}
		weightSum += weight
	}

	allocations := make([]SectionBudget, 0, len(present))
	allocated := 0
	for _, section := range present {
		tokens := req.TotalTokens * a.weights[section] / weightSum
		if tokens <= 0 {
			tokens = 1
		}
		allocations = append(allocations, SectionBudget{Section: section, Tokens: tokens})
		allocated += tokens
	}
	for idx := 0; allocated < req.TotalTokens; idx++ {
		allocations[idx%len(allocations)].Tokens++
		allocated++
	}
	allocations = applyPriorityHint(allocations, sectionForPriority(req.ContextPriority))
	return BudgetStatus{TotalTokens: req.TotalTokens, Allocations: allocations}, nil
}

func (p *defaultContextPlane) Budget(_ context.Context, req BudgetRequest) (BudgetStatus, error) {
	if p == nil {
		return BudgetStatus{}, fmt.Errorf("context plane is not initialized")
	}
	return p.budgetAllocator.Allocate(req)
}

func normalizeSections(sections []Section) []Section {
	seen := make(map[Section]bool, len(sections))
	result := make([]Section, 0, len(sections))
	for _, section := range sections {
		if section == "" || seen[section] {
			continue
		}
		seen[section] = true
		result = append(result, section)
	}
	return result
}

func zeroAllocations(sections []Section) []SectionBudget {
	allocations := make([]SectionBudget, 0, len(sections))
	for _, section := range sections {
		allocations = append(allocations, SectionBudget{Section: section})
	}
	return allocations
}

func sectionForPriority(priority ContextPriority) Section {
	switch priority {
	case PrioritySkill:
		return SectionSkill
	case PriorityConversation:
		return SectionConversation
	default:
		return ""
	}
}

func applyPriorityHint(allocations []SectionBudget, priority Section) []SectionBudget {
	if priority == "" {
		return allocations
	}
	priorityIdx := allocationIndex(allocations, priority)
	if priorityIdx < 0 {
		return allocations
	}
	target := allocations[priorityIdx].Tokens * 2
	delta := target - allocations[priorityIdx].Tokens
	if delta <= 0 {
		return allocations
	}
	allocations[priorityIdx].Tokens = target

	if delta <= 0 {
		return allocations
	}

	for _, section := range []Section{SectionConversation, SectionToolDef, SectionSkill, SectionMemory} {
		if section == priority {
			continue
		}
		delta = reduceAllocationAtLeast(allocations, section, 1, delta)
		if delta <= 0 {
			return allocations
		}
	}
	return allocations
}

func reduceAllocationAtLeast(allocations []SectionBudget, section Section, floor int, delta int) int {
	idx := allocationIndex(allocations, section)
	if idx < 0 || delta <= 0 {
		return delta
	}
	available := allocations[idx].Tokens - floor
	if available <= 0 {
		return delta
	}
	reduction := min(available, delta)
	allocations[idx].Tokens -= reduction
	return delta - reduction
}

func allocationIndex(allocations []SectionBudget, section Section) int {
	for i, allocation := range allocations {
		if allocation.Section == section {
			return i
		}
	}
	return -1
}
