package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"github.com/ycvk/acorn/internal/events"
)

type RunContextBridge interface {
	CurrentRunID(context.Context) string
	CurrentSessionID(context.Context) string
}

type LifecycleEventAppender interface {
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}

type ToolOptions struct {
	Loader SkillLoader
	Store  LifecycleEventAppender
	Bridge RunContextBridge
}

type AssessToolInput struct {
	ID              string            `json:"id" jsonschema:"required,description=Skill ID to assess"`
	Verdict         AssessmentVerdict `json:"verdict" jsonschema:"required,description=Assessment verdict: verified, needs_eval, or retired"`
	Reason          string            `json:"reason" jsonschema:"required,description=Why this verdict was chosen"`
	EvidenceRefs    []string          `json:"evidence_refs,omitempty" jsonschema:"description=Evidence refs backing the assessment"`
	ChangesRequired []string          `json:"changes_required,omitempty" jsonschema:"description=Changes required before promotion or to justify retirement"`
}

type AssessToolOutput struct {
	Assessment SkillAssessment `json:"assessment"`
	Updated    *Spec           `json:"updated,omitempty"`
}

func BuildSkillLifecycleTools(opts ToolOptions) ([]einotool.BaseTool, error) {
	if opts.Loader == nil {
		return nil, errors.New("skill lifecycle tools require loader")
	}
	if opts.Store == nil {
		return nil, errors.New("skill lifecycle tools require event store")
	}
	if opts.Bridge == nil {
		return nil, errors.New("skill lifecycle tools require run context bridge")
	}
	assessTool, err := toolutils.InferTool("skill_assess", "Assess a filesystem skill using durable evidence refs, then update lifecycle status when the skill source is mutable.", func(ctx context.Context, input AssessToolInput) (string, error) {
		output, err := assessSkill(ctx, opts, input)
		if err != nil {
			return "", err
		}
		body, err := json.Marshal(output)
		if err != nil {
			return "", fmt.Errorf("marshal skill assessment result: %w", err)
		}
		return string(body), nil
	})
	if err != nil {
		return nil, fmt.Errorf("build skill_assess tool: %w", err)
	}
	return []einotool.BaseTool{assessTool}, nil
}

func assessSkill(ctx context.Context, opts ToolOptions, input AssessToolInput) (*AssessToolOutput, error) {
	trimmedID := strings.TrimSpace(input.ID)
	if trimmedID == "" {
		return nil, errors.New("id is required")
	}
	verdict := normalizeAssessmentVerdict(input.Verdict)
	if verdict == "" {
		return nil, fmt.Errorf("skill assess %s: verdict is required", trimmedID)
	}
	if err := validateAssessmentVerdict(verdict); err != nil {
		return nil, fmt.Errorf("skill assess %s: %w", trimmedID, err)
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("skill assess %s: reason is required", trimmedID)
	}
	evidenceRefs := uniqueNonEmpty(input.EvidenceRefs)
	changesRequired := uniqueNonEmpty(input.ChangesRequired)
	if verdict == AssessmentVerified && len(evidenceRefs) == 0 {
		return nil, fmt.Errorf("skill assess %s: verdict verified requires evidence_refs", trimmedID)
	}
	skill, err := findSkill(ctx, opts.Loader, trimmedID)
	if err != nil {
		return nil, err
	}
	runID := strings.TrimSpace(opts.Bridge.CurrentRunID(ctx))
	if runID == "" {
		return nil, fmt.Errorf("skill assess %s: current run id is required", trimmedID)
	}
	assessment := SkillAssessment{
		AssessmentID:    fmt.Sprintf("skill_assessment_%d", time.Now().UTC().UnixNano()),
		SkillID:         skill.ID,
		Verdict:         verdict,
		Reason:          reason,
		SourceRunID:     runID,
		EvidenceRefs:    append([]string(nil), evidenceRefs...),
		ChangesRequired: append([]string(nil), changesRequired...),
	}

	var updated *Spec
	status, err := lifecycleStatusForAssessment(verdict)
	if err != nil {
		return nil, err
	}
	applied := false
	if skillMutableSource(skill.Source) {
		updated, err = opts.Loader.UpdateSkillLifecycle(ctx, skill.ID, LifecycleUpdate{
			Status:         status,
			EvidenceRefs:   evidenceRefs,
			UpdatedByRunID: runID,
		})
		if err != nil {
			return nil, err
		}
		applied = true
	}
	assessment.Applied = applied

	event := LifecycleEvent{
		SkillID:         skill.ID,
		Action:          "assessed",
		Status:          string(status),
		Verdict:         string(verdict),
		Reason:          reason,
		EvidenceRefs:    append([]string(nil), evidenceRefs...),
		AssessmentID:    assessment.AssessmentID,
		ChangesRequired: append([]string(nil), changesRequired...),
		Applied:         applied,
		Assessment:      &assessment,
	}
	if err := emitAssessmentLifecycleEvent(ctx, opts.Store, runID, event); err != nil {
		return nil, err
	}
	return &AssessToolOutput{Assessment: assessment, Updated: updated}, nil
}

func findSkill(ctx context.Context, loader SkillLoader, id string) (Spec, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return Spec{}, errors.New("id is required")
	}
	scan, err := loader.ScanSkills(ctx)
	if err != nil {
		return Spec{}, err
	}
	for _, item := range scan.Skills {
		if item.ID == trimmedID {
			return item, nil
		}
	}
	return Spec{}, fmt.Errorf("%w: %s", ErrNotFound, trimmedID)
}

func normalizeAssessmentVerdict(verdict AssessmentVerdict) AssessmentVerdict {
	return AssessmentVerdict(strings.ToLower(strings.TrimSpace(string(verdict))))
}

func validateAssessmentVerdict(verdict AssessmentVerdict) error {
	switch verdict {
	case AssessmentVerified, AssessmentNeedsEval, AssessmentRetired:
		return nil
	default:
		return fmt.Errorf("verdict %q is invalid", verdict)
	}
}

func lifecycleStatusForAssessment(verdict AssessmentVerdict) (LifecycleStatus, error) {
	switch verdict {
	case AssessmentVerified:
		return LifecycleVerified, nil
	case AssessmentNeedsEval:
		return LifecycleNeedsEval, nil
	case AssessmentRetired:
		return LifecycleRetired, nil
	default:
		return "", fmt.Errorf("assessment verdict %q cannot be applied", verdict)
	}
}

func emitAssessmentLifecycleEvent(ctx context.Context, store LifecycleEventAppender, runID string, event LifecycleEvent) error {
	if store == nil {
		return errors.New("skill lifecycle event store is required")
	}
	_, err := store.AppendEventContext(ctx, runID, "skill.lifecycle", map[string]any{
		"skill_lifecycle": event,
	})
	if err != nil {
		return fmt.Errorf("append skill lifecycle event: %w", err)
	}
	return nil
}

func skillMutableSource(source string) bool {
	switch strings.TrimSpace(source) {
	case WorkspaceScope, GeneratedScope, UserScope:
		return true
	default:
		return false
	}
}
