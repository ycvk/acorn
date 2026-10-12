package skills

import (
	"fmt"
	"strings"
)

type HealthStatus string

const (
	HealthPassed HealthStatus = "passed"
	HealthFailed HealthStatus = "failed"
)

type HealthFailureKind string

const (
	HealthFailureInvalidSkill       HealthFailureKind = "invalid_skill"
	HealthFailureEligibility        HealthFailureKind = "eligibility"
	HealthFailureInvalidHealthInput HealthFailureKind = "invalid_health_input"
)

type HealthReport struct {
	Status   HealthStatus    `json:"status"`
	Failures []HealthFailure `json:"failures,omitempty"`
}

type HealthFailure struct {
	Kind    HealthFailureKind `json:"kind"`
	SkillID string            `json:"skill_id,omitempty"`
	Message string            `json:"message"`
}

func BuildHealthReport(scan ScanResult, ctx EligibilityContext) (*HealthReport, error) {
	report := &HealthReport{
		Status: HealthPassed,
	}
	for _, problem := range scan.Problems {
		report.addFailure(HealthFailure{
			Kind:    HealthFailureInvalidSkill,
			SkillID: problem.ID,
			Message: problem.Error,
		})
	}

	for _, item := range scan.Skills {
		current, err := NormalizeSpec(item)
		if err != nil {
			report.addFailure(HealthFailure{
				Kind:    HealthFailureInvalidHealthInput,
				SkillID: item.ID,
				Message: fmt.Sprintf("normalize skill for health: %v", err),
			})
			continue
		}
		view, err := Evaluate(current, ctx)
		if err != nil {
			report.addFailure(HealthFailure{
				Kind:    HealthFailureInvalidHealthInput,
				SkillID: current.ID,
				Message: fmt.Sprintf("evaluate skill for health: %v", err),
			})
			continue
		}
		if !view.Eligible {
			report.addFailure(HealthFailure{
				Kind:    HealthFailureEligibility,
				SkillID: current.ID,
				Message: strings.Join(view.DisabledReasons, ";"),
			})
		}
	}

	if len(report.Failures) > 0 {
		report.Status = HealthFailed
	}
	return report, nil
}

func CopyHealthReport(item HealthReport) HealthReport {
	return HealthReport{
		Status:   item.Status,
		Failures: append([]HealthFailure(nil), item.Failures...),
	}
}

func (r *HealthReport) addFailure(failure HealthFailure) {
	if r == nil {
		return
	}
	failure.SkillID = strings.TrimSpace(failure.SkillID)
	failure.Message = strings.TrimSpace(failure.Message)
	r.Failures = append(r.Failures, failure)
	r.Status = HealthFailed
}
