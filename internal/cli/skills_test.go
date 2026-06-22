package cli

import (
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
			Message: "missing_required_tools:memory_search",
		}},
	})

	for _, want := range []string{
		"Status: failed",
		"failure eligibility: skill.procedure.curator: missing_required_tools:memory_search",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderSkillsCheck missing %q:\n%s", want, body)
		}
	}
}

func TestSkillCheckErrorFailsOnlyOnHealthFailure(t *testing.T) {
	passed := skills.HealthReport{
		Status: skills.HealthPassed,
	}
	if err := skillCheckError(passed); err != nil {
		t.Fatalf("skillCheckError passed report = %v, want nil", err)
	}

	failed := skills.HealthReport{
		Status: skills.HealthFailed,
		Failures: []skills.HealthFailure{{
			Kind:    skills.HealthFailureEligibility,
			SkillID: "skill.ineligible",
			Message: "missing_required_tools:read_file",
		}},
	}
	err := skillCheckError(failed)
	if err == nil || !strings.Contains(err.Error(), "eligibility: skill.ineligible") {
		t.Fatalf("skillCheckError failed report = %v, want eligibility failure", err)
	}
}
