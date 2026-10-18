package cli

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

func TestRenderSkillsCheckUsesHealthReport(t *testing.T) {
	body := renderSkillsCheck(skills.HealthReport{
		Status: skills.HealthFailed,
		Failures: []skills.HealthFailure{{
			Kind:    skills.HealthFailureEligibility,
			Skill:   "memory-triage",
			Message: "missing_required_tools:memory_search",
		}},
	})

	for _, want := range []string{
		"Status: failed",
		"failure eligibility: memory-triage: missing_required_tools:memory_search",
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
			Skill:   "ineligible",
			Message: "missing_required_tools:read_file",
		}},
	}
	err := skillCheckError(failed)
	if err == nil || !strings.Contains(err.Error(), "eligibility: ineligible") {
		t.Fatalf("skillCheckError failed report = %v, want eligibility failure", err)
	}
}

func TestRenderSkillDetailShowsFiles(t *testing.T) {
	body := renderSkillDetail(skills.View{Spec: skills.Spec{
		Name: "pdf", Description: "Fill PDF forms.", License: "MIT", Revision: 2, Instruction: "Run scripts/fill.py.",
		Files: []core.SkillFileInfo{{Path: "scripts/fill.py", Size: 12}},
	}, Eligible: true})
	for _, want := range []string{"Name: pdf", "Revision: 2", "License: MIT", "File: scripts/fill.py (12 bytes)", "Run scripts/fill.py."} {
		if !strings.Contains(body, want) {
			t.Fatalf("renderSkillDetail missing %q:\n%s", want, body)
		}
	}
}

func TestSecretValuesDropOneTrailingNewline(t *testing.T) {
	for in, want := range map[string]string{"ghp_1\n": "ghp_1", "ghp_1\r\n": "ghp_1", "a b\n\n": "a b\n", "ghp_1": "ghp_1"} {
		if got, err := secretValue([]byte(in)); err != nil || got != want {
			t.Fatalf("secretValue(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "\n", strings.Repeat("x", skills.MaxSecretBytes+1)} {
		if _, err := secretValue([]byte(in)); err == nil {
			t.Fatalf("secretValue(%q) succeeded", in)
		}
	}
}
