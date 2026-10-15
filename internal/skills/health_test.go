package skills

import "testing"

func TestHealthReportFailsOnBrokenAndUnusableSkills(t *testing.T) {
	report := BuildHealthReport(ScanResult{
		Skills:   []Spec{{Name: "shop-orders", RequiresTools: []string{"shell"}}, {Name: "plain"}},
		Problems: []Problem{{Name: "broken", Error: "invalid frontmatter"}},
	}, EligibilityContext{AvailableTools: []string{"files"}})
	if report.Status != HealthFailed || len(report.Failures) != 2 {
		t.Fatalf("report = %+v", report)
	}
	if f := report.Failures[0]; f.Kind != HealthFailureInvalidSkill || f.Skill != "broken" {
		t.Fatalf("first failure = %+v", f)
	}
	if f := report.Failures[1]; f.Kind != HealthFailureEligibility || f.Skill != "shop-orders" || f.Message != "missing_required_tools:shell" {
		t.Fatalf("second failure = %+v", f)
	}
	if ok := BuildHealthReport(ScanResult{Skills: []Spec{{Name: "plain"}}}, EligibilityContext{}); ok.Status != HealthPassed {
		t.Fatalf("healthy report = %+v", ok)
	}
}
