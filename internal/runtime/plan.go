package runtime

import (
	"context"
	"fmt"
	"slices"

	"github.com/ycvk/acorn/internal/core"
)

// closePlan records the plan as the run's reply leaves it. Only the run can
// record a step's end after the reply is written, so the agent spends no
// model call on it.
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

// closedPlan returns the run's last plan as a reply that ends the run leaves
// it: the steps still running are done, and so is a pending step the reply
// carries out. It returns false when the run wrote no plan or the reply ends
// none of its steps. A step that never began stays pending.
func closedPlan(events []core.EventRecord) (core.PlanUpdate, bool, error) {
	for _, event := range slices.Backward(events) {
		if event.Kind != core.EventPlanUpdated {
			continue
		}
		var plan core.PlanUpdate
		if err := reencodeViaJSON(event.Payload, &plan); err != nil {
			return core.PlanUpdate{}, false, fmt.Errorf("%s event %d of %s: %w", core.EventPlanUpdated, event.Sequence, event.RunID, err)
		}
		steps := slices.Clone(plan.Steps)
		closed := false
		for i, step := range steps {
			if step.Status == core.PlanStepRunning || step.Reply && step.Status == core.PlanStepPending {
				steps[i].Status = core.PlanStepDone
				closed = true
			}
		}
		return core.PlanUpdate{Steps: steps}, closed, nil
	}
	return core.PlanUpdate{}, false, nil
}
