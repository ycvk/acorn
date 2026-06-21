package plan

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
)

func formatExecuteChildTask(plan *model.Plan, step model.PlanStep) string {
	var b strings.Builder
	b.WriteString("Execute exactly one parent plan step. Finish only this step.\n")
	b.WriteString("Your final output must be the user-facing result for this step, not an execution report. Do not add headings such as \"Completion Summary\" unless the user explicitly asked for a report.\n\n")
	fmt.Fprintf(&b, "Step %s: %s\n", step.ID, strings.TrimSpace(step.Action))
	writeChildRepoTargets(&b, step.RepoTargets)
	writeChildVerificationIntent(&b, step.VerificationIntent)
	upstream := completedStepContext(plan, step.ID)
	if upstream != "" {
		b.WriteString("\nUpstream completed context:\n")
		b.WriteString(upstream)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func writeChildRepoTargets(b *strings.Builder, targets []model.PlanRepoTarget) {
	if len(targets) == 0 {
		return
	}
	b.WriteString("\nRepo targets:\n")
	for _, target := range targets {
		b.WriteString("- ")
		b.WriteString(formatRepoTargetLine(target))
		b.WriteString("\n")
	}
}

func formatRepoTargetLine(target model.PlanRepoTarget) string {
	line := strings.TrimSpace(target.Path)
	if strings.TrimSpace(target.Symbol) != "" {
		line = fmt.Sprintf("%s#%s", line, strings.TrimSpace(target.Symbol))
	}
	if strings.TrimSpace(target.Reason) != "" {
		line = fmt.Sprintf("%s (%s)", line, strings.TrimSpace(target.Reason))
	}
	return line
}

func writeChildVerificationIntent(b *strings.Builder, intents []model.VerificationIntent) {
	if len(intents) == 0 {
		return
	}
	b.WriteString("\nVerification requirements:\n")
	for _, intent := range intents {
		line := strings.TrimSpace(intent.Kind)
		if len(intent.Command) > 0 {
			line = fmt.Sprintf("%s via %s", line, strings.Join(intent.Command, " "))
		}
		if strings.TrimSpace(intent.Reason) != "" {
			line = fmt.Sprintf("%s (%s)", line, strings.TrimSpace(intent.Reason))
		}
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func completedStepContext(plan *model.Plan, currentStepID string) string {
	if plan == nil {
		return ""
	}
	lines := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		if step.ID == currentStepID || step.Status != model.PlanStepCompleted {
			continue
		}
		summary := latestEvidenceSummary(step.Evidence)
		line := step.Action
		if summary != "" {
			line = fmt.Sprintf("%s: %s", step.Action, summary)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func stepRequiresVerifier(step model.PlanStep) bool {
	for _, intent := range step.VerificationIntent {
		if strings.TrimSpace(intent.Kind) == "verifier" {
			return true
		}
	}
	return false
}

func verifierAcceptanceCriteria(step model.PlanStep) []string {
	criteria := make([]string, 0, len(step.VerificationIntent)+1)
	action := strings.TrimSpace(step.Action)
	if action != "" {
		criteria = append(criteria, fmt.Sprintf("completed plan step %s: %s", strings.TrimSpace(step.ID), action))
	}
	for _, intent := range step.VerificationIntent {
		if strings.TrimSpace(intent.Kind) != "verifier" {
			continue
		}
		if reason := strings.TrimSpace(intent.Reason); reason != "" {
			criteria = append(criteria, reason)
		}
	}
	return trimmedNonEmptyStrings(criteria)
}

func verifierEvidenceRefs(items []model.PlanEvidence) []string {
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if ref := strings.TrimSpace(item.ID); ref != "" {
			refs = append(refs, ref)
		}
	}
	return trimmedNonEmptyStrings(refs)
}

func verifierToolResultRefs(items []model.PlanEvidence) []string {
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if ref := strings.TrimSpace(item.ToolResultRef); ref != "" {
			refs = append(refs, ref)
		}
	}
	return trimmedNonEmptyStrings(refs)
}

func verifierReadOnlyToolNames() []string {
	return []string{"read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff"}
}

func failedPlanExecutionEvidenceReason(items []model.PlanEvidence) (string, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		if reason, ok := failedPlanExecutionItemReason(items[i]); ok {
			return reason, true
		}
	}
	return "", false
}

func failedPlanExecutionItemReason(item model.PlanEvidence) (string, bool) {
	if !planEvidenceItemFailed(item) {
		return "", false
	}
	if strings.TrimSpace(item.Error) != "" {
		return strings.TrimSpace(item.Error), true
	}
	return strings.TrimSpace(item.Summary), true
}

func planEvidenceItemFailed(item model.PlanEvidence) bool {
	switch item.Kind {
	case model.EvidenceKindSubagent:
		return item.Status == model.EvidenceStatusFailed
	case model.EvidenceKindVerifier:
		return item.Status == model.EvidenceStatusFailed ||
			(item.Status == model.EvidenceStatusRecorded && strings.TrimSpace(item.Error) != "")
	default:
		return false
	}
}
