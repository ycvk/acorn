package stream

import (
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

// skillMap extracts the nested "skill" payload map that streamPayloadMap
// produces after re-encoding the *domain.StreamSkill via JSON.
func skillMap(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	raw, ok := body["skill"]
	if !ok || raw == nil {
		t.Fatalf("missing skill payload: %#v", body)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("skill payload not a map: %#v", raw)
	}
	return m
}

func getStringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func TestProjectStreamItemToEventProjectsSkillPayload(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, domain.StreamItem{
		RunID: "run_5",
		Kind:  domain.StreamKindSkillSelected,
		Payload: map[string]any{"skill": &domain.StreamSkill{
			SelectedID:   "skill.inspect.repo",
			Name:         "Inspect Repo",
			Source:       "workspace",
			Path:         "/tmp/skills/inspect_repo",
			Instruction:  "Read README.md first.",
			Scripts:      []string{"scripts/quick_map.sh"},
			Requirements: domain.StreamSkillRequirements{Tools: []string{"read_file", "run_command"}},
			Score:        145,
		}},
	})
	if kind != "skill.selected" {
		t.Fatalf("kind = %q, want skill.selected", kind)
	}
	body := payload.(map[string]any)
	skill := skillMap(t, body)
	if got := getStringField(skill, "selected_id"); got != "skill.inspect.repo" {
		t.Fatalf("unexpected selected_id: %q", got)
	}
	if got, want := getStringField(skill, "name"), "Inspect Repo"; got != want {
		t.Fatalf("unexpected name: %q, want %q", got, want)
	}
	if got, want := getStringField(skill, "source"), "workspace"; got != want {
		t.Fatalf("unexpected source: %q, want %q", got, want)
	}
	if got, want := getStringField(skill, "instruction"), "Read README.md first."; got != want {
		t.Fatalf("unexpected instruction: %q, want %q", got, want)
	}
	if got, want := getStringField(skill, "path"), "/tmp/skills/inspect_repo"; got != want {
		t.Fatalf("unexpected path: %q, want %q", got, want)
	}
}

func TestProjectStreamItemToEventProjectsNoSelectionReason(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, domain.StreamItem{
		RunID: "run_5b",
		Kind:  domain.StreamKindSkillDiscovered,
		Payload: map[string]any{"skill": &domain.StreamSkill{
			NoSelectionReason: "no_eligible_match",
			Candidates: []domain.StreamSkillCandidate{
				{ID: "skill.inspect.repo", FilteredReason: "missing_required_tools:read_file"},
			},
		}},
	})
	if kind != "skill.discovered" {
		t.Fatalf("kind = %q, want skill.discovered", kind)
	}
	body := payload.(map[string]any)
	skill := skillMap(t, body)
	if got, want := getStringField(skill, "no_selection_reason"), "no_eligible_match"; got != want {
		t.Fatalf("no_selection_reason = %q, want %q", got, want)
	}
	candidatesRaw, ok := skill["candidates"].([]any)
	if !ok || len(candidatesRaw) != 1 {
		t.Fatalf("candidates = %#v, want 1 entry", skill["candidates"])
	}
	cand, ok := candidatesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("candidate not a map: %#v", candidatesRaw[0])
	}
	if got, want := getStringField(cand, "filtered_reason"), "missing_required_tools:read_file"; got != want {
		t.Fatalf("filtered_reason = %q, want %q", got, want)
	}
}

func TestProjectStreamItemToEventProjectsSkillFailureReason(t *testing.T) {
	kind, payload := mustProjectStreamItemToEvent(t, domain.StreamItem{
		RunID: "run_6",
		Kind:  domain.StreamKindSkillFailed,
		Payload: map[string]any{"skill": &domain.StreamSkill{
			SelectedID:    "skill.inspect.repo",
			FailureReason: "missing_output_term:entrypoint",
		}},
	})
	if kind != "skill.failed" {
		t.Fatalf("kind = %q, want skill.failed", kind)
	}
	body := payload.(map[string]any)
	skill := skillMap(t, body)
	if got, want := getStringField(skill, "failure_reason"), "missing_output_term:entrypoint"; got != want {
		t.Fatalf("unexpected failure_reason: %q, want %q", got, want)
	}
}
