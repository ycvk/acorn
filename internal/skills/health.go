package skills

import (
	"slices"
	"strings"
)

type HealthStatus string

const (
	HealthPassed HealthStatus = "passed"
	HealthFailed HealthStatus = "failed"
)

type HealthFailureKind string

const (
	HealthFailureInvalidSkill HealthFailureKind = "invalid_skill"
	HealthFailureEligibility  HealthFailureKind = "eligibility"
)

type HealthReport struct {
	Status   HealthStatus    `json:"status"`
	Failures []HealthFailure `json:"failures,omitempty"`
}

type HealthFailure struct {
	Kind    HealthFailureKind `json:"kind"`
	Skill   string            `json:"skill,omitempty"`
	Message string            `json:"message"`
}

// BuildHealthReport fails on stored skills that no longer parse and on skills
// the context cannot run.
func BuildHealthReport(scan ScanResult, ctx EligibilityContext) HealthReport {
	report := HealthReport{Status: HealthPassed}
	for _, problem := range scan.Problems {
		report.Failures = append(report.Failures, HealthFailure{Kind: HealthFailureInvalidSkill, Skill: problem.Name, Message: problem.Error})
	}
	for _, item := range scan.Skills {
		if view := Evaluate(item, ctx); !view.Eligible {
			report.Failures = append(report.Failures, HealthFailure{Kind: HealthFailureEligibility, Skill: item.Name, Message: strings.Join(view.DisabledReasons, ";")})
		}
	}
	if len(report.Failures) > 0 {
		report.Status = HealthFailed
	}
	return report
}

func CopyHealthReport(item HealthReport) HealthReport {
	return HealthReport{Status: item.Status, Failures: slices.Clone(item.Failures)}
}
