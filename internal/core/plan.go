package core

// EventPlanUpdated is recorded on a run each time its plan tool call
// replaces the plan, and once more when the run succeeds and closes the
// steps its reply ended; the latest one is the plan as it stands.
const EventPlanUpdated = "plan.updated"

// PlanToolName is the tool a run writes its plan with. Its calls are the
// plan itself and stay out of the run's steps.
const PlanToolName = "plan"

// Where a plan step stands.
const (
	PlanStepPending = "pending"
	PlanStepRunning = "running"
	PlanStepDone    = "done"
	PlanStepFailed  = "failed"
)

// PlanStepStatuses lists every status a plan step can have.
var PlanStepStatuses = []string{PlanStepPending, PlanStepRunning, PlanStepDone, PlanStepFailed}

// PlanStep is one step of the plan a run works through: what it does, where
// it stands and an optional short note such as a count. Reply marks the
// last step when the run's reply is what carries it out; clients do not see
// it.
type PlanStep struct {
	Label  string `json:"label"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
	Reply  bool   `json:"reply,omitempty"`
}

// PlanUpdate is the payload of EventPlanUpdated: the whole plan, in order.
type PlanUpdate struct {
	Steps []PlanStep `json:"steps"`
}
