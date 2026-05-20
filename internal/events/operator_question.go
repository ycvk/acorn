package events

const (
	OperatorQuestionDecisionAnswer  = "answer"
	OperatorQuestionDecisionDecline = "decline"
)

type PendingActionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type OperatorQuestionPayload struct {
	Question      string                `json:"question"`
	Options       []PendingActionOption `json:"options,omitempty"`
	AllowFreeform bool                  `json:"allow_freeform,omitempty"`
}

type OperatorQuestionDecision struct {
	Action           string `json:"action"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}
