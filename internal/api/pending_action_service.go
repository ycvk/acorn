package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

var ErrPendingActionDecisionInvalid = errors.New("pending action decision invalid")

type PendingActionService struct {
	store    core.SessionStore
	resumer  runResumer
	packages skillPackages
	watches  newsSources
}

// newsSources reads the news source a news_source call changes.
type newsSources interface {
	LoadWatch(ctx context.Context, id int64) (*core.Watch, error)
}

// skillPackages reads the package a skill_import would store and the files a
// skill_write builds on.
type skillPackages interface {
	StagedSkillPackage(ctx context.Context, sha256 string) (core.SkillPackage, error)
	SkillFiles(ctx context.Context, id string, revision int64) ([]core.SkillFile, error)
}

// runResumer resumes an interrupted run once nothing in it awaits a decision.
type runResumer interface {
	ResumeIfReady(ctx context.Context, runID string) error
}

func NewPendingActionService(store core.SessionStore) *PendingActionService {
	return &PendingActionService{store: store}
}

// WithSkillPackages shows a skill_import's staged package, and the manifest
// a skill_write keeps, in their detail.
func (s *PendingActionService) WithSkillPackages(packages skillPackages) *PendingActionService {
	s.packages = packages
	return s
}

// WithNewsSources names the news source a news_source call changes in its
// detail.
func (s *PendingActionService) WithNewsSources(watches newsSources) *PendingActionService {
	s.watches = watches
	return s
}

// WithResumer makes every decision try to resume the decided run.
func (s *PendingActionService) WithResumer(resumer runResumer) *PendingActionService {
	s.resumer = resumer
	return s
}

type PendingActionDetail struct {
	PendingActionSummary
	Payload map[string]any
	Reason  string
	Rule    string
	// ToolDetail is the whole text a tool approval acts on when its summary
	// cannot hold it, such as the skill skill_write or skill_import would
	// store with its package's manifest first.
	ToolDetail string
}

type PendingActionDecisionInput struct {
	Decision         string
	SelectedOptionID string
	Answer           string
}

func (s *PendingActionService) List(ctx context.Context, limit int) ([]PendingActionSummary, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	records, err := s.store.ListPendingActions(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]PendingActionSummary, 0, len(records))
	for _, record := range records {
		run, err := s.store.LoadRun(ctx, record.RunID)
		if err != nil {
			return nil, err
		}
		summary, err := buildPendingActionSummary(record, *run)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

func (s *PendingActionService) Get(ctx context.Context, actionID string) (*PendingActionDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil, errors.New("pending action id is required")
	}
	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if record.Status != core.PendingActionStatusPending {
		return nil, fmt.Errorf("%w: status %q", core.ErrPendingActionDecided, record.Status)
	}
	run, err := s.store.LoadRun(ctx, record.RunID)
	if err != nil {
		return nil, err
	}
	summary, err := buildPendingActionSummary(*record, *run)
	if err != nil {
		return nil, err
	}
	payload, err := pendingActionPayload(*record)
	if err != nil {
		return nil, err
	}
	detail, err := s.toolDetail(ctx, summary.ToolName, topLevelString(payload, "arguments"))
	if err != nil {
		return nil, err
	}
	return &PendingActionDetail{
		PendingActionSummary: summary,
		Payload:              payload,
		Reason:               record.Reason,
		Rule:                 "",
		ToolDetail:           detail,
	}, nil
}

