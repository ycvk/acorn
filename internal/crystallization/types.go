package crystallization

import (
	"time"

	"github.com/cloudwego/eino/adk"
)

type CrystallizationVerdict string

const (
	VerdictCrystallized      CrystallizationVerdict = "crystallized"
	VerdictInsufficientValue CrystallizationVerdict = "insufficient_value"
	VerdictTooSimilar        CrystallizationVerdict = "too_similar"
)

type CrystallizationRequest struct {
	RunID        string
	SessionID    string
	Input        string
	Output       string
	ToolNames    []string
	TouchedPaths []string
	EvidenceRefs []string
	Messages     []adk.Message
}

type CrystallizationResult struct {
	Verdict   CrystallizationVerdict
	SkillID   string
	Reason    string
	SimilarTo string
}

type IndexEntry struct {
	SkillID      string    `json:"skill_id"`
	SkillName    string    `json:"skill_name"`
	Summary      string    `json:"summary"`
	Keywords     []string  `json:"keywords"`
	TaskPattern  string    `json:"task_pattern"`
	QualityScore int       `json:"quality_score"`
	Source       string    `json:"source"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SkillCandidate struct {
	Title       string
	Body        string
	TaskPattern string
	Reason      string
}
