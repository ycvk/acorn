package contextplane

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
)

func TestDefaultContextPlaneAssembleBuildsContextMessagesWithPreparedMemory(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 2000,
		TokenCounter:             testTokenCounter(t),
	})

	result, err := plane.Assemble(context.Background(), AssembleRequest{
		SessionID: "session-1",
		Input:     "show repo structure",
		SelectedSkill: &SelectedSkill{
			Skill: skillsSpecWithBrief("skill.inspect.repo", "Inspect repo"),
			Score: 10,
		},
		SkillSnapshot: &skills.Snapshot{
			Skills: []skills.View{{
				Spec: skills.Spec{
					ID:      "skill.web.browser.research",
					Name:    "Web Browser Research",
					Summary: "Search, fetch, and browse the web.",
				},
				Eligible: true,
			}},
		},
		MemoryPrepared: &memory.PrepareResult{
			Nudges: []memory.Nudge{{
				Ref:    "facts/workspaces/acorn/runtime.md",
				Kind:   "fact",
				Title:  "Runtime contract",
				Status: "verified",
				Reason: "matched input",
			}},
			Entries: []memory.Entry{{
				Ref:     "facts/workspaces/acorn/runtime.md",
				Kind:    "fact",
				Title:   "Runtime contract",
				Content: "verified prepared memory",
			}},
		},
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if got, want := len(result.Messages), 3; got != want {
		t.Fatalf("messages = %d, want %d", got, want)
	}
	if !strings.Contains(result.Messages[0].Content, "<skill-context>") {
		t.Fatalf("first message should be skill context: %q", result.Messages[0].Content)
	}
	if !strings.Contains(result.Messages[1].Content, "<skill-catalog>") {
		t.Fatalf("second message should be skill catalog: %q", result.Messages[1].Content)
	}
	if !strings.Contains(result.Messages[1].Content, "skill.web.browser.research") {
		t.Fatalf("skill catalog missing expected entry: %q", result.Messages[1].Content)
	}
	memoryContent := result.Messages[2].Content
	for _, fragment := range []string{"<memory-context>", "## Memory Nudges", "## Memory Entries", "facts/workspaces/acorn/runtime.md", "verified prepared memory"} {
		if !strings.Contains(memoryContent, fragment) {
			t.Fatalf("memory content missing %q:\n%s", fragment, memoryContent)
		}
	}
	for _, forbidden := range []string{"## Retrieval Cards", "hydrate_memory_refs", "retrieval refs helper"} {
		if strings.Contains(memoryContent, forbidden) {
			t.Fatalf("memory content contains old retrieval fragment %q:\n%s", forbidden, memoryContent)
		}
	}
}

func TestDefaultContextPlaneAssembleInjectsPreparedMemoryEntry(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 2000,
		TokenCounter:             testTokenCounter(t),
	})
	result, err := plane.Assemble(context.Background(), AssembleRequest{
		RunID:     "run_context",
		SessionID: "session_context",
		Input:     "known preference",
		MemoryPrepared: &memory.PrepareResult{
			Entries: []memory.Entry{{
				Ref:     "skills/learned/preference.md#preference",
				Kind:    "skill",
				Title:   "Preference",
				Content: "Use concise Chinese responses.",
			}},
		},
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if result == nil || len(result.Messages) == 0 {
		t.Fatal("expected assembled memory message")
	}
	if !strings.Contains(result.Messages[0].Content, "Use concise Chinese responses.") {
		t.Fatalf("prepared memory missing:\n%s", result.Messages[0].Content)
	}
}

func TestDefaultContextPlaneAssembleWorksWithoutPreparedMemory(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemoryContextTokenBudget: 2000,
		TokenCounter:             testTokenCounter(t),
	})
	result, err := plane.Assemble(context.Background(), AssembleRequest{
		RunID:     "run_sop_only",
		SessionID: "session_sop_only",
		Input:     "activate procedure",
	})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if result == nil {
		t.Fatal("expected assemble result")
	}
}

func TestBudgetedContextMessagesSkipsWhenMaxContextTokensZero(t *testing.T) {
	messages := []*schema.Message{
		schema.SystemMessage("system prompt"),
		schema.UserMessage("<memory-context>\n## Memory Entries\n- fact:1 one\n</memory-context>"),
	}

	result, err := budgetedContextMessages(context.Background(), testTokenCounter(t), 0, messages)
	if err != nil {
		t.Fatalf("budgetedContextMessages: %v", err)
	}
	if got, want := len(result), len(messages); got != want {
		t.Fatalf("messages = %d, want %d", got, want)
	}
	if result[1].Content != messages[1].Content {
		t.Fatalf("memory context changed when max=0: %q", result[1].Content)
	}
}

func TestBudgetedContextMessagesFailsWhenAssembledContextExceedsBudget(t *testing.T) {
	messages := []*schema.Message{
		schema.UserMessage("<skill-context>\n" + strings.Repeat("skill ", 80) + "\n</skill-context>"),
		schema.UserMessage("<memory-context>\n" + strings.Repeat("memory ", 80) + "\n</memory-context>"),
	}

	_, err := budgetedContextMessages(context.Background(), testTokenCounter(t), 10, messages)
	if err == nil || !strings.Contains(err.Error(), "assembled context requires") {
		t.Fatalf("error = %v, want assembled context budget error", err)
	}
}

func skillsSpecWithBrief(id, summary string) skills.Spec {
	return skills.Spec{
		ID:      id,
		Name:    id,
		Summary: summary,
	}
}
