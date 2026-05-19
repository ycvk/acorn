package skills

import (
	"strings"
	"testing"
)

func TestBuildHealthReportFailsOnScanProblem(t *testing.T) {
	report, err := BuildHealthReport(ScanResult{
		Problems: []Problem{{
			ID:    "skill.bad",
			Path:  "/tmp/skill.bad/SKILL.md",
			Error: "parse skill markdown: invalid frontmatter",
		}},
	}, testEligibleCtx(), nil)
	if err != nil {
		t.Fatalf("BuildHealthReport: %v", err)
	}
	if report.Status != HealthFailed {
		t.Fatalf("status = %q, want failed", report.Status)
	}
	if !hasHealthFailure(report, HealthFailureLoaderProblem, "skill.bad", "invalid frontmatter") {
		t.Fatalf("failures = %#v, want loader problem", report.Failures)
	}
}

func TestBuildHealthReportRoutingFixturePassesAndFails(t *testing.T) {
	scan := ScanResult{
		Skills: []Spec{
			{
				ID:           "skill.inspect.repo",
				Name:         "Inspect Repo",
				Source:       WorkspaceScope,
				Summary:      "Inspect repository structure.",
				TriggerHints: []string{"inspect repo"},
				Requires:     Requirements{Tools: []string{"read_file"}},
			},
			{
				ID:           "skill.ship.patch",
				Name:         "Ship Patch",
				Source:       WorkspaceScope,
				Summary:      "Implement and verify code changes.",
				TriggerHints: []string{"ship patch"},
				Requires:     Requirements{Tools: []string{"apply_unified_patch"}},
			},
		},
	}
	report, err := BuildHealthReport(scan, testEligibleCtx(), []RoutingFixture{
		{ID: "inspect", Input: "inspect repo before changing code", ExpectedSkill: "skill.inspect.repo"},
		{ID: "mismatch", Input: "ship patch now", ExpectedSkill: "skill.inspect.repo"},
	})
	if err != nil {
		t.Fatalf("BuildHealthReport: %v", err)
	}
	if report.Status != HealthFailed {
		t.Fatalf("status = %q, want failed because mismatch fixture fails", report.Status)
	}
	if len(report.Fixtures) != 2 {
		t.Fatalf("fixtures = %d, want 2", len(report.Fixtures))
	}
	if report.Fixtures[0].Status != HealthPassed || report.Fixtures[0].ActualSkill != "skill.inspect.repo" {
		t.Fatalf("first fixture = %#v, want passed inspect", report.Fixtures[0])
	}
	if report.Fixtures[1].Status != HealthFailed || report.Fixtures[1].ActualSkill != "skill.ship.patch" {
		t.Fatalf("second fixture = %#v, want failed with ship.patch actual", report.Fixtures[1])
	}
	if !hasHealthFailure(report, HealthFailureRoutingFixture, "skill.inspect.repo", "expected top eligible skill") {
		t.Fatalf("failures = %#v, want routing fixture failure", report.Failures)
	}
}

func TestBuildHealthReportFailsUnreachableAndEligibility(t *testing.T) {
	scan := ScanResult{
		Skills: []Spec{
			{
				ID:       "skill.unreachable",
				Name:     "Solo",
				Source:   WorkspaceScope,
				Requires: Requirements{Tools: []string{"read_file"}},
			},
			{
				ID:           "skill.needs.write",
				Name:         "Needs Write",
				Source:       WorkspaceScope,
				Summary:      "Needs unavailable tool.",
				TriggerHints: []string{"needs write"},
				Requires:     Requirements{Tools: []string{"missing_tool"}},
			},
		},
	}
	report, err := BuildHealthReport(scan, testEligibleCtx(), nil)
	if err != nil {
		t.Fatalf("BuildHealthReport: %v", err)
	}
	if report.Status != HealthFailed {
		t.Fatalf("status = %q, want failed", report.Status)
	}
	if !hasHealthFailure(report, HealthFailureUnreachable, "skill.unreachable", "no routing metadata") {
		t.Fatalf("failures = %#v, want unreachable failure", report.Failures)
	}
	if !hasHealthFailure(report, HealthFailureEligibility, "skill.needs.write", "missing_required_tools:missing_tool") {
		t.Fatalf("failures = %#v, want eligibility failure", report.Failures)
	}
}

func TestBuildHealthReportFailsDuplicateTriggers(t *testing.T) {
	scan := ScanResult{
		Skills: []Spec{
			{
				ID:           "skill.one",
				Name:         "One",
				Source:       WorkspaceScope,
				Summary:      "First skill.",
				TriggerHints: []string{"inspect repo"},
			},
			{
				ID:           "skill.two",
				Name:         "Two",
				Source:       WorkspaceScope,
				Summary:      "Second skill.",
				TriggerHints: []string{"inspect repo"},
			},
		},
	}
	report, err := BuildHealthReport(scan, testEligibleCtx(), nil)
	if err != nil {
		t.Fatalf("BuildHealthReport: %v", err)
	}
	if report.Status != HealthFailed {
		t.Fatalf("status = %q, want failed", report.Status)
	}
	if !hasHealthFailure(report, HealthFailureDuplicateTrigger, "skill.two", "duplicate routing trigger") {
		t.Fatalf("failures = %#v, want duplicate trigger failure", report.Failures)
	}
}

func TestBuildHealthReportPassesHealthySkillSet(t *testing.T) {
	scan := ScanResult{
		Skills: []Spec{
			{
				ID:           "skill.inspect.repo",
				Name:         "Inspect Repo",
				Source:       WorkspaceScope,
				Summary:      "Inspect repository structure.",
				TriggerHints: []string{"inspect repo"},
				Requires:     Requirements{Tools: []string{"read_file"}},
			},
		},
	}
	report, err := BuildHealthReport(scan, testEligibleCtx(), []RoutingFixture{
		{ID: "inspect", Input: "inspect repo before changing code", ExpectedSkill: "skill.inspect.repo"},
	})
	if err != nil {
		t.Fatalf("BuildHealthReport: %v", err)
	}
	if report.Status != HealthPassed {
		t.Fatalf("status = %q, failures=%#v", report.Status, report.Failures)
	}
}

func hasHealthFailure(report *HealthReport, kind HealthFailureKind, skillID string, contains string) bool {
	if report == nil {
		return false
	}
	for _, failure := range report.Failures {
		if failure.Kind != kind || failure.SkillID != skillID {
			continue
		}
		if strings.Contains(failure.Message, contains) {
			return true
		}
	}
	return false
}
