package runtime

import (
	"context"
	"fmt"
	"slices"

	"github.com/ycvk/acorn/internal/core"
)

// closePlan marks the last step of the run's plan done when the run
// succeeds with that step running and every step before it ended. The reply
// carries out that step, so only the run can record its end after the reply
// is written.
func (e *Executor) closePlan(ctx context.Context, runID string) error {
	events, err := e.store.LoadStepEvents(ctx, []string{runID})
	if err != nil {
		return err
	}
	closed, ok, err := closedPlan(events[runID])
	if err != nil || !ok {
		return err
	}
	if _, err := e.store.AppendEvent(ctx, runID, core.EventPlanUpdated, closed); err != nil {
		return fmt.Errorf("append %s event: %w", core.EventPlanUpdated, err)
	}
	return nil
}

// closedPlan returns the run's last plan with its last step done, or false
// when the run wrote no plan, its last step is not running, or a step
// before it has not ended.
func closedPlan(events []core.EventRecord) (core.PlanUpdate, bool, error) {
	for _, event := range slices.Backward(events) {
		if event.Kind != core.EventPlanUpdated {
			continue
		}
		var plan core.PlanUpdate
		if err := reencodeViaJSON(event.Payload, &plan); err != nil {
			return core.PlanUpdate{}, false, fmt.Errorf("%s event %d of %s: %w", core.EventPlanUpdated, event.Sequence, event.RunID, err)
		}
		last := len(plan.Steps) - 1
		if last < 0 || plan.Steps[last].Status != core.PlanStepRunning {
			return core.PlanUpdate{}, false, nil
		}
		for _, step := range plan.Steps[:last] {
			if step.Status != core.PlanStepDone && step.Status != core.PlanStepFailed {
				return core.PlanUpdate{}, false, nil
			}
		}
		steps := slices.Clone(plan.Steps)
		steps[last].Status = core.PlanStepDone
		return core.PlanUpdate{Steps: steps}, true, nil
	}
	return core.PlanUpdate{}, false, nil
}
