package orchestrationmode

type Mode string

const (
	DirectResponse Mode = "direct_response"
	SingleAgent    Mode = "single_agent"
	PlanExecute    Mode = "plan_execute"
)

func Normalize(mode Mode) Mode {
	switch mode {
	case DirectResponse, SingleAgent, PlanExecute:
		return mode
	default:
		return mode
	}
}
