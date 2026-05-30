package plan

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
)

func parsePlanSteps(content string) ([]model.PlanStep, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, fmt.Errorf("empty plan response")
	}
	var envelope struct {
		Steps []model.PlanStep `json:"steps"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil && envelope.Steps != nil {
		return envelope.Steps, nil
	}
	var steps []model.PlanStep
	if err := json.Unmarshal([]byte(trimmed), &steps); err != nil {
		return nil, fmt.Errorf("parse plan JSON: %w", err)
	}
	return steps, nil
}

func validatePlanSteps(steps []model.PlanStep, enabledToolNames []string) error {
	if len(steps) == 0 {
		return fmt.Errorf("plan must contain at least one step")
	}
	ids := make(map[string]bool, len(steps))
	for i, step := range steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return fmt.Errorf("step %d id is required", i)
		}
		if ids[id] {
			return fmt.Errorf("duplicate step id %q", id)
		}
		ids[id] = true
		if strings.TrimSpace(step.Action) == "" {
			return fmt.Errorf("step %s action is required", id)
		}
		if step.Status != "" && step.Status != model.PlanStepPending {
			return fmt.Errorf("step %s initial status must be pending", id)
		}
		if err := validatePlanStepMetadata(step, enabledToolNames); err != nil {
			return err
		}
	}
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			depID := strings.TrimSpace(dep)
			if depID == "" {
				return fmt.Errorf("step %s has empty dependency", step.ID)
			}
			if !ids[depID] {
				return fmt.Errorf("step %s depends on unknown step %s", step.ID, depID)
			}
			if depID == strings.TrimSpace(step.ID) {
				return fmt.Errorf("step %s depends on itself", step.ID)
			}
		}
	}
	if err := detectPlanStepCycle(steps); err != nil {
		return err
	}
	return nil
}

func validatePlanStepMetadata(step model.PlanStep, enabledToolNames []string) error {
	stepID := strings.TrimSpace(step.ID)
	for i, target := range step.RepoTargets {
		path := strings.TrimSpace(target.Path)
		if path == "" {
			return fmt.Errorf("step %s repo_targets[%d].path is required", stepID, i)
		}
		if strings.HasPrefix(path, "/") || containsParentPathSegment(path) {
			return fmt.Errorf("step %s repo_targets[%d].path must be workspace-relative: %s", stepID, i, path)
		}
		confidence := strings.TrimSpace(target.Confidence)
		if confidence != "high" && confidence != "medium" && confidence != "low" {
			return fmt.Errorf("step %s repo_targets[%d].confidence must be high, medium, or low", stepID, i)
		}
		if confidence == "low" && strings.TrimSpace(target.Reason) == "" {
			return fmt.Errorf("step %s repo_targets[%d].reason is required for low confidence", stepID, i)
		}
	}
	switch step.Risk {
	case model.PlanStepRiskRead, model.PlanStepRiskWrite, model.PlanStepRiskExecute, model.PlanStepRiskDelegate:
	default:
		return fmt.Errorf("step %s risk must be read, write, execute, or delegate", stepID)
	}
	for i, intent := range step.VerificationIntent {
		kind := strings.TrimSpace(intent.Kind)
		if !validVerificationIntentKind(kind) {
			return fmt.Errorf("step %s verification_intent[%d].kind is invalid: %s", stepID, i, kind)
		}
	}
	if step.Risk == model.PlanStepRiskWrite || step.Risk == model.PlanStepRiskExecute || step.Risk == model.PlanStepRiskDelegate {
		if len(step.VerificationIntent) == 0 {
			return fmt.Errorf("step %s risk %s requires verification_intent", stepID, step.Risk)
		}
	}
	enabledTools := map[string]bool{}
	for _, name := range enabledToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		enabledTools[trimmed] = true
	}
	for _, hint := range step.ToolHints {
		name := strings.TrimSpace(hint)
		if name == "" {
			return fmt.Errorf("step %s tool_hints contains an empty tool name", stepID)
		}
		if !enabledTools[name] {
			return fmt.Errorf("step %s tool_hints contains unknown tool %q", stepID, name)
		}
	}
	return nil
}

func containsParentPathSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func validVerificationIntentKind(kind string) bool {
	switch kind {
	case "test", "build", "lint", "diff", "read", "manual", "subagent", "verifier", "checkpoint", "rollback":
		return true
	default:
		return false
	}
}

func detectPlanStepCycle(steps []model.PlanStep) error {
	deps := make(map[string][]string, len(steps))
	for _, step := range steps {
		id := strings.TrimSpace(step.ID)
		for _, dep := range step.DependsOn {
			deps[id] = append(deps[id], strings.TrimSpace(dep))
		}
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("plan dependencies contain a cycle at %s", id)
		}
		visiting[id] = true
		for _, dep := range deps[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for _, step := range steps {
		if err := visit(strings.TrimSpace(step.ID)); err != nil {
			return err
		}
	}
	return nil
}

func normalizePlanSteps(steps []model.PlanStep) []model.PlanStep {
	out := make([]model.PlanStep, 0, len(steps))
	for _, step := range steps {
		normalized := clonePlanStep(step)
		normalized.ID = strings.TrimSpace(normalized.ID)
		normalized.Action = strings.TrimSpace(normalized.Action)
		if normalized.Status == "" {
			normalized.Status = model.PlanStepPending
		}
		if normalized.Risk == "" {
			normalized.Risk = model.PlanStepRiskRead
		}
		deps := make([]string, 0, len(normalized.DependsOn))
		for _, dep := range normalized.DependsOn {
			deps = append(deps, strings.TrimSpace(dep))
		}
		normalized.DependsOn = deps
		normalized.RepoTargets = normalizePlanRepoTargets(normalized.RepoTargets)
		normalized.VerificationIntent = normalizeVerificationIntents(normalized.VerificationIntent)
		normalized.ToolHints = normalizeStringList(normalized.ToolHints)
		out = append(out, normalized)
	}
	return out
}

func clonePlanStep(step model.PlanStep) model.PlanStep {
	return model.PlanStep{
		ID:                 step.ID,
		Action:             step.Action,
		Status:             step.Status,
		DependsOn:          append([]string(nil), step.DependsOn...),
		RepoTargets:        append([]model.PlanRepoTarget(nil), step.RepoTargets...),
		VerificationIntent: cloneVerificationIntents(step.VerificationIntent),
		Risk:               step.Risk,
		ToolHints:          append([]string(nil), step.ToolHints...),
		Evidence:           append([]model.PlanEvidence(nil), step.Evidence...),
	}
}

func cloneVerificationIntents(items []model.VerificationIntent) []model.VerificationIntent {
	out := make([]model.VerificationIntent, 0, len(items))
	for _, item := range items {
		out = append(out, model.VerificationIntent{
			Kind:    item.Kind,
			Command: append([]string(nil), item.Command...),
			Paths:   append([]string(nil), item.Paths...),
			Reason:  item.Reason,
		})
	}
	return out
}

func normalizePlanRepoTargets(items []model.PlanRepoTarget) []model.PlanRepoTarget {
	out := make([]model.PlanRepoTarget, 0, len(items))
	for _, item := range items {
		normalized := item
		normalized.Path = strings.TrimSpace(normalized.Path)
		normalized.Symbol = strings.TrimSpace(normalized.Symbol)
		normalized.Reason = strings.TrimSpace(normalized.Reason)
		normalized.Confidence = strings.TrimSpace(normalized.Confidence)
		out = append(out, normalized)
	}
	return out
}

func normalizeVerificationIntents(items []model.VerificationIntent) []model.VerificationIntent {
	out := make([]model.VerificationIntent, 0, len(items))
	for _, item := range items {
		normalized := item
		normalized.Kind = strings.TrimSpace(normalized.Kind)
		normalized.Reason = strings.TrimSpace(normalized.Reason)
		normalized.Command = normalizeStringList(normalized.Command)
		normalized.Paths = normalizeStringList(normalized.Paths)
		out = append(out, normalized)
	}
	return out
}

func normalizeStringList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, strings.TrimSpace(item))
	}
	return out
}
