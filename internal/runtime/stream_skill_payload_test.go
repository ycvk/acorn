package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/stream"
)

func TestProjectStreamItemToEventProjectsSkillPayload(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_5",
		Kind:  stream.StreamKindSkillSelected,
		Payload: map[string]any{"skill": &stream.StreamSkill{
			SelectedID:   "skill.inspect.repo",
			Name:         "Inspect Repo",
			Source:       "workspace",
			Path:         "/tmp/skills/inspect_repo",
			Instruction:  "Read README.md first.",
			Scripts:      []string{"scripts/quick_map.sh"},
			Requirements: stream.StreamSkillRequirements{Tools: []string{"read_file", "run_command"}},
			Score:        145,
		}},
	})
	if kind != "skill.selected" {
		t.Fatalf("kind = %q, want skill.selected", kind)
	}
	body := payload.(map[string]any)
	skill := stream.StreamItem{Payload: body}.GetSkill()
	if skill == nil {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if skill.SelectedID != "skill.inspect.repo" {
		t.Fatalf("unexpected selected_id: %#v", skill)
	}
	if skill.Name != "Inspect Repo" || skill.Source != "workspace" || skill.Instruction != "Read README.md first." {
		t.Fatalf("unexpected metadata payload: %#v", skill)
	}
	if skill.Path != "/tmp/skills/inspect_repo" {
		t.Fatalf("unexpected skill path payload: %#v", skill)
	}
}

func TestProjectStreamItemToEventProjectsNoSelectionReason(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_5b",
		Kind:  stream.StreamKindSkillDiscovered,
		Payload: map[string]any{"skill": &stream.StreamSkill{
			NoSelectionReason: "no_eligible_match",
			Candidates: []stream.StreamSkillCandidate{
				{ID: "skill.inspect.repo", FilteredReason: "missing_required_tools:read_file"},
			},
		}},
	})
	if kind != "skill.discovered" {
		t.Fatalf("kind = %q, want skill.discovered", kind)
	}
	body := payload.(map[string]any)
	skill := stream.StreamItem{Payload: body}.GetSkill()
	if skill == nil {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if got, want := skill.NoSelectionReason, "no_eligible_match"; got != want {
		t.Fatalf("no_selection_reason = %#v, want %#v", got, want)
	}
	if len(skill.Candidates) != 1 {
		t.Fatalf("candidates count = %d, want 1", len(skill.Candidates))
	}
	if got, want := skill.Candidates[0].FilteredReason, "missing_required_tools:read_file"; got != want {
		t.Fatalf("filtered_reason = %#v, want %#v", got, want)
	}
}

func TestProjectStreamItemToEventProjectsSkillFailureReason(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_6",
		Kind:  stream.StreamKindSkillFailed,
		Payload: map[string]any{"skill": &stream.StreamSkill{
			SelectedID:    "skill.inspect.repo",
			FailureReason: "missing_output_term:entrypoint",
		}},
	})
	if kind != "skill.failed" {
		t.Fatalf("kind = %q, want skill.failed", kind)
	}
	body := payload.(map[string]any)
	skill := stream.StreamItem{Payload: body}.GetSkill()
	if skill == nil {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if skill.FailureReason != "missing_output_term:entrypoint" {
		t.Fatalf("unexpected failure_reason: %#v", skill)
	}
}

func TestProjectStreamItemToEventProjectsSkillLifecycle(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_7",
		Kind:  stream.StreamKindSkillLifecycle,
		Payload: map[string]any{"skill_lifecycle": &stream.StreamSkillLifecycle{
			SkillID:         "skill.generated",
			Action:          "assessed",
			Status:          "verified",
			Verdict:         "verified",
			Reason:          "durable evidence-backed promotion",
			EvidenceRefs:    []string{"child_run:run_eval"},
			AssessmentID:    "skill_assessment_1",
			ChangesRequired: []string{"none"},
			Applied:         true,
			Assessment:      map[string]any{"assessment_id": "skill_assessment_1"},
		}},
	})
	if kind != "skill.lifecycle" {
		t.Fatalf("kind = %q, want skill.lifecycle", kind)
	}
	body := payload.(map[string]any)
	lifecycle := stream.StreamItem{Payload: body}.GetSkillLifecycle()
	if lifecycle == nil {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if lifecycle.SkillID != "skill.generated" || lifecycle.Action != "assessed" {
		t.Fatalf("unexpected lifecycle payload: %#v", lifecycle)
	}
}

func TestSummarizeStreamItemsCountsCurrentSkillEvents(t *testing.T) {
	items := []stream.StreamItem{
		{Kind: stream.StreamKindSkillDiscovered, Payload: map[string]any{"skill": &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillSelected, Payload: map[string]any{"skill": &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillLoaded, Payload: map[string]any{"skill": &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillFailed, Payload: map[string]any{"skill": &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillLifecycle, Payload: map[string]any{"skill_lifecycle": &stream.StreamSkillLifecycle{SkillID: "s1", Action: "assessed"}}},
	}
	summary := stream.SummarizeStreamItems(items)
	if summary.SkillEventCount != 5 {
		t.Fatalf("SkillEventCount = %d, want 5", summary.SkillEventCount)
	}
	if !summary.SkillSelected {
		t.Fatalf("SkillSelected = false, want true")
	}
}
