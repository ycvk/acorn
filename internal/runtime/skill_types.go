package runtime

import (
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/skills"
)

type SelectedSkill = contextplane.SelectedSkill

type SkillMatch struct {
	Skill          skills.Spec
	Score          int
	MatchedTerms   []string
	TriggerMatched bool
	FilteredReason string
}

func CopySelectedSkill(selected *SelectedSkill) *SelectedSkill {
	return contextplane.CopySelectedSkill(selected)
}
