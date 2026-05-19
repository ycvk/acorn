package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/skills"
)

func TestRenderSkillsCheckUsesHealthReport(t *testing.T) {
	body := renderSkillsCheck(skills.HealthReport{
		Status: skills.HealthFailed,
		Failures: []skills.HealthFailure{{
			Kind:    skills.HealthFailureEligibility,
			SkillID: "skill.procedure.curator",
			Message: "missing_required_tools:memory_search_text",
		}},
		Observations: []skills.HealthObservation{{
			Kind:    skills.HealthObservationLowRoutingMetadata,
			SkillID: "skill.inspect.repo",
			Message: "skill relies on weak name-only routing metadata",
		}},
		Fixtures: []skills.RoutingFixtureResult{{
			ID:            "inspect",
			Status:        skills.HealthFailed,
			ExpectedSkill: "skill.inspect.repo",
			ActualSkill:   "skill.ship.patch",
			Candidates:    []string{"skill.ship.patch", "skill.inspect.repo"},
			Error:         "expected top eligible skill",
		}},
	})

	for _, want := range []string{
		"Status: failed",
		"failure eligibility: skill.procedure.curator: missing_required_tools:memory_search_text",
		"observation low_routing_metadata: skill.inspect.repo",
		"fixture inspect failed expected=skill.inspect.repo actual=skill.ship.patch candidates=skill.ship.patch,skill.inspect.repo",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderSkillsCheck missing %q:\n%s", want, body)
		}
	}
}

func TestSkillCheckErrorFailsOnlyOnHealthFailure(t *testing.T) {
	passed := skills.HealthReport{
		Status:       skills.HealthPassed,
		Observations: []skills.HealthObservation{{Kind: skills.HealthObservationLowRoutingMetadata, SkillID: "skill.inspect.repo"}},
	}
	if err := skillCheckError(passed); err != nil {
		t.Fatalf("skillCheckError passed report = %v, want nil", err)
	}

	failed := skills.HealthReport{
		Status: skills.HealthFailed,
		Failures: []skills.HealthFailure{{
			Kind:    skills.HealthFailureUnreachable,
			SkillID: "skill.unreachable",
			Message: "active skill has no routing metadata",
		}},
	}
	err := skillCheckError(failed)
	if err == nil || !strings.Contains(err.Error(), "unreachable: skill.unreachable") {
		t.Fatalf("skillCheckError failed report = %v, want unreachable failure", err)
	}
}

func TestLoadRoutingFixtures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixtures.yaml")
	if err := os.WriteFile(path, []byte(`
- id: inspect
  input: inspect this repo
  expected_skill: skill.inspect.repo
  must_not_select:
    - skill.ship.patch
`), 0o600); err != nil {
		t.Fatalf("write fixtures: %v", err)
	}

	fixtures, err := loadRoutingFixtures(path)
	if err != nil {
		t.Fatalf("loadRoutingFixtures: %v", err)
	}
	if len(fixtures) != 1 {
		t.Fatalf("fixtures = %d, want 1", len(fixtures))
	}
	if fixtures[0].ID != "inspect" || fixtures[0].ExpectedSkill != "skill.inspect.repo" {
		t.Fatalf("fixture = %#v, want parsed inspect fixture", fixtures[0])
	}
	if len(fixtures[0].MustNotSelect) != 1 || fixtures[0].MustNotSelect[0] != "skill.ship.patch" {
		t.Fatalf("must_not_select = %#v, want skill.ship.patch", fixtures[0].MustNotSelect)
	}
}
