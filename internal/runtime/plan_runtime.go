package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	storecore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
)

var (
	ErrRiskyToolRequiresPlan   = errors.New("risky tool execution requires an active persisted plan")
	ErrPlanStepVerificationGap = errors.New("plan step requires recorded verification before completion")
)

func enforceRiskyToolPlan(ctx context.Context, store PlanStore, spec tooling.ToolSpec) (string, string, error) {
	if spec.PlanPolicy != tooling.PlanPolicyRequireActivePlan {
		return "", "", nil
	}
	if store == nil {
		return "", "", errors.New("plan enforcement store is not available")
	}
	sessionID := strings.TrimSpace(sessionIDFromContext(ctx))
	if sessionID == "" {
		return "", "", fmt.Errorf("%w: session_id not available for %s", ErrRiskyToolRequiresPlan, spec.Name)
	}
	plan, err := store.LoadPlan(ctx, sessionID)
	if err != nil {
		if errors.Is(err, storecore.ErrPlanNotFound) {
			return "", "", fmt.Errorf("%w: active plan not available before %s", ErrRiskyToolRequiresPlan, spec.Name)
		}
		return "", "", fmt.Errorf("load active plan for %s: %w", spec.Name, err)
	}
	stepIndex, err := findSingleInProgressPlanStep(plan)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrRiskyToolRequiresPlan, err)
	}
	return sessionID, plan.Steps[stepIndex].ID, nil
}
