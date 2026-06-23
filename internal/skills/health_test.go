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
			Error: "invalid frontmatter",
		}},
	}, testEligibleCtx())
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

func TestBuildHealthReportFailsOnEligibility(t *testing.T) {
	scan := ScanResult{
		Skills: []Spec{
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
	report, err := BuildHealthReport(scan, testEligibleCtx())
	if err != nil {
		t.Fatalf("BuildHealthReport: %v", err)
	}
	if report.Status != HealthFailed {
		t.Fatalf("status = %q, want failed", report.Status)
	}
	if !hasHealthFailure(report, HealthFailureEligibility, "skill.needs.write", "missing_required_tools:missing_tool") {
		t.Fatalf("failures = %#v, want eligibility failure", report.Failures)
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
	report, err := BuildHealthReport(scan, testEligibleCtx())
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

func TestCopyHealthReportDeepCopy(t *testing.T) {
	original := HealthReport{
		Status: HealthFailed,
		Failures: []HealthFailure{
			{Kind: HealthFailureEligibility, SkillID: "x", Message: "msg"},
		},
	}
	copy := CopyHealthReport(original)
	copy.Failures[0].SkillID = "mutated"
	if original.Failures[0].SkillID != "x" {
		t.Error("CopyHealthReport did not deep-copy Failures slice")
	}
}

func TestAddFailureAppends(t *testing.T) {
	report := &HealthReport{Status: HealthPassed}
	report.addFailure(HealthFailure{Kind: HealthFailureLoaderProblem, Message: "err"})
	if report.Status != HealthFailed {
		t.Errorf("Status = %q, addFailure should set Status to failed", report.Status)
	}
	if len(report.Failures) != 1 {
		t.Fatalf("Failures = %d, want 1", len(report.Failures))
	}
}
