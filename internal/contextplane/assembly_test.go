package contextplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/runtimehistory"
)

type snapshotStoreStub struct{}

func (snapshotStoreStub) SaveRunContextSnapshot(context.Context, runtimehistory.RunContextSnapshot) error {
	return nil
}

func (snapshotStoreStub) LoadRunContextSnapshot(context.Context, string) (*runtimehistory.RunContextSnapshot, error) {
	return nil, nil
}

type fakeSessionSummaryService struct {
	summary *runtimehistory.SessionSummary
}

func (s fakeSessionSummaryService) Get(context.Context, string) (*runtimehistory.SessionSummary, error) {
	return s.summary, nil
}

func TestDefaultContextPlaneAssembleBuildsContextMessagesWithPreparedMemory(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemorySearchTokenBudget: 2000,
		TokenCounter:            testTokenCounter(t),
		SessionSummaryService: fakeSessionSummaryService{summary: &runtimehistory.SessionSummary{
			SessionID:   "session-1",
			SourceRunID: "run-prev",
			RunStatus:   "succeeded",
			Summary:     "previous summary",
			UpdatedAt:   time.Now().UTC(),
		}},
	})

	result, err := plane.Assemble(context.Background(), AssembleRequest{
		SessionID: "session-1",
		Input:     "show repo structure",
		SelectedSkill: &SelectedSkill{
			Skill: skillsSpecWithBrief("skill.inspect.repo", "Inspect repo"),
			Score: 10,
		},
		MemoryPrepared: &memorymodule.PrepareResult{
			Nudges: []memorymodule.Nudge{{
				Ref:    "facts/workspaces/acorn/runtime.md",
				Kind:   "fact",
				Title:  "Runtime contract",
				Status: "verified",
				Reason: "matched input",
			}},
			Entries: []memorymodule.Entry{{
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
	if got, want := len(result.Messages), 2; got != want {
		t.Fatalf("messages = %d, want %d", got, want)
	}
	if !strings.Contains(result.Messages[0].Content, "<skill-context>") {
		t.Fatalf("first message should be skill context: %q", result.Messages[0].Content)
	}
	memoryContent := result.Messages[1].Content
	for _, fragment := range []string{"<memory-context>", "previous summary", "## Memory Nudges", "## Memory Entries", "facts/workspaces/acorn/runtime.md", "verified prepared memory"} {
		if !strings.Contains(memoryContent, fragment) {
			t.Fatalf("memory content missing %q:\n%s", fragment, memoryContent)
		}
	}
	for _, forbidden := range []string{"## Retrieval Cards", "hydrate_memory_refs", "retrieval refs helper"} {
		if strings.Contains(memoryContent, forbidden) {
			t.Fatalf("memory content contains old retrieval fragment %q:\n%s", forbidden, memoryContent)
		}
	}
	if sumBudget(result.BudgetUsed) != 2000 {
		t.Fatalf("budget sum = %d, want 2000", sumBudget(result.BudgetUsed))
	}
}

func TestDefaultContextPlaneAssembleInjectsPreparedMemoryEntry(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemorySearchTokenBudget: 2000,
		TokenCounter:            testTokenCounter(t),
		Store:                   snapshotStoreStub{},
	})
	result, err := plane.Assemble(context.Background(), AssembleRequest{
		RunID:     "run_context",
		SessionID: "session_context",
		Input:     "known preference",
		MemoryPrepared: &memorymodule.PrepareResult{
			Entries: []memorymodule.Entry{{
				Ref:     "skills/learned/preference.md#preference",
				Kind:    "skill",
				Title:   "Preference",
				Content: "Use concise Chinese responses.",
			}},
			ProcedureActivations: []memorymodule.ProcedureActivation{{
				RunID:        "run_context",
				SessionID:    "session_context",
				ProcedureRef: "skills/learned/preference.md#preference",
				Title:        "Preference",
				Kind:         "skill",
				Phase:        memorymodule.ProcedureActivationSelected,
				Reason:       "selected_for_prepared_memory_entry",
				Status:       memorymodule.StatusVerified,
				Origin:       memorymodule.ProcedureOriginActionVerified,
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
	if !hasContextProcedureActivation(result.ProcedureActivations, memorymodule.ProcedureActivationInjected, "skills/learned/preference.md#preference") {
		t.Fatalf("missing injected activation: %#v", result.ProcedureActivations)
	}
}

func TestDefaultContextPlaneAssembleWorksWithoutPreparedMemory(t *testing.T) {
	plane := NewDefaultContextPlane(DefaultOptions{
		MemorySearchTokenBudget: 2000,
		TokenCounter:            testTokenCounter(t),
		Store:                   snapshotStoreStub{},
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

func hasContextProcedureActivation(items []memorymodule.ProcedureActivation, phase memorymodule.ProcedureActivationPhase, ref string) bool {
	for _, item := range items {
		if item.Phase == phase && item.ProcedureRef == ref {
			return true
		}
	}
	return false
}
