package skills

import (
	"fmt"
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
	HealthFailureLoaderProblem      HealthFailureKind = "loader_problem"
	HealthFailureEligibility        HealthFailureKind = "eligibility"
	HealthFailureUnreachable        HealthFailureKind = "unreachable"
	HealthFailureDuplicateTrigger   HealthFailureKind = "duplicate_trigger"
	HealthFailureRoutingFixture     HealthFailureKind = "routing_fixture"
	HealthFailureInvalidHealthInput HealthFailureKind = "invalid_health_input"
)

type HealthObservationKind string

const (
	HealthObservationLowRoutingMetadata HealthObservationKind = "low_routing_metadata"
)

type RoutingFixture struct {
	ID            string   `json:"id" yaml:"id"`
	Input         string   `json:"input" yaml:"input"`
	ExpectedSkill string   `json:"expected_skill" yaml:"expected_skill"`
	MustNotSelect []string `json:"must_not_select,omitempty" yaml:"must_not_select,omitempty"`
}

type RoutingFixtureResult struct {
	ID            string       `json:"id"`
	Status        HealthStatus `json:"status"`
	Input         string       `json:"input"`
	ExpectedSkill string       `json:"expected_skill"`
	ActualSkill   string       `json:"actual_skill,omitempty"`
	Candidates    []string     `json:"candidates,omitempty"`
	Error         string       `json:"error,omitempty"`
}

type HealthReport struct {
	Status       HealthStatus           `json:"status"`
	Failures     []HealthFailure        `json:"failures,omitempty"`
	Observations []HealthObservation    `json:"observations,omitempty"`
	Fixtures     []RoutingFixtureResult `json:"fixtures,omitempty"`
}

type HealthFailure struct {
	Kind    HealthFailureKind `json:"kind"`
	SkillID string            `json:"skill_id,omitempty"`
	Path    string            `json:"path,omitempty"`
	Fixture string            `json:"fixture,omitempty"`
	Message string            `json:"message"`
}

type HealthObservation struct {
	Kind    HealthObservationKind `json:"kind"`
	SkillID string                `json:"skill_id,omitempty"`
	Path    string                `json:"path,omitempty"`
	Message string                `json:"message"`
}

func BuildHealthReport(scan ScanResult, ctx EligibilityContext, fixtures []RoutingFixture) (*HealthReport, error) {
	report := &HealthReport{
		Status: HealthPassed,
	}
	for _, problem := range scan.Problems {
		report.addFailure(HealthFailure{
			Kind:    HealthFailureLoaderProblem,
			SkillID: problem.ID,
			Path:    problem.Path,
			Message: problem.Error,
		})
	}

	normalized := make([]Spec, 0, len(scan.Skills))
	for _, item := range scan.Skills {
		current, err := NormalizeSpec(item)
		if err != nil {
			report.addFailure(HealthFailure{
				Kind:    HealthFailureInvalidHealthInput,
				SkillID: item.ID,
				Path:    item.Path,
				Message: fmt.Sprintf("normalize skill for health: %v", err),
			})
			continue
		}
		normalized = append(normalized, current)
		if current.LifecycleStatus == LifecycleRetired {
			continue
		}
		view, err := Evaluate(current, ctx)
		if err != nil {
			report.addFailure(HealthFailure{
				Kind:    HealthFailureInvalidHealthInput,
				SkillID: current.ID,
				Path:    current.Path,
				Message: fmt.Sprintf("evaluate skill for health: %v", err),
			})
			continue
		}
		if !view.Eligible {
			report.addFailure(HealthFailure{
				Kind:    HealthFailureEligibility,
				SkillID: current.ID,
				Path:    current.Path,
				Message: strings.Join(view.DisabledReasons, ";"),
			})
		}
		if !hasRoutingMetadata(current) {
			report.addFailure(HealthFailure{
				Kind:    HealthFailureUnreachable,
				SkillID: current.ID,
				Path:    current.Path,
				Message: "active skill has no routing metadata",
			})
			continue
		}
		if hasWeakRoutingMetadata(current) {
			report.Observations = append(report.Observations, HealthObservation{
				Kind:    HealthObservationLowRoutingMetadata,
				SkillID: current.ID,
				Path:    current.Path,
				Message: "skill relies on weak name-only routing metadata",
			})
		}
	}

	report.addDuplicateTriggerFailures(normalized)
	report.runRoutingFixtures(ctx, normalized, fixtures)
	if len(report.Failures) > 0 {
		report.Status = HealthFailed
	}
	return report, nil
}

func CopyHealthReport(item HealthReport) HealthReport {
	copy := HealthReport{
		Status:       item.Status,
		Failures:     append([]HealthFailure(nil), item.Failures...),
		Observations: append([]HealthObservation(nil), item.Observations...),
		Fixtures:     make([]RoutingFixtureResult, 0, len(item.Fixtures)),
	}
	for _, fixture := range item.Fixtures {
		current := fixture
		current.Candidates = append([]string(nil), fixture.Candidates...)
		copy.Fixtures = append(copy.Fixtures, current)
	}
	return copy
}

