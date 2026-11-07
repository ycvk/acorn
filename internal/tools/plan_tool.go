package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// A plan has minPlanSteps to maxPlanSteps steps; a step's label and note
// fit one row of the app.
const (
	minPlanSteps      = 2
	maxPlanSteps      = 12
	maxPlanLabelRunes = 60
	maxPlanNoteRunes  = 20
)

// PlanStore records each plan on its run.
type PlanStore interface {
	AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error)
}

// PlanToolDeps configure plan.
type PlanToolDeps struct {
	Store   PlanStore
	Context core.ToolCallContextBridge
}

type PlanInput struct {
	Steps []PlanStepInput `json:"steps" jsonschema:"required" jsonschema_description:"The whole plan in order, 2 to 12 steps; it replaces the plan written before."`
}

type PlanStepInput struct {
	Label  string `json:"label" jsonschema:"required" jsonschema_description:"What the step does, in the owner's language, at most 60 characters."`
	Status string `json:"status" jsonschema:"required,enum=pending,enum=running,enum=done,enum=failed" jsonschema_description:"pending has not started; running is under way, and steps whose calls run at once are all running; done is finished; failed could not be done."`
	Note   string `json:"note,omitempty" jsonschema_description:"Optional note shown at the end of the row, at most 20 characters, such as 3/5 or 2 files."`
}

type PlanOutput struct {
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Note  string `json:"note"`
}

func buildPlanTool(deps PlanToolDeps) (einotool.BaseTool, error) {
	return inferLocalTool(core.PlanToolName,
		"Show the owner your plan for this request as a checklist above your reply, and keep it current. Send the whole list each time: with the first calls, marking running the steps they carry out, then each time a step ends, in the same response as the calls that start the next ones. When only the reply is left, leave the last step running; the run marks it done once it ends with your reply.",
		func(ctx context.Context, input PlanInput) (PlanOutput, error) {
			runID := strings.TrimSpace(deps.Context.CurrentRunID(ctx))
			if runID == "" {
				return PlanOutput{}, errors.New("plan needs the current run")
			}
			update, err := checkPlan(input)
			if err != nil {
				return PlanOutput{}, err
			}
			if _, err := deps.Store.AppendEvent(ctx, runID, core.EventPlanUpdated, update); err != nil {
				return PlanOutput{}, fmt.Errorf("append %s event: %w", core.EventPlanUpdated, err)
			}
			done := 0
			for _, step := range update.Steps {
				if step.Status == core.PlanStepDone {
					done++
				}
			}
			return PlanOutput{
				Done:  done,
				Total: len(update.Steps),
				Note:  "The owner sees the plan above your reply; the reply need not repeat it.",
			}, nil
		})
}

// checkPlan puts each label and note on one line and rejects a plan the app
// cannot show: too few or too many steps, an empty or long label, a long
// note or an unknown status.
func checkPlan(input PlanInput) (core.PlanUpdate, error) {
	if n := len(input.Steps); n < minPlanSteps || n > maxPlanSteps {
		return core.PlanUpdate{}, fmt.Errorf("steps: a plan has %d to %d steps, got %d; a request done in one step needs no plan", minPlanSteps, maxPlanSteps, n)
	}
	steps := make([]core.PlanStep, 0, len(input.Steps))
	for i, in := range input.Steps {
		step := core.PlanStep{Label: oneLine(in.Label), Status: strings.TrimSpace(in.Status), Note: oneLine(in.Note)}
		switch {
		case step.Label == "":
			return core.PlanUpdate{}, fmt.Errorf("steps[%d].label is empty", i)
		case utf8.RuneCountInString(step.Label) > maxPlanLabelRunes:
			return core.PlanUpdate{}, fmt.Errorf("steps[%d].label has %d characters, at most %d", i, utf8.RuneCountInString(step.Label), maxPlanLabelRunes)
		case utf8.RuneCountInString(step.Note) > maxPlanNoteRunes:
			return core.PlanUpdate{}, fmt.Errorf("steps[%d].note has %d characters, at most %d", i, utf8.RuneCountInString(step.Note), maxPlanNoteRunes)
		case !slices.Contains(core.PlanStepStatuses, step.Status):
			return core.PlanUpdate{}, fmt.Errorf("steps[%d].status must be pending, running, done or failed, got %q", i, in.Status)
		}
		steps = append(steps, step)
	}
	return core.PlanUpdate{Steps: steps}, nil
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
