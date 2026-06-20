package runtime

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
)

func delegationPlanFailureReasons(planRecord *model.Plan) []string {
	if planRecord == nil {
		return nil
	}
	reasons := make([]string, 0)
	for _, step := range planRecord.Steps {
		if reason := delegationStepFailureReason(step); reason != "" {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

func delegationStepFailureReason(step model.PlanStep) string {
	if strings.TrimSpace(string(step.Status)) != string(model.PlanStepFailed) {
		return ""
	}
	reason := strings.TrimSpace(latestStoreEvidenceError(step.Evidence))
	if reason != "" {
		return reason
	}
	action := strings.TrimSpace(step.Action)
	if action == "" {
		action = strings.TrimSpace(step.ID)
	}
	return fmt.Sprintf("child plan step %s failed", action)
}

func latestStoreEvidenceError(items []model.PlanEvidence) string {
	for i := len(items) - 1; i >= 0; i-- {
		if strings.TrimSpace(string(items[i].Status)) != string(model.EvidenceStatusFailed) {
			continue
		}
		if errText := strings.TrimSpace(items[i].Error); errText != "" {
			return errText
		}
		if summary := strings.TrimSpace(items[i].Summary); summary != "" {
			return summary
		}
	}
	return ""
}

func delegationEvidenceSummaries(planRecord *model.Plan) []string {
	if planRecord == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(planRecord.Steps))
	for _, step := range planRecord.Steps {
		for _, evidence := range step.Evidence {
			summary := strings.TrimSpace(evidence.Summary)
			if summary == "" {
				continue
			}
			if _, ok := seen[summary]; ok {
				continue
			}
			seen[summary] = struct{}{}
			result = append(result, summary)
		}
	}
	return result
}

func delegationEvidenceRefs(planRecord *model.Plan) []string {
	if planRecord == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, step := range planRecord.Steps {
		for _, evidence := range step.Evidence {
			result = appendUniqueRefs(result, seen, childEvidenceRefs(evidence))
		}
	}
	return result
}

func appendUniqueRefs(result []string, seen map[string]struct{}, refs []string) []string {
	for _, ref := range refs {
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func childEvidenceRefs(evidence model.PlanEvidence) []string {
	refs := make([]string, 0, 3)
	if ref := strings.TrimSpace(evidence.ToolResultRef); ref != "" {
		refs = append(refs, ref)
	}
	if ref := strings.TrimSpace(evidence.ChildRunID); ref != "" {
		refs = append(refs, "run:"+ref)
	}
	if ref := strings.TrimSpace(evidence.ID); ref != "" {
		refs = append(refs, "evidence:"+ref)
	}
	return refs
}
