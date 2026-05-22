package contextplane

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/skills"
)

func TestBuildMemoryMessageEmpty(t *testing.T) {
	got, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 2000}, "", "", nil)
	if err != nil {
		t.Fatalf("buildMemoryMessage: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for all-empty sections, got %+v", got)
	}
}

func TestBuildMemoryMessageContent(t *testing.T) {
	prepared := &memorymodule.PrepareResult{
		Nudges: []memorymodule.Nudge{{Ref: "fact:1", Kind: "fact", Title: "Fact 1", Status: "verified", Reason: "matched"}},
		Entries: []memorymodule.Entry{
			{Ref: "fact:1", Kind: "fact", Title: "Fact 1", Content: "fact1"},
			{Ref: "fact:2", Kind: "fact", Title: "Fact 2", Content: "fact2"},
		},
	}
	got, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 2000}, "", "checkpoint data", prepared)
	if err != nil {
		t.Fatalf("buildMemoryMessage: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil message, got nil")
	}
	if got.Role != schema.User {
		t.Fatalf("expected role=User, got %v", got.Role)
	}
	c := got.Content
	if !strings.Contains(c, "<memory-context>") {
		t.Fatalf("missing opening tag: %q", c)
	}
	if !strings.Contains(c, "</memory-context>") {
		t.Fatalf("missing closing tag: %q", c)
	}
	if !strings.Contains(c, "NOT new user input") {
		t.Fatalf("missing disclaimer: %q", c)
	}
	if !strings.Contains(c, "checkpoint data") {
		t.Fatalf("missing checkpoint section: %q", c)
	}
	if !strings.Contains(c, "## Memory Nudges") || !strings.Contains(c, "## Memory Entries") || !strings.Contains(c, "fact:1 kind=fact title=Fact 1 fact1") || !strings.Contains(c, "fact:2 kind=fact title=Fact 2 fact2") {
		t.Fatalf("missing prepared memory section: %q", c)
	}
	if strings.Contains(c, "## Retrieval Cards") || strings.Contains(c, "hydrate_memory_refs") {
		t.Fatalf("memory context contains old retrieval guidance: %q", c)
	}
	idxCp := strings.Index(c, "checkpoint data")
	idxFacts := strings.Index(c, "## Memory Nudges")
	if !(idxCp < idxFacts) {
		t.Fatalf("sections out of order: checkpoint=%d facts=%d", idxCp, idxFacts)
	}
}

func TestBuildMemoryMessageRole(t *testing.T) {
	got, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 2000}, "", "cp", nil)
	if err != nil {
		t.Fatalf("buildMemoryMessage: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil message")
	}
	if got.Role != schema.User {
		t.Fatalf("expected schema.User, got %v", got.Role)
	}
}

func TestBuildMemoryMessagePartialContent(t *testing.T) {
	got, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 2000}, "", "checkpoint only", nil)
	if err != nil {
		t.Fatalf("buildMemoryMessage: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil message for partial content")
	}
	if !strings.Contains(got.Content, "checkpoint only") {
		t.Fatalf("missing checkpoint section: %q", got.Content)
	}
	if strings.Contains(got.Content, "## Verified Facts") {
		t.Fatalf("unexpected old verified facts section when prepared memory empty: %q", got.Content)
	}
	if strings.Contains(got.Content, "## Memory Entries") {
		t.Fatalf("unexpected memory entries section when prepared memory empty: %q", got.Content)
	}
}

func skillsSpecWithBrief(id, summary string) skills.Spec {
	return skills.Spec{
		ID:      id,
		Name:    id,
		Summary: summary,
	}
}

func TestBuildSkillContextMessageWithSkill(t *testing.T) {
	selected := &SelectedSkill{
		Skill: skillsSpecWithBrief("inspect_repo", "Inspect a repository"),
		Score: 10,
	}
	got := buildSkillContextMessage(selected)
	if got == nil {
		t.Fatal("expected non-nil message for selected skill")
	}
	if got.Role != schema.User {
		t.Fatalf("expected role=User, got %v", got.Role)
	}
	if !strings.Contains(got.Content, "<skill-context>") {
		t.Fatalf("missing <skill-context> tag: %q", got.Content)
	}
	if !strings.Contains(got.Content, "</skill-context>") {
		t.Fatalf("missing closing tag: %q", got.Content)
	}
	if !strings.Contains(got.Content, "Selected skill: inspect_repo") {
		t.Fatalf("missing skill brief content: %q", got.Content)
	}
}

func TestBuildSkillContextMessageNil(t *testing.T) {
	got := buildSkillContextMessage(nil)
	if got != nil {
		t.Fatalf("expected nil for nil selected skill, got %+v", got)
	}
}

