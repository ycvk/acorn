package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/skills"
)

type runSelection struct {
	decisionRecord *decision.Record
	selectedSkill  *SelectedSkill
}

func (f *RunnerFactory) resolveRunSelection(
	ctx context.Context,
	req RunnerBuildRequest,
	caps *runCapabilities,
) (*runSelection, error) {
	if caps == nil {
		return nil, fmt.Errorf("run capabilities are required")
	}
	if isEmptyRunSelectionInput(req) {
		return &runSelection{}, nil
	}
	if strings.TrimSpace(req.Input) != "" || strings.TrimSpace(req.SkillID) != "" {
		return f.resolveRunSelectionByDecision(ctx, req, caps)
	}
	return f.resolveRunSelectionByResume(ctx, req, caps)
}

func isEmptyRunSelectionInput(req RunnerBuildRequest) bool {
	return strings.TrimSpace(req.Input) == "" && strings.TrimSpace(req.SkillID) == "" && strings.TrimSpace(req.RunID) == ""
}

func (f *RunnerFactory) resolveRunSelectionByDecision(ctx context.Context, req RunnerBuildRequest, caps *runCapabilities) (*runSelection, error) {
	engine, parsed, err := buildDecisionEngine(f.deps.DecisionProfiles)
	if err != nil {
		return nil, err
	}
	hasWorkingContext, err := f.hasWorkingContext(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	discovered, err := f.retrieveSkillCandidates(req, caps)
	if err != nil {
		return nil, err
	}
	input := buildDecisionInput(req, discovered, hasWorkingContext)
	record, err := engine.Decide(ctx, input)
	if err != nil {
		return nil, err
	}
	fillRecordMetadata(record, parsed.Hash)
	if err := f.persistDecisionAndEmit(ctx, req, record); err != nil {
		return nil, err
	}
	return f.finalizeRunSelection(ctx, req, record, discovered, caps)
}

func (f *RunnerFactory) retrieveSkillCandidates(req RunnerBuildRequest, caps *runCapabilities) ([]SkillMatch, error) {
	retrieved, err := skills.RetrieveCandidates(skills.CandidateQuery{
		Input:           req.Input,
		ExplicitSkillID: req.SkillID,
		Eligibility:     skillEligibilityContextFromCatalog(caps.catalog),
	}, caps.stableSkills)
	if err != nil {
		return nil, err
	}
	if retrieved == nil {
		return nil, nil
	}
	return runtimeMatchesFromRecommendations(retrieved.Candidates), nil
}

func (f *RunnerFactory) persistDecisionAndEmit(ctx context.Context, req RunnerBuildRequest, record *decision.Record) error {
	if err := f.deps.Store.SaveRunDecision(ctx, *record); err != nil {
		return err
	}
	return emitDecisionEvents(ctx, f.deps.Store, req, record, req.SkillID)
}

func (f *RunnerFactory) finalizeRunSelection(ctx context.Context, req RunnerBuildRequest, record *decision.Record, discovered []SkillMatch, caps *runCapabilities) (*runSelection, error) {
	selectedSkill, err := selectedSkillFromDecisionRecord(record, discovered, caps.stableSkills)
	if err != nil {
		return nil, err
	}
	if emitErr := emitSkillSelectionEvents(ctx, f.deps.Store, req, selectedSkill, discovered); emitErr != nil {
		return nil, emitErr
	}
	if err := enforceContinuableAction(record); err != nil {
		return nil, err
	}
	return &runSelection{decisionRecord: record, selectedSkill: selectedSkill}, nil
}

func enforceContinuableAction(record *decision.Record) error {
	if decision.IsContinuableAction(record.Action) {
		return nil
	}
	switch record.Action {
	case decision.ActionAskUser:
		return fmt.Errorf("decision requires operator confirmation: %s", record.DecisionReason)
	case decision.ActionBlock:
		return fmt.Errorf("decision blocked execution: %s", record.DecisionReason)
	default:
		return fmt.Errorf("decision action %q is not continuable", record.Action)
	}
}

func (f *RunnerFactory) resolveRunSelectionByResume(ctx context.Context, req RunnerBuildRequest, caps *runCapabilities) (*runSelection, error) {
	decisionRecord, err := f.deps.Store.LoadRunDecision(ctx, req.RunID)
	if err != nil {
		return nil, err
	}
	if decisionRecord == nil {
		return nil, fmt.Errorf("run decision missing for %s", req.RunID)
	}
	selectedSkill, err := selectedSkillFromDecisionRecord(decisionRecord, nil, caps.stableSkills)
	if err != nil {
		return nil, err
	}
	return &runSelection{decisionRecord: decisionRecord, selectedSkill: selectedSkill}, nil
}