// toolDetail is the whole text a tool approval acts on when its summary
// cannot hold it: the skill a skill_write or skill_import would store, its
// package's manifest first. A package no longer staged shows as such, and the
// import fails.
func (s *PendingActionService) toolDetail(ctx context.Context, toolName, arguments string) (string, error) {
	switch toolName {
	case "news_source":
		return s.newsSourceDetail(ctx, arguments)
	case "skill_write", "skill_import":
	default:
		return "", nil
	}
	if s.packages == nil {
		return "", fmt.Errorf("pending action service: skill packages are required to show %s", toolName)
	}
	if toolName == "skill_write" {
		args := skillWriteArgs(arguments)
		spec, parseErr := skills.Parse(args.Markdown)
		var kept []byte
		if parseErr == nil {
			var err error
			if kept, err = s.keptManifest(ctx, spec.Name, args); err != nil {
				return "", err
			}
		}
		detail := skillWriteDetail(args, kept)
		if parseErr != nil {
			detail = fmt.Sprintf("SKILL.md does not parse, and the write will fail: %v\n\n%s", parseErr, detail)
		}
		return detail, nil
	}
	var args struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("skill_import arguments: %w", err)
	}
	pkg, err := s.packages.StagedSkillPackage(ctx, strings.TrimSpace(args.SHA256))
	switch {
	case errors.Is(err, core.ErrSkillPackageNotStaged):
		return fmt.Sprintf("Package %s is not staged; the import will fail.", args.SHA256), nil
	case err != nil:
		return "", err
	}
	return skillPackageDetail(pkg.Markdown, pkg.Files, nil, nil), nil
}

// newsSourceDetail says which news source a news_source call takes into the
// briefing or stops, and why.
func (s *PendingActionService) newsSourceDetail(ctx context.Context, arguments string) (string, error) {
	if s.watches == nil {
		return "", errors.New("pending action service: news sources are required to show news_source")
	}
	var args struct {
		Action string `json:"action"`
		ID     int64  `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("news_source arguments: %w", err)
	}
	w, err := s.watches.LoadWatch(ctx, args.ID)
	if errors.Is(err, core.ErrWatchNotFound) {
		return fmt.Sprintf("News source #%d does not exist; the call will fail.", args.ID), nil
	}
	if err != nil {
		return "", err
	}
	source := fmt.Sprintf("news source #%d %s (%s, section %s)", w.ID, w.Name, w.Target, w.Section)
	var what string
	switch args.Action {
	case "promote":
		what = "Take " + source + " into the morning briefing."
	case "stop":
		what = "Stop " + source + "; the morning briefing no longer takes it."
	default:
		what = fmt.Sprintf("Action %q on %s is neither promote nor stop; the call will fail.", args.Action, source)
	}
	return what + "\n\nReason: " + strings.TrimSpace(args.Reason), nil
}

// keptManifest is the manifest a skill_write keeps from its base revision:
// one it neither replaces nor deletes.
func (s *PendingActionService) keptManifest(ctx context.Context, name string, args skillWriteArguments) ([]byte, error) {
	if _, ok := args.Files[skills.ManifestFileName]; ok || args.BaseRevision == 0 || slices.Contains(args.Delete, skills.ManifestFileName) {
		return nil, nil
	}
	files, err := s.packages.SkillFiles(ctx, name, args.BaseRevision)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Path == skills.ManifestFileName {
			return f.Content, nil
		}
	}
	return nil, nil
}

func (s *PendingActionService) Decide(ctx context.Context, actionID string, input PendingActionDecisionInput) (*core.PendingActionRecord, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil, errors.New("pending action id is required")
	}

	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}

	status, decisionJSON, eventKind, eventPayload, err := buildPendingActionDecision(*record, input)
	if err != nil {
		return nil, err
	}

	record, err = s.store.DecidePendingAction(ctx, actionID, status, string(decisionJSON))
	if err != nil {
		return nil, err
	}
	if _, err := s.store.AppendEvent(ctx, record.RunID, eventKind, eventPayload); err != nil {
		return nil, fmt.Errorf("append %s event: %w", eventKind, err)
	}
	if s.resumer != nil {
		if err := s.resumer.ResumeIfReady(ctx, record.RunID); err != nil {
			return nil, fmt.Errorf("resume run %s after decision: %w", record.RunID, err)
		}
	}
	return record, nil
}

func buildPendingActionDecision(record core.PendingActionRecord, input PendingActionDecisionInput) (core.PendingActionStatus, []byte, string, map[string]any, error) {
	switch record.Kind {
	case core.PendingActionKindElicitation:
		return buildAcceptDeclineDecision(record, input, "elicitation.decided")
	case core.PendingActionKindToolApproval:
		return buildAcceptDeclineDecision(record, input, "tool_approval.decided")
	case core.PendingActionKindOperatorQuestion:
		return buildOperatorQuestionDecision(record, input)
	default:
		return "", nil, "", nil, fmt.Errorf("unsupported pending action kind %q", record.Kind)
	}
}
