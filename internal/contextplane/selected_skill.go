package contextplane

import "github.com/ycvk/acorn/internal/skills"

type SelectedSkill struct {
	Skill        skills.Spec
	Score        int
	MatchedTerms []string
	Explicit     bool
}

func CopySelectedSkill(selected *SelectedSkill) *SelectedSkill {
	if selected == nil {
		return nil
	}
	return &SelectedSkill{
		Skill:        skills.CopySpec(selected.Skill),
		Score:        selected.Score,
		MatchedTerms: append([]string(nil), selected.MatchedTerms...),
		Explicit:     selected.Explicit,
	}
}
