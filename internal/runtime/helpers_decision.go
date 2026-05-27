package runtime

import (
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/skills"
)

func buildDecisionInput(
	req RunnerBuildRequest,
	matches []SkillMatch,
	hasWorkingContext bool,
) decision.DecideInput {
	return decision.DecideInput{
		RunID:             req.RunID,
		SessionID:         req.SessionID,
		Input:             req.Input,
		ExplicitSkillID:   req.SkillID,
		HasWorkingContext: hasWorkingContext,
		AvailableSkills:   recommendedSkillsFromMatches(matches),
	}
}

func buildDecisionEngine(profile *decision.ProfileService) (*decision.Engine, *decision.ParsedProfile, error) {
	if profile == nil {
		return nil, nil, fmt.Errorf("decision profile service is nil")
	}
	parsed, err := profile.Load()
	if err != nil {
		return nil, nil, err
	}
	engine := decision.NewEngine(parsed.Profile)
	return engine, parsed, nil
}

func fillRecordMetadata(record *decision.Record, profileHash string) {
	if record == nil {
		return
	}
	record.DecisionProfileHash = profileHash
	record.CreatedAt = time.Now().UTC()
}

func selectedSkillFromDecisionRecord(record *decision.Record, matches []SkillMatch, stableSkills []skills.Spec) (*SelectedSkill, error) {
	if record == nil {
		return nil, nil
	}
	if record.Action != decision.ActionExecuteWithSkill {
		return nil, nil
	}
	skillID := strings.TrimSpace(record.SelectedSkillID)
	if skillID == "" {
		return nil, fmt.Errorf("decision action execute_with_skill requires selected skill id")
	}
	score, matchedTerms := selectedSkillMatchMetadata(skillID, matches)
	for _, item := range stableSkills {
		if item.ID != skillID {
			continue
		}
		return &SelectedSkill{
			Skill:        skills.CopySpec(item),
			Score:        score,
			MatchedTerms: matchedTerms,
			Explicit:     record.DecisionReason == "explicit_skill",
		}, nil
	}
	return nil, fmt.Errorf("decision selected skill %q not found", skillID)
}

func selectedSkillMatchMetadata(skillID string, matches []SkillMatch) (int, []string) {
	for _, match := range matches {
		if match.Skill.ID == skillID {
			return match.Score, append([]string(nil), match.MatchedTerms...)
		}
	}
	return 0, nil
}