func (r *HealthReport) addFailure(failure HealthFailure) {
	if r == nil {
		return
	}
	failure.SkillID = strings.TrimSpace(failure.SkillID)
	failure.Path = strings.TrimSpace(failure.Path)
	failure.Fixture = strings.TrimSpace(failure.Fixture)
	failure.Message = strings.TrimSpace(failure.Message)
	r.Failures = append(r.Failures, failure)
	r.Status = HealthFailed
}

func (r *HealthReport) addDuplicateTriggerFailures(items []Spec) {
	seen := make(map[string]Spec)
	for _, item := range items {
		if item.LifecycleStatus == LifecycleRetired {
			continue
		}
		for _, trigger := range duplicateTriggerKeys(item) {
			existing, exists := seen[trigger]
			if !exists {
				seen[trigger] = item
				continue
			}
			r.addFailure(HealthFailure{
				Kind:    HealthFailureDuplicateTrigger,
				SkillID: item.ID,
				Path:    item.Path,
				Message: fmt.Sprintf("duplicate routing trigger %q also used by %s", trigger, existing.ID),
			})
		}
	}
}

func (r *HealthReport) runRoutingFixtures(ctx EligibilityContext, items []Spec, fixtures []RoutingFixture) {
	for _, fixture := range fixtures {
		result := runRoutingFixture(ctx, items, fixture)
		r.Fixtures = append(r.Fixtures, result)
		if result.Status == HealthFailed {
			r.addFailure(HealthFailure{
				Kind:    HealthFailureRoutingFixture,
				Fixture: result.ID,
				SkillID: result.ExpectedSkill,
				Message: result.Error,
			})
		}
	}
}

func runRoutingFixture(ctx EligibilityContext, items []Spec, fixture RoutingFixture) RoutingFixtureResult {
	result := RoutingFixtureResult{
		ID:            strings.TrimSpace(fixture.ID),
		Status:        HealthPassed,
		Input:         strings.TrimSpace(fixture.Input),
		ExpectedSkill: strings.TrimSpace(fixture.ExpectedSkill),
	}
	if result.ID == "" {
		result.Status = HealthFailed
		result.Error = "fixture id is required"
		return result
	}
	if result.Input == "" {
		result.Status = HealthFailed
		result.Error = "fixture input is required"
		return result
	}
	if result.ExpectedSkill == "" {
		result.Status = HealthFailed
		result.Error = "fixture expected_skill is required"
		return result
	}
	candidates, err := RetrieveCandidates(CandidateQuery{
		Input:       result.Input,
		Eligibility: ctx,
	}, items)
	if err != nil {
		result.Status = HealthFailed
		result.Error = err.Error()
		return result
	}
	result.Candidates = candidateIDs(candidates.Candidates)
	for _, candidate := range candidates.Candidates {
		if candidate.FilteredReason != "" {
			continue
		}
		result.ActualSkill = candidate.Skill.ID
		break
	}
	if result.ActualSkill != result.ExpectedSkill {
		result.Status = HealthFailed
		result.Error = fmt.Sprintf("expected top eligible skill %q, got %q", result.ExpectedSkill, result.ActualSkill)
		return result
	}
	for _, forbidden := range uniqueNonEmpty(fixture.MustNotSelect) {
		if result.ActualSkill == forbidden {
			result.Status = HealthFailed
			result.Error = fmt.Sprintf("must_not_select skill %q was selected", forbidden)
			return result
		}
	}
	return result
}

func candidateIDs(items []Recommendation) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item.Skill.ID == "" {
			continue
		}
		result = append(result, item.Skill.ID)
	}
	return result
}

func hasRoutingMetadata(item Spec) bool {
	if len(item.TriggerHints) > 0 || strings.TrimSpace(item.TaskPattern) != "" || len(item.Tags) > 0 || strings.TrimSpace(item.Summary) != "" {
		return true
	}
	return len(tokenizeSelectionText(normalizeSelectionText(item.Name))) > 1
}

func hasWeakRoutingMetadata(item Spec) bool {
	return len(item.TriggerHints) == 0 &&
		strings.TrimSpace(item.TaskPattern) == "" &&
		len(item.Tags) == 0 &&
		strings.TrimSpace(item.Summary) == "" &&
		strings.TrimSpace(item.Name) != ""
}

func duplicateTriggerKeys(item Spec) []string {
	result := make([]string, 0, len(item.TriggerHints)+1)
	for _, hint := range item.TriggerHints {
		if normalized := normalizeSelectionText(hint); normalized != "" {
			result = append(result, "trigger_hint:"+normalized)
		}
	}
	if pattern := normalizeSelectionText(item.TaskPattern); pattern != "" {
		result = append(result, "task_pattern:"+pattern)
	}
	slices.Sort(result)
	return result
}
