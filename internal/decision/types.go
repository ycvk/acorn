package decision

import "time"

type Action string

const (
	ActionExecuteWithSkill    Action = "execute_with_skill"
	ActionInspectFirst        Action = "inspect_first"
	ActionExecuteWithoutSkill Action = "execute_without_skill"
	ActionResumeRun           Action = "resume_run"
	ActionAskUser             Action = "ask_user"
	ActionBlock               Action = "block"
)

type Defaults struct {
	MissingContext            Action `yaml:"missing_context" json:"missing_context"`
	MissingRequiredCapability Action `yaml:"missing_required_capability" json:"missing_required_capability"`
}

type Route struct {
	Intent  string `yaml:"intent" json:"intent"`
	Action  Action `yaml:"action" json:"action"`
	SkillID string `yaml:"skill_id,omitempty" json:"skill_id,omitempty"`
}

type Profile struct {
	Defaults Defaults `json:"defaults"`
	Routes   []Route  `json:"routes,omitempty"`
}

type Record struct {
	RunID               string    `json:"run_id"`
	SessionID           string    `json:"session_id,omitempty"`
	Action              Action    `json:"action"`
	Intent              string    `json:"intent"`
	SelectedSkillID     string    `json:"selected_skill_id,omitempty"`
	DecisionReason      string    `json:"decision_reason"`
	DecisionProfileHash string    `json:"decision_profile_hash"`
	CreatedAt           time.Time `json:"created_at"`
}

type DecideInput struct {
	RunID             string
	SessionID         string
	Input             string
	ExplicitSkillID   string
	HasWorkingContext bool
	AvailableSkills   []RecommendedSkill
}

type RecommendedSkill struct {
	ID             string
	Name           string
	Score          int
	TriggerMatched bool
	FilteredReason string
}