func TestBuildSkillCatalogMessage(t *testing.T) {
	snapshot := &skills.Snapshot{
		Skills: []skills.View{
			{
				Spec: skills.Spec{
					ID:           "skill.web.browser.research",
					Name:         "Web Browser Research",
					Summary:      "Search, fetch, and browse the web.",
					TriggerHints: []string{"search web", "browse site", "open page"},
					Requires:     skills.Requirements{Tools: []string{"load_tools"}},
				},
				Eligible: true,
			},
			{
				Spec: skills.Spec{
					ID:              "skill.retired",
					Name:            "Retired",
					LifecycleStatus: skills.LifecycleRetired,
				},
				Eligible: true,
			},
			{
				Spec: skills.Spec{
					ID:      "skill.browser.disabled",
					Name:    "Browser Disabled",
					Summary: "Needs browser runtime.",
				},
				Eligible:        false,
				DisabledReasons: []string{"missing_required_tools:browser"},
			},
		},
	}
	got := buildSkillCatalogMessage(snapshot)
	if got == nil {
		t.Fatal("expected non-nil skill catalog message")
	}
	if !strings.Contains(got.Content, "<skill-catalog>") {
		t.Fatalf("missing <skill-catalog> tag: %q", got.Content)
	}
	if !strings.Contains(got.Content, "skill.web.browser.research") {
		t.Fatalf("missing browser research catalog entry: %q", got.Content)
	}
	if !strings.Contains(got.Content, "call skill_list or skill_view before answering") {
		t.Fatalf("missing catalog guidance: %q", got.Content)
	}
	if strings.Contains(got.Content, "skill.retired") {
		t.Fatalf("retired skill should not appear in catalog: %q", got.Content)
	}
	if !strings.Contains(got.Content, "disabled=missing_required_tools:browser") {
		t.Fatalf("missing disabled reason in catalog: %q", got.Content)
	}
}

func TestMemoryMessagePresentWhenDynamicContentExists(t *testing.T) {
	cases := []struct {
		name string
		cp   string
		fact string
	}{
		{"checkpoint only", "cp data", ""},
		{"facts only", "", "fact:1 fact1"},
		{"all present", "cp", "fact:1 fact1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var prepared *memorymodule.PrepareResult
			if strings.TrimSpace(tc.fact) != "" {
				prepared = &memorymodule.PrepareResult{Entries: []memorymodule.Entry{{Ref: "fact:1", Kind: "fact", Content: "fact1"}}}
			}
			got, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 2000}, "", tc.cp, prepared)
			if err != nil {
				t.Fatalf("buildMemoryMessage: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil MemoryMessage")
			}
		})
	}
}

func TestMemoryMessageNilWhenAllEmpty(t *testing.T) {
	got, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 2000}, "", "", nil)
	if err != nil {
		t.Fatalf("buildMemoryMessage: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil MemoryMessage for all-empty, got %+v", got)
	}
}

func TestInstructionSplitCoverage(t *testing.T) {
	base := "you are a helper"
	skill := &SelectedSkill{
		Skill: skillsSpecWithBrief("inspect", "Inspect repo"),
		Score: 10,
	}
	suffix := "be concise"
	cp := "checkpoint data"
	prepared := &memorymodule.PrepareResult{Entries: []memorymodule.Entry{{Ref: "fact:1", Kind: "fact", Content: "fact1"}}}

	stable := buildStableInstruction(base, suffix)
	skillMsg := buildSkillContextMessage(skill)
	memMsg, err := buildMemoryMessageForTest(context.Background(), testTokenCounter(t), LayeredMemoryBudget{L2InitialTokens: 2000}, "", cp, prepared)
	if err != nil {
		t.Fatalf("buildMemoryMessage: %v", err)
	}

	for _, fragment := range []string{base, suffix} {
		if !strings.Contains(stable, fragment) {
			t.Fatalf("stable instruction missing stable fragment %q\nstable=%q", fragment, stable)
		}
	}

	if skillMsg == nil {
		t.Fatal("expected non-nil skill context message for selected skill")
	}
	if !strings.Contains(skillMsg.Content, "Selected skill: inspect") {
		t.Fatalf("skill context message missing skill brief\ncontent=%q", skillMsg.Content)
	}

	if memMsg == nil {
		t.Fatal("expected non-nil memory message when dynamic content present")
	}
	for _, fragment := range []string{cp, "fact:1 kind=fact fact1"} {
		if !strings.Contains(memMsg.Content, fragment) {
			t.Fatalf("memory message missing dynamic fragment %q\ncontent=%q", fragment, memMsg.Content)
		}
	}

	for _, fragment := range []string{cp, "fact:1 kind=fact fact1"} {
		if strings.Contains(stable, fragment) {
			t.Fatalf("stable instruction should not contain dynamic fragment %q\nstable=%q", fragment, stable)
		}
	}
	if strings.Contains(stable, "Selected skill") {
		t.Fatalf("stable instruction should not contain skill brief\nstable=%q", stable)
	}

	for _, fragment := range []string{base, suffix} {
		if strings.Contains(memMsg.Content, fragment) {
			t.Fatalf("memory message should not contain stable fragment %q\ncontent=%q", fragment, memMsg.Content)
		}
	}
	for _, fragment := range []string{base, suffix} {
		if strings.Contains(skillMsg.Content, fragment) {
			t.Fatalf("skill context message should not contain stable fragment %q\ncontent=%q", fragment, skillMsg.Content)
		}
	}
}

func buildStableInstruction(base string, instructionSuffix string) string {
	parts := []string{
		strings.TrimSpace(base),
		strings.TrimSpace(instructionSuffix),
	}
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return strings.Join(out, "\n\n")
}
