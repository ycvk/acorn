package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/stream"
)

func TestProjectStreamItemToEventProjectsSkillPayload(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_5",
		Kind:  stream.StreamKindSkillSelected,
		Payload: &stream.SkillSelectedPayload{Skill: &stream.StreamSkill{
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
	skillBody, ok := body["skill"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if skillBody["selected_id"] != "skill.inspect.repo" {
		t.Fatalf("unexpected selected_id: %#v", skillBody)
	}
	if skillBody["name"] != "Inspect Repo" || skillBody["source"] != "workspace" || skillBody["instruction"] != "Read README.md first." {
		t.Fatalf("unexpected metadata payload: %#v", skillBody)
	}
	if skillBody["path"] != "/tmp/skills/inspect_repo" {
		t.Fatalf("unexpected skill path payload: %#v", skillBody)
	}
}

func TestProjectStreamItemToEventProjectsNoSelectionReason(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_5b",
		Kind:  stream.StreamKindSkillDiscovered,
		Payload: &stream.SkillDiscoveredPayload{Skill: &stream.StreamSkill{
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
	skillBody := body["skill"].(map[string]any)
	if got, want := skillBody["no_selection_reason"], "no_eligible_match"; got != want {
		t.Fatalf("no_selection_reason = %#v, want %#v", got, want)
	}
	candidates := skillBody["candidates"].([]any)
	c0 := candidates[0].(map[string]any)
	if got, want := c0["filtered_reason"], "missing_required_tools:read_file"; got != want {
		t.Fatalf("filtered_reason = %#v, want %#v", got, want)
	}
}

func TestProjectStreamItemToEventProjectsSkillFailureReason(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_6",
		Kind:  stream.StreamKindSkillFailed,
		Payload: &stream.SkillFailedPayload{Skill: &stream.StreamSkill{
			SelectedID:    "skill.inspect.repo",
			FailureReason: "missing_output_term:entrypoint",
		}},
	})
	if kind != "skill.failed" {
		t.Fatalf("kind = %q, want skill.failed", kind)
	}
	body := payload.(map[string]any)
	skillBody, ok := body["skill"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if skillBody["failure_reason"] != "missing_output_term:entrypoint" {
		t.Fatalf("unexpected failure_reason: %#v", skillBody)
	}
}

func TestProjectStreamItemToEventProjectsSkillLifecycle(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, stream.StreamItem{
		RunID: "run_7",
		Kind:  stream.StreamKindSkillLifecycle,
		Payload: &stream.SkillLifecyclePayload{SkillLifecycle: &stream.StreamSkillLifecycle{
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
	lifecycleBody, ok := body["skill_lifecycle"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload: %#v", body)
	}
	if lifecycleBody["skill_id"] != "skill.generated" || lifecycleBody["action"] != "assessed" {
		t.Fatalf("unexpected lifecycle payload: %#v", lifecycleBody)
	}
}

func TestSummarizeStreamItemsCountsCurrentSkillEvents(t *testing.T) {
	items := []stream.StreamItem{
		{Kind: stream.StreamKindSkillDiscovered, Payload: &stream.SkillDiscoveredPayload{Skill: &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillSelected, Payload: &stream.SkillSelectedPayload{Skill: &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillLoaded, Payload: &stream.SkillLoadedPayload{Skill: &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillFailed, Payload: &stream.SkillFailedPayload{Skill: &stream.StreamSkill{SelectedID: "s1"}}},
		{Kind: stream.StreamKindSkillLifecycle, Payload: &stream.SkillLifecyclePayload{SkillLifecycle: &stream.StreamSkillLifecycle{SkillID: "s1", Action: "assessed"}}},
	}
	summary := SummarizeStreamItems(items)
	if summary.SkillEventCount != 5 {
		t.Fatalf("SkillEventCount = %d, want 5", summary.SkillEventCount)
	}
	if !summary.SkillSelected {
		t.Fatalf("SkillSelected = false, want true")
	}
}
