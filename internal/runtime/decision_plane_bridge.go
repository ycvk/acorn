package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/skills"
)

type planeBridge struct {
	engine  *decision.Engine
	profile *decision.ProfileService
	store   runDecisionStore
	saveFn  func(context.Context, decision.Record) error
	emitFn  func(context.Context, *decision.Record, string) error
}

type runDecisionStore interface {
	SaveRunDecision(context.Context, decision.Record) error
	LoadRunDecision(context.Context, string) (*decision.Record, error)
}

func newPlaneBridge(
	engine *decision.Engine,
	profile *decision.ProfileService,
	store runDecisionStore,
	saveFn func(context.Context, decision.Record) error,
	emitFn func(context.Context, *decision.Record, string) error,
) *planeBridge {
	return &planeBridge{
		engine:  engine,
		profile: profile,
		store:   store,
		saveFn:  saveFn,
		emitFn:  emitFn,
	}
}

func (b *planeBridge) Decide(ctx context.Context, req decision.Request) (*decision.Result, error) {
	if b.engine == nil {
		return nil, fmt.Errorf("decision engine is nil")
	}
	record, err := b.engine.Decide(ctx, decision.DecideInput(req))
	if err != nil {
		return nil, err
	}
	selected := selectedSkillResultFromRecord(record)
	hint := decision.BuildHint(record.Action)
	result := &decision.Result{
		Record:        record,
		SelectedSkill: selected,
		Hint:          hint,
	}
	return result, nil
}

func enrichSelectedSkillFromMatches(result *decision.Result, matches []SkillMatch, stableSkills []skills.Spec) {
	if result == nil || result.Record == nil || result.SelectedSkill == nil {
		return
	}
	skillID := result.SelectedSkill.SkillID
	for _, m := range matches {
		if m.Skill.ID == skillID {
			result.SelectedSkill.SkillName = m.Skill.Name
			result.SelectedSkill.Score = m.Score
			result.SelectedSkill.MatchedTerms = append([]string(nil), m.MatchedTerms...)
			return
		}
	}
	for _, s := range stableSkills {
		if s.ID == skillID {
			result.SelectedSkill.SkillName = s.Name
			return
		}
	}
}

func (b *planeBridge) HandleAction(ctx context.Context, req decision.ActionRequest) error {
	if req.Result == nil || req.Result.Record == nil {
		return nil
	}
	action := req.Result.Record.Action
	if decision.IsContinuableAction(action) {
		return nil
	}
	switch action {
	case decision.ActionAskUser:
		return fmt.Errorf("decision requires operator confirmation: %s", req.Result.Record.DecisionReason)
	case decision.ActionBlock:
		return fmt.Errorf("decision blocked execution: %s", req.Result.Record.DecisionReason)
	case decision.ActionResumeRun:
		return fmt.Errorf("decision resolved to resume_run for a new execution")
	default:
		return nil
	}
}

func selectedSkillResultFromRecord(record *decision.Record) *decision.SelectedSkillResult {
	if record == nil || record.Action != decision.ActionExecuteWithSkill {
		return nil
	}
	skillID := strings.TrimSpace(record.SelectedSkillID)
	if skillID == "" {
		return nil
	}
	return &decision.SelectedSkillResult{
		SkillID:  skillID,
		Explicit: record.DecisionReason == "explicit_skill",
	}
}

func buildPlaneRequest(
	req RunnerBuildRequest,
	caps *runCapabilities,
	matches []SkillMatch,
	hasWorkingContext bool,
) decision.Request {
	return decision.Request{
		RunID:             req.RunID,
		SessionID:         req.SessionID,
		Input:             req.Input,
		ExplicitSkillID:   req.SkillID,
		HasWorkingContext: hasWorkingContext,
		AvailableSkills:   recommendedSkillsFromMatches(matches),
	}
}

func buildPlaneEngine(profile *decision.ProfileService) (*decision.Engine, *decision.ParsedProfile, error) {
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

func selectedSkillFromPlaneResult(result *decision.Result, stableSkills []skills.Spec) (*SelectedSkill, error) {
	if result == nil || result.Record == nil {
		return nil, nil
	}
	if result.Record.Action != decision.ActionExecuteWithSkill {
		return nil, nil
	}
	if result.SelectedSkill == nil {
		return nil, fmt.Errorf("decision action execute_with_skill requires selected skill result")
	}
	skillID := strings.TrimSpace(result.SelectedSkill.SkillID)
	if skillID == "" {
		return nil, fmt.Errorf("decision action execute_with_skill requires selected skill id")
	}
	for _, item := range stableSkills {
		if item.ID != skillID {
			continue
		}
		return &SelectedSkill{
			Skill:        skills.CopySpec(item),
			Score:        result.SelectedSkill.Score,
			MatchedTerms: append([]string(nil), result.SelectedSkill.MatchedTerms...),
			Explicit:     result.SelectedSkill.Explicit,
		}, nil
	}
	return nil, fmt.Errorf("decision selected skill %q not found", skillID)
}

func skillIDs(items []skills.Spec) []string {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
