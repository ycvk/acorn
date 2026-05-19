package skilllifecycle

import (
	"strings"

	"github.com/ycvk/acorn/internal/skills"
)

type Spec = skills.Spec
type LifecycleStatus = skills.LifecycleStatus
type LifecycleEvent = skills.LifecycleEvent
type AssessmentVerdict = skills.AssessmentVerdict
type SkillAssessment = skills.SkillAssessment
type Requirements = skills.Requirements

const (
	BuiltinScope        = skills.BuiltinScope
	GeneratedScope      = skills.GeneratedScope
	OriginHuman         = skills.OriginHuman
	LifecycleDraft      = skills.LifecycleDraft
	LifecycleVerified   = skills.LifecycleVerified
	LifecycleNeedsEval  = skills.LifecycleNeedsEval
	LifecycleRetired    = skills.LifecycleRetired
	AssessmentVerified  = skills.AssessmentVerified
	AssessmentNeedsEval = skills.AssessmentNeedsEval
	AssessmentRetired   = skills.AssessmentRetired
)

func uniqueNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
