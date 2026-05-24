package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"

	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/providerusage"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/terminalsession"
	"github.com/ycvk/acorn/internal/toolresult"
	"github.com/ycvk/acorn/internal/workspace"
)

type RunService struct {
	newExecutor func(context.Context) (executorHandle, error)
	controller  *runtime.RunController
}

func NewRunService(newExecutor func(context.Context) (executorHandle, error), controller *runtime.RunController) *RunService {
	return &RunService{newExecutor: newExecutor, controller: controller}
}

func (s *RunService) Run(ctx context.Context, input, skillID string, sink runtime.StreamSink) (*runtime.Result, error) {
	if s == nil || s.newExecutor == nil {
		return nil, errors.New("run executor factory is nil")
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, err
	}
	return exec.Run(ctx, input, skillID, sink)
}

func (s *RunService) InterruptRun(ctx context.Context, runID string) error {
	_ = ctx
	if s == nil || s.controller == nil {
		return errors.New("run controller is nil")
	}
	return s.controller.Interrupt(runID)
}

type ResumeService struct {
	trace       *TraceService
	newExecutor func(context.Context) (executorHandle, error)
	pending     runtime.PendingResumeStore
}

func NewResumeService(trace *TraceService, newExecutor func(context.Context) (executorHandle, error), pending runtime.PendingResumeStore) *ResumeService {
	return &ResumeService{trace: trace, newExecutor: newExecutor, pending: pending}
}

func (s *ResumeService) FindPendingResume(ctx context.Context) (*runtime.PendingResumeInfo, error) {
	if s == nil || s.pending == nil {
		return nil, errors.New("resume pending store is nil")
	}
	return runtime.FindPendingResume(ctx, s.pending)
}

func (s *ResumeService) Resume(ctx context.Context, runID string, sink runtime.StreamSink) (*runtime.Result, error) {
	if s == nil || s.trace == nil {
		return nil, errors.New("resume trace service is nil")
	}
	if s.newExecutor == nil {
		return nil, errors.New("resume executor factory is nil")
	}
	targets, err := s.trace.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, err
	}
	return exec.ResumeWithTargets(ctx, runID, targets, sink)
}

type TraceService struct {
	store *storesqlite.Store
}

type ResumeStatus struct {
	RunID        string           `json:"run_id,omitempty"`
	Status       events.RunStatus `json:"status,omitempty"`
	Resumable    bool             `json:"resumable"`
	InterruptIDs []string         `json:"interrupt_ids,omitempty"`
	Reason       string           `json:"reason,omitempty"`
}

func NewTraceService(store *storesqlite.Store) *TraceService {
	return &TraceService{store: store}
}

func (s *TraceService) Trace(ctx context.Context, runID string) (*runtime.Trace, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	trace := runtime.BuildTrace(run, items)
	if trace == nil {
		return nil, nil
	}
	return trace, nil
}

func (s *TraceService) ResumeStatus(ctx context.Context, runID string) (*ResumeStatus, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	status := buildResumeStatus(runID, run, items)
	if status == nil || run == nil || run.Status != events.RunStatusInterrupted {
		return status, nil
	}
	targets, inferReason := s.inferResumeTargetsOrReason(ctx, runID)
	if inferReason != "" {
		status.Resumable = false
		status.Reason = inferReason
		return status, nil
	}
	status.Resumable = true
	status.Reason = fmt.Sprintf("run %s is interrupted and resumable via %d pending actions", runID, len(targets))
	return status, nil
}

func (s *TraceService) inferResumeTargetsOrReason(ctx context.Context, runID string) (map[string]any, string) {
	targets, err := s.InferResumeTargets(ctx, runID)
	if err != nil {
		return nil, err.Error()
	}
	return targets, ""
}

func (s *TraceService) InferResumeTargets(ctx context.Context, runID string) (map[string]any, error) {
	run, items, err := s.loadRunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	status := buildResumeStatus(runID, run, items)
	if !status.Resumable {
		return nil, fmt.Errorf("%w: %s", runtime.ErrRunNotInterrupted, status.Reason)
	}
	contexts, err := runtime.LatestRootInterruptContexts(items)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", runtime.ErrRunNotInterrupted, err)
	}
	targets := make(map[string]any, len(contexts))
	for _, ctxItem := range contexts {
		target, err := s.resumeTargetsForContext(ctx, runID, ctxItem)
		if err != nil {
			return nil, err
		}
		for interruptID, payload := range target {
			targets[interruptID] = payload
		}
	}
	return targets, nil
}

func (s *TraceService) resumeTargetsForContext(ctx context.Context, runID string, interrupt runtime.StreamInterruptContext) (map[string]any, error) {
	switch kind := interruptInfoKind(interrupt.Info); kind {
	case "", "run_command_pause":
		return defaultTargets(interrupt.ID), nil
	case "operator_question":
		return s.operatorQuestionTargets(ctx, runID, interrupt)
	default:
		return nil, fmt.Errorf("run %s interrupt %s has unsupported kind %q", runID, interrupt.ID, kind)
	}
}

func (s *TraceService) operatorQuestionTargets(ctx context.Context, runID string, interrupt runtime.StreamInterruptContext) (map[string]any, error) {
	actionID := interruptInfoField(interrupt.Info, "action_id")
	if actionID == "" {
		return nil, fmt.Errorf("run %s interrupt %s operator_question is missing action_id", runID, interrupt.ID)
	}
	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if record.RunID != runID {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s belongs to run %s", runID, interrupt.ID, actionID, record.RunID)
	}
	if record.Kind != events.PendingActionKindOperatorQuestion {
		return nil, fmt.Errorf("run %s interrupt %s action %s has kind %q", runID, interrupt.ID, actionID, record.Kind)
	}
	if record.Status == events.PendingActionStatusPending {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s is still pending", runID, interrupt.ID, actionID)
	}
	if record.Status != events.PendingActionStatusApproved && record.Status != events.PendingActionStatusRejected {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s has unsupported status %q", runID, interrupt.ID, actionID, record.Status)
	}
	var decision map[string]any
	if err := json.Unmarshal([]byte(record.DecisionJSON), &decision); err != nil {
		return nil, fmt.Errorf("run %s interrupt %s operator_question action %s has invalid decision_json: %w", runID, interrupt.ID, actionID, err)
	}
	decision["action_id"] = actionID
	return map[string]any{interrupt.ID: decision}, nil
}

func interruptInfoKind(raw any) string {
	return interruptInfoField(raw, "kind")
}

func interruptInfoField(raw any, key string) string {
	info, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := info[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (s *TraceService) loadRunEvents(ctx context.Context, runID string) (*events.RunRecord, []events.EventRecord, error) {
	if s == nil || s.store == nil {
		return nil, nil, fmt.Errorf("trace store is nil")
	}
	run, err := s.store.LoadRun(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	items, err := s.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, nil, err
	}
	return run, items, nil
}

func buildResumeStatus(runID string, run *events.RunRecord, items []events.EventRecord) *ResumeStatus {
	status := &ResumeStatus{RunID: runID}
	if run == nil {
		status.Reason = fmt.Sprintf("run %s is unavailable", runID)
		return status
	}
	status.Status = run.Status

	switch run.Status {
	case events.RunStatusInterrupted:
		interruptIDs, err := runtime.LatestRootInterruptIDs(items)
		if err != nil {
			status.Reason = fmt.Sprintf("run %s is interrupted but missing resumable interrupt data: %v", runID, err)
			return status
		}
		status.Resumable = true
		status.InterruptIDs = append(status.InterruptIDs, interruptIDs...)
		status.Reason = fmt.Sprintf("run %s is interrupted and waiting on %d root actions", runID, len(interruptIDs))
		return status
	case events.RunStatusFailed:
		status.Reason = fmt.Sprintf("run %s failed and cannot be resumed; inspect run detail or start a new client run", runID)
	case events.RunStatusSucceeded:
		status.Reason = fmt.Sprintf("run %s completed and does not need resume", runID)
	case events.RunStatusRunning:
		status.Reason = fmt.Sprintf("run %s is still running and has no persisted interrupt to resume", runID)
	default:
		status.Reason = fmt.Sprintf("run %s is not resumable from status %s", runID, run.Status)
	}
	return status
}

func defaultTargets(interruptID string) map[string]any {
	return map[string]any{
		interruptID: map[string]any{},
	}
}

type RuntimeWorkbenchService struct {
	cfg   RuntimeWorkbenchConfig
	store runtimeWorkbenchStore
	plans runtimeWorkbenchPlanStore
	trace *TraceService
}

type RuntimeWorkbenchConfig struct {
	Workspace *workspace.Workspace
}

type runtimeWorkbenchStore interface {
	LoadSession(ctx context.Context, sessionID string) (*events.SessionRecord, error)
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*events.RunRecord, error)
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error)
	GetSessionSummary(ctx context.Context, sessionID string) (*runtimehistory.SessionSummary, error)
	ListByRun(ctx context.Context, runID string) ([]toolresult.Record, error)
	ListArtifactsByRun(ctx context.Context, runID string) ([]artifacts.Record, error)
	ListTerminalSessionsByRun(ctx context.Context, runID string) ([]terminalsession.SessionRecord, error)
	ListTerminalSessionLogs(ctx context.Context, terminalSessionID string) ([]terminalsession.LogRecord, error)
	ListProviderUsagesByRun(ctx context.Context, runID string) ([]providerusage.Record, error)
}

type runtimeWorkbenchPlanStore interface {
	LoadRuntimePlan(ctx context.Context, sessionID string) (*runtime.Plan, error)
}

func NewRuntimeWorkbenchService(cfg RuntimeWorkbenchConfig, store runtimeWorkbenchStore, trace *TraceService) *RuntimeWorkbenchService {
	return &RuntimeWorkbenchService{
		cfg:   cfg,
		store: store,
		plans: runtimeWorkbenchPlanStoreFrom(store),
		trace: trace,
	}
}

func runtimeWorkbenchPlanStoreFrom(store runtimeWorkbenchStore) runtimeWorkbenchPlanStore {
	if typed, ok := store.(runtimeWorkbenchPlanStore); ok {
		return typed
	}
	if typed, ok := store.(runtimePlanRecordStore); ok {
		return newRuntimePlanStoreAdapter(typed)
	}
	return nil
}

func (s *RuntimeWorkbenchService) Load(ctx context.Context, sessionID string) (*RuntimeWorkbench, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("runtime workbench store is nil")
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return nil, errors.New("session id is required")
	}

	session, err := s.store.LoadSession(ctx, trimmedSessionID)
	if err != nil {
		return nil, err
	}
	latestRun, err := s.store.LoadLatestRunForSession(ctx, trimmedSessionID)
	if err != nil {
		return nil, err
	}

	workbench := &RuntimeWorkbench{
		SessionID: trimmedSessionID,
		Title:     session.Title,
	}
	if latestRun != nil {
		workbench.LatestRunID = latestRun.RunID
		workbench.LatestRunStatus = latestRun.Status
		workbench.LatestRunMode = string(latestRun.OrchestrationMode)
		workbench.LatestRunDepth = latestRun.Depth
		workbench.ParentRunID = latestRun.ParentRunID
	}
	workbench.State = runtime.DeriveSessionState(latestRun, false)

	if latestRun != nil && latestRun.Status == events.RunStatusInterrupted {
		if s.trace == nil {
			return nil, fmt.Errorf("load resume status for run %s: trace service is nil", latestRun.RunID)
		}
		resumeStatus, resumeErr := s.trace.ResumeStatus(ctx, latestRun.RunID)
		if resumeErr != nil {
			return nil, fmt.Errorf("load resume status for run %s: %w", latestRun.RunID, resumeErr)
		}
		if resumeStatus == nil {
			return nil, fmt.Errorf("load resume status for run %s: resume status is nil", latestRun.RunID)
		}
		if resumeStatus != nil {
			workbench.Resumable = resumeStatus.Resumable
			workbench.ResumeReason = resumeStatus.Reason
		}
	}
	if workbench.ResumeReason == "" {
		workbench.ResumeReason = defaultResumeReason(latestRun)
	}

	summary, summaryErr := s.store.GetSessionSummary(ctx, trimmedSessionID)
	if summaryErr != nil {
		return nil, summaryErr
	}
	workbench.SessionSummary = summary

	if latestRun != nil {
		rawEvents, eventsErr := s.store.LoadEvents(ctx, latestRun.RunID)
		if eventsErr != nil {
			return nil, eventsErr
		}
		if len(rawEvents) > 0 {
			workbench.TraceSummary = runtime.BuildTraceSummary(rawEvents)
			workbench.SelectedSkill = runtime.SelectedSkillFromEvents(rawEvents)
			workbench.Subagents = buildSubagentRuns(rawEvents)
		}

		decisionRecord, decisionErr := s.store.LoadRunDecision(ctx, latestRun.RunID)
		if decisionErr != nil {
			return nil, decisionErr
		}
		workbench.LatestDecision = decisionRecord

		if s.plans != nil {
			plan, planErr := s.plans.LoadRuntimePlan(ctx, trimmedSessionID)
			if planErr != nil && !errors.Is(planErr, store.ErrPlanNotFound) {
				return nil, planErr
			}
			workbench.Plan = plan
		}
		if workbench.Plan != nil {
			workbench.Evidence = flattenPlanEvidence(workbench.Plan)
		}

		toolResults, toolResultsErr := s.store.ListByRun(ctx, latestRun.RunID)
		if toolResultsErr != nil {
			return nil, toolResultsErr
		}
		workbench.MutationCheckpoints = buildMutationCheckpointSummaries(toolResults)
		var rollbackErr error
		workbench.RollbackResults, rollbackErr = buildRollbackSummaries(toolResults)
		if rollbackErr != nil {
			return nil, rollbackErr
		}
		workbench.ContextEconomy = buildContextEconomySummary(rawEvents, toolResults)

		artifactRecords, artifactsErr := s.store.ListArtifactsByRun(ctx, latestRun.RunID)
		if artifactsErr != nil {
			return nil, artifactsErr
		}
		workbench.Artifacts = buildArtifactSummaries(artifactRecords)

		terminalRecords, terminalErr := s.store.ListTerminalSessionsByRun(ctx, latestRun.RunID)
		if terminalErr != nil {
			return nil, terminalErr
		}
		workbench.TerminalSessions, terminalErr = s.buildTerminalSessionSummaries(ctx, terminalRecords)
		if terminalErr != nil {
			return nil, terminalErr
		}

		providerUsages, usageErr := s.store.ListProviderUsagesByRun(ctx, latestRun.RunID)
		if usageErr != nil {
			return nil, usageErr
		}
		workbench.ProviderUsage = buildProviderUsageSummary(providerUsages)
	}

	if s.cfg.Workspace != nil {
		workspaceRoot := s.cfg.Workspace.Root()
		workbench.WorkspaceRoot = workspaceRoot
		gitStatus, gitErr := s.cfg.Workspace.InspectGitStatus(ctx, "")
		if gitErr != nil {
			workbench.GitStatus = WorkspaceGitStatus{
				WorkspaceRoot: workspaceRoot,
				Available:     false,
				Clean:         false,
				Error:         gitErr.Error(),
			}
		} else {
			workbench.GitStatus = WorkspaceGitStatus{
				WorkspaceRoot: gitStatus.WorkspaceRoot,
				Available:     true,
				Branch:        gitStatus.Branch,
				Clean:         gitStatus.Clean,
				Entries:       append([]workspace.GitStatusEntry(nil), gitStatus.Entries...),
			}
		}
	}

	workbench.NextStepHint = runtimeWorkbenchNextStepHint(workbench)
	return workbench, nil
}

func flattenPlanEvidence(plan *runtime.Plan) []runtime.PlanEvidence {
	if plan == nil {
		return nil
	}
	evidence := make([]runtime.PlanEvidence, 0)
	for _, step := range plan.Steps {
		evidence = append(evidence, step.Evidence...)
	}
	sort.SliceStable(evidence, func(i, j int) bool {
		return evidence[i].RecordedAt.After(evidence[j].RecordedAt)
	})
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}
	return evidence
}

func buildContextEconomySummary(rawEvents []events.EventRecord, records []toolresult.Record) ContextEconomySummary {
	summary := ContextEconomySummary{
		ToolResultCount: len(records),
		ToolResults:     make([]ContextToolResultSummary, 0, len(records)),
	}
	for _, record := range records {
		item := ContextToolResultSummary{
			ResultRef:     record.ResultRef,
			ToolName:      record.ToolName,
			Status:        string(record.Status),
			Preview:       record.Preview,
			TokenEstimate: record.TokenEstimate,
			FullTextBytes: len([]byte(record.FullText)),
			Elided:        strings.TrimSpace(record.Preview) != strings.TrimSpace(record.FullText),
			EvidenceRefs:  evidenceRefStrings(record.EvidenceRefs),
		}
		if item.Elided {
			summary.ElidedToolResultCount++
		}
		summary.ToolResultTokenEstimate += record.TokenEstimate
		summary.ToolResults = append(summary.ToolResults, item)
	}
	sort.SliceStable(summary.ToolResults, func(i, j int) bool {
		return summary.ToolResults[i].ResultRef < summary.ToolResults[j].ResultRef
	})

	trace := runtime.BuildTrace(nil, rawEvents)
	for _, item := range trace.Items {
		if pressure := item.GetContextPressure(); pressure != nil {
			summary.LatestPressure = &ContextPressureSummary{
				State:                 pressure.State,
				EstimatedInputTokens:  pressure.EstimatedInputTokens,
				EffectiveWindowTokens: pressure.EffectiveWindowTokens,
				PercentUsed:           pressure.PercentUsed,
			}
		}
		if compressed := item.GetContextCompressed(); compressed != nil {
			summary.LatestCompression = &ContextCompressionSummary{
				BoundaryID:   compressed.BoundaryID,
				TokensBefore: compressed.TokensBefore,
				TokensAfter:  compressed.TokensAfter,
				Summary:      compressed.SummarySnippet,
			}
		}
		if prepared := item.GetMemoryPrepared(); prepared != nil {
			for _, entry := range prepared.Entries {
				summary.MemoryRefs = appendUniqueStrings(summary.MemoryRefs, []string{entry.Ref})
			}
			for _, nudge := range prepared.Nudges {
				summary.MemoryRefs = appendUniqueStrings(summary.MemoryRefs, []string{nudge.Ref})
			}
		}
		if activation := item.GetProcedureActivation(); activation != nil {
			summary.ProcedureRefs = appendUniqueStrings(summary.ProcedureRefs, []string{activation.ProcedureRef})
		}
	}
	return summary
}

func buildArtifactSummaries(records []artifacts.Record) []ArtifactSummary {
	if len(records) == 0 {
		return nil
	}
	items := make([]ArtifactSummary, 0, len(records))
	for _, record := range records {
		items = append(items, ArtifactSummary{
			ArtifactID:          record.ArtifactID,
			RunID:               record.RunID,
			SessionID:           record.SessionID,
			SourceToolResultRef: record.SourceToolResultRef,
			Kind:                string(record.Kind),
			Title:               record.Title,
			MIMEType:            record.MIMEType,
			SizeBytes:           record.SizeBytes,
			SHA256:              record.SHA256,
			CreatedAt:           record.CreatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ArtifactID < items[j].ArtifactID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (s *RuntimeWorkbenchService) buildTerminalSessionSummaries(ctx context.Context, records []terminalsession.SessionRecord) ([]TerminalSessionSummary, error) {
	if len(records) == 0 {
		return nil, nil
	}
	items := make([]TerminalSessionSummary, 0, len(records))
	for _, record := range records {
		logs, err := s.store.ListTerminalSessionLogs(ctx, record.TerminalSessionID)
		if err != nil {
			return nil, err
		}
		items = append(items, TerminalSessionSummary{
			TerminalSessionID: record.TerminalSessionID,
			RunID:             record.RunID,
			SessionID:         record.SessionID,
			Label:             record.Label,
			CommandJSON:       record.CommandJSON,
			Cwd:               record.Cwd,
			Interactive:       record.Interactive,
			PTY:               record.PTY,
			Status:            string(record.Status),
			ProcessID:         copyOptionalInt(record.ProcessID),
			ProcessGroupID:    copyOptionalInt(record.ProcessGroupID),
			ExitCode:          copyOptionalInt(record.ExitCode),
			Signal:            record.Signal,
			StdoutArtifactID:  record.StdoutArtifactID,
			StderrArtifactID:  record.StderrArtifactID,
			PTYArtifactID:     record.PTYArtifactID,
			StartedAt:         copyOptionalTime(record.StartedAt),
			EndedAt:           copyOptionalTime(record.EndedAt),
			CreatedAt:         record.CreatedAt,
			UpdatedAt:         record.UpdatedAt,
			Logs:              buildTerminalSessionLogSummaries(logs),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].TerminalSessionID < items[j].TerminalSessionID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func buildTerminalSessionLogSummaries(records []terminalsession.LogRecord) []TerminalSessionLogSummary {
	if len(records) == 0 {
		return nil
	}
	items := make([]TerminalSessionLogSummary, 0, len(records))
	for _, record := range records {
		items = append(items, TerminalSessionLogSummary{
			LogID:             record.LogID,
			TerminalSessionID: record.TerminalSessionID,
			Stream:            string(record.Stream),
			ArtifactID:        record.ArtifactID,
			StartOffset:       record.StartOffset,
			SizeBytes:         record.SizeBytes,
			CreatedAt:         record.CreatedAt,
		})
	}
	return items
}

func copyOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	return new(*value)
}

func copyOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return new(value.UTC())
}

func buildProviderUsageSummary(records []providerusage.Record) ProviderUsageSummary {
	summary := ProviderUsageSummary{
		CallCount: len(records),
		Records:   make([]ProviderUsageCallSummary, 0, len(records)),
	}
	for _, record := range records {
		summary.PromptTokens += record.PromptTokens
		summary.CompletionTokens += record.CompletionTokens
		summary.TotalTokens += record.TotalTokens
		summary.CachedTokens += record.CachedTokens
		summary.ReasoningTokens += record.ReasoningTokens
		summary.Records = append(summary.Records, ProviderUsageCallSummary{
			UsageID:          record.UsageID,
			CallSite:         record.CallSite,
			ProviderName:     record.ProviderName,
			ModelName:        record.ModelName,
			PromptTokens:     record.PromptTokens,
			CompletionTokens: record.CompletionTokens,
			TotalTokens:      record.TotalTokens,
			CachedTokens:     record.CachedTokens,
			ReasoningTokens:  record.ReasoningTokens,
			CreatedAt:        record.CreatedAt,
		})
	}
	sort.SliceStable(summary.Records, func(i, j int) bool {
		return summary.Records[i].CreatedAt.Before(summary.Records[j].CreatedAt)
	})
	return summary
}

func evidenceRefStrings(items []toolresult.EvidenceRef) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Ref) != "" {
			out = append(out, strings.TrimSpace(item.Ref))
		}
	}
	return out
}

func buildSubagentRuns(raw []events.EventRecord) []SubagentRun {
	if len(raw) == 0 {
		return nil
	}
	byID := make(map[string]*SubagentRun)
	order := make([]string, 0)
	for _, event := range raw {
		item := runtime.BuildTrace(nil, []events.EventRecord{event}).Items
		if len(item) == 0 {
			continue
		}
		current := item[0]
		switch current.Kind {
		case runtime.StreamKindSubagentStarted:
			payload, ok := current.Payload.(*runtime.SubagentStartedPayload)
			if !ok || strings.TrimSpace(payload.SubRunID) == "" {
				continue
			}
			run := &SubagentRun{
				SubRunID:          payload.SubRunID,
				ParentRunID:       payload.ParentID,
				SessionID:         payload.SessionID,
				Depth:             payload.Depth,
				Task:              payload.Task,
				ChildRunMode:      payload.ChildRunMode,
				WorkspaceMode:     payload.WorkspaceMode,
				WorktreePath:      payload.WorktreePath,
				ContextMessages:   payload.ContextMessages,
				OrchestrationMode: payload.OrchestrationMode,
				ParentStepID:      payload.ParentStepID,
				State:             "started",
				UpdatedAt:         current.CreatedAt,
			}
			byID[payload.SubRunID] = run
			order = append(order, payload.SubRunID)
		case runtime.StreamKindSubagentCompleted:
			payload, ok := current.Payload.(*runtime.SubagentCompletedPayload)
			if !ok || strings.TrimSpace(payload.SubRunID) == "" {
				continue
			}
			run, ok := byID[payload.SubRunID]
			if !ok {
				run = &SubagentRun{SubRunID: payload.SubRunID}
				byID[payload.SubRunID] = run
				order = append(order, payload.SubRunID)
			}
			run.ParentRunID = payload.ParentID
			run.SessionID = payload.SessionID
			run.OrchestrationMode = payload.OrchestrationMode
			run.ParentStepID = payload.ParentStepID
			run.State = "completed"
			run.FinalStatus = payload.FinalStatus
			run.AcceptanceStatus = payload.AcceptanceStatus
			run.AcceptanceReasons = append([]string(nil), payload.AcceptanceReasons...)
			run.ChildRunMode = payload.ChildRunMode
			run.WorkspaceMode = payload.WorkspaceMode
			run.WorktreePath = payload.WorktreePath
			run.EvidenceRefs = append([]string(nil), payload.EvidenceRefs...)
			run.Summary = payload.Summary
			run.UpdatedAt = current.CreatedAt
		case runtime.StreamKindSubagentFailed:
			payload, ok := current.Payload.(*runtime.SubagentFailedPayload)
			if !ok || strings.TrimSpace(payload.SubRunID) == "" {
				continue
			}
			run, ok := byID[payload.SubRunID]
			if !ok {
				run = &SubagentRun{SubRunID: payload.SubRunID}
				byID[payload.SubRunID] = run
				order = append(order, payload.SubRunID)
			}
			run.ParentRunID = payload.ParentID
			run.SessionID = payload.SessionID
			run.OrchestrationMode = payload.OrchestrationMode
			run.ParentStepID = payload.ParentStepID
			run.State = "failed"
			run.AcceptanceStatus = payload.AcceptanceStatus
			run.AcceptanceReasons = append([]string(nil), payload.AcceptanceReasons...)
			run.ChildRunMode = payload.ChildRunMode
			run.WorkspaceMode = payload.WorkspaceMode
			run.WorktreePath = payload.WorktreePath
			run.Summary = payload.Error
			run.UpdatedAt = current.CreatedAt
		}
	}
	result := make([]SubagentRun, 0, len(order))
	for _, id := range order {
		if run, ok := byID[id]; ok {
			result = append(result, *run)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result
}

func buildMutationCheckpointSummaries(records []toolresult.Record) []MutationCheckpointSummary {
	byID := make(map[string]*MutationCheckpointSummary)
	for _, record := range records {
		paths := sideEffectPaths(record.SideEffects, workspace.MutationCheckpointEffect)
		if len(paths) == 0 {
			continue
		}
		checkpointID := firstSideEffectRef(record.SideEffects, workspace.MutationCheckpointEffect)
		if checkpointID == "" {
			continue
		}
		summary := byID[checkpointID]
		if summary == nil {
			summary = &MutationCheckpointSummary{CheckpointID: checkpointID}
			byID[checkpointID] = summary
		}
		summary.ToolResultRef = record.ResultRef
		summary.ToolName = record.ToolName
		summary.Status = string(record.Status)
		summary.Summary = record.Preview
		summary.CreatedAt = record.CreatedAt
		summary.Paths = appendUniqueStrings(summary.Paths, paths)
		if diffStat := mutationCheckpointDiffStat(record.FullText); diffStat != "" {
			summary.VerifiedDiffStat = diffStat
		}
	}
	return sortedMutationCheckpointSummaries(byID)
}

func buildRollbackSummaries(records []toolresult.Record) ([]RollbackSummary, error) {
	byID := make(map[string]*RollbackSummary)
	for _, record := range records {
		if strings.TrimSpace(record.ToolName) != "rollback_workspace_checkpoint" {
			continue
		}
		parsed, err := parseRollbackSummaryRecord(record)
		if err != nil {
			return nil, err
		}
		summary := byID[parsed.RollbackID]
		if summary == nil {
			summary = &RollbackSummary{RollbackID: parsed.RollbackID}
			byID[parsed.RollbackID] = summary
		}
		summary.CheckpointID = parsed.CheckpointID
		summary.ToolResultRef = record.ResultRef
		summary.ToolName = record.ToolName
		summary.Status = parsed.Status
		summary.RestoredPaths = appendUniqueStrings(summary.RestoredPaths, parsed.RestoredPaths)
		summary.ConflictPaths = appendUniqueStrings(summary.ConflictPaths, parsed.ConflictPaths)
		summary.Summary = record.Preview
		summary.Error = parsed.Error
		summary.CreatedAt = record.CreatedAt
	}
	return sortedRollbackSummaries(byID), nil
}

type rollbackSummaryPayload struct {
	CheckpointID  string   `json:"checkpoint_id"`
	RollbackID    string   `json:"rollback_id"`
	Status        string   `json:"status"`
	RestoredPaths []string `json:"restored_paths"`
	ConflictPaths []string `json:"conflict_paths"`
	Error         string   `json:"error"`
}

func parseRollbackSummaryRecord(record toolresult.Record) (RollbackSummary, error) {
	var payload rollbackSummaryPayload
	if err := json.Unmarshal([]byte(record.FullText), &payload); err != nil {
		if record.Status == toolresult.StatusFailed {
			return failedRollbackSummaryRecord(record), nil
		}
		return RollbackSummary{}, fmt.Errorf("parse rollback_workspace_checkpoint result %s: %w", record.ResultRef, err)
	}
	rollbackID := strings.TrimSpace(payload.RollbackID)
	if rollbackID == "" {
		rollbackID = strings.TrimSpace(record.ResultRef)
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = string(record.Status)
	}
	return RollbackSummary{
		RollbackID:    rollbackID,
		CheckpointID:  strings.TrimSpace(payload.CheckpointID),
		ToolResultRef: record.ResultRef,
		ToolName:      record.ToolName,
		Status:        status,
		RestoredPaths: trimmedWorkspacePaths(payload.RestoredPaths),
		ConflictPaths: trimmedWorkspacePaths(payload.ConflictPaths),
		Summary:       record.Preview,
		Error:         strings.TrimSpace(payload.Error),
		CreatedAt:     record.CreatedAt,
	}, nil
}

func failedRollbackSummaryRecord(record toolresult.Record) RollbackSummary {
	errText := strings.TrimSpace(record.ErrorReason)
	if errText == "" {
		errText = strings.TrimSpace(record.Preview)
	}
	if errText == "" {
		errText = strings.TrimSpace(record.FullText)
	}
	if errText == "" {
		errText = "workspace rollback failed"
	}
	return RollbackSummary{
		RollbackID:    strings.TrimSpace(record.ResultRef),
		ToolResultRef: record.ResultRef,
		ToolName:      record.ToolName,
		Status:        string(record.Status),
		Summary:       record.Preview,
		Error:         errText,
		CreatedAt:     record.CreatedAt,
	}
}

func mutationCheckpointDiffStat(fullText string) string {
	var payload struct {
		VerifiedDiffStat string `json:"verified_diff_stat"`
	}
	if err := json.Unmarshal([]byte(fullText), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.VerifiedDiffStat)
}

func sideEffectPaths(items []toolresult.SideEffectRef, kind string) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Kind) != kind {
			continue
		}
		if path := strings.TrimSpace(item.Path); path != "" {
			paths = append(paths, path)
		}
	}
	return trimmedWorkspacePaths(paths)
}

func firstSideEffectRef(items []toolresult.SideEffectRef, kind string) string {
	for _, item := range items {
		if strings.TrimSpace(item.Kind) == kind && strings.TrimSpace(item.Ref) != "" {
			return strings.TrimSpace(item.Ref)
		}
	}
	return ""
}

func trimmedWorkspacePaths(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, filepath.ToSlash(trimmed))
		}
	}
	return out
}

func appendUniqueStrings(base []string, values []string) []string {
	seen := make(map[string]struct{}, len(base))
	for _, item := range base {
		seen[strings.TrimSpace(item)] = struct{}{}
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		base = append(base, trimmed)
	}
	return base
}

func sortedMutationCheckpointSummaries(byID map[string]*MutationCheckpointSummary) []MutationCheckpointSummary {
	result := make([]MutationCheckpointSummary, 0, len(byID))
	for _, summary := range byID {
		if summary != nil {
			result = append(result, *summary)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CheckpointID < result[j].CheckpointID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func sortedRollbackSummaries(byID map[string]*RollbackSummary) []RollbackSummary {
	result := make([]RollbackSummary, 0, len(byID))
	for _, summary := range byID {
		if summary != nil {
			result = append(result, *summary)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].RollbackID < result[j].RollbackID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func runtimeWorkbenchNextStepHint(workbench *RuntimeWorkbench) string {
	if workbench == nil {
		return ""
	}
	if workbench.Resumable {
		return "可以继续当前工作，从上次中断处恢复。"
	}
	switch workbench.State {
	case runtime.SessionStateRunning:
		return "先查看最近一次轨迹，确认当前运行是否仍在进行中。"
	case runtime.SessionStateFailed:
		return "先查看失败轨迹，再决定继续当前工作还是补发新消息。"
	case runtime.SessionStateCompleted:
		return "可以基于当前上下文继续推进，或发送新消息开始下一步。"
	case runtime.SessionStateInterrupted:
		return "当前运行已中断，先检查中断上下文和最近证据。"
	case runtime.SessionStateNew:
		return "新建本地会话开始新的工作。"
	default:
		if strings.TrimSpace(workbench.LatestRunID) != "" {
			return "先查看最近一次运行和轨迹，再继续当前工作。"
		}
		return "发送第一条消息开始新的工作。"
	}
}

type runtimePlanRecordStore interface {
	LoadPlanBySession(ctx context.Context, sessionID string) (*store.PlanRecord, error)
}

type runtimePlanStoreAdapter struct {
	store runtimePlanRecordStore
}

func newRuntimePlanStoreAdapter(store runtimePlanRecordStore) runtimeWorkbenchPlanStore {
	if store == nil {
		return nil
	}
	return runtimePlanStoreAdapter{store: store}
}

func (s runtimePlanStoreAdapter) LoadRuntimePlan(ctx context.Context, sessionID string) (*runtime.Plan, error) {
	record, err := s.store.LoadPlanBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return runtimePlanFromStoreRecord(record), nil
}

func runtimePlanFromStoreRecord(record *store.PlanRecord) *runtime.Plan {
	if record == nil {
		return nil
	}
	plan := &runtime.Plan{
		PlanID:    record.PlanID,
		SessionID: record.SessionID,
		RunID:     record.RunID,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
	steps := make([]runtime.PlanStep, 0, len(record.Steps))
	for _, step := range record.Steps {
		current := runtime.PlanStep{
			ID:        step.ID,
			Action:    step.Action,
			Status:    runtime.PlanStepStatus(step.Status),
			DependsOn: append([]string(nil), step.DependsOn...),
			Risk:      runtime.PlanStepRisk(step.Risk),
			ToolHints: append([]string(nil), step.ToolHints...),
		}
		for _, target := range step.RepoTargets {
			current.RepoTargets = append(current.RepoTargets, runtime.PlanRepoTarget{
				Path:       target.Path,
				Symbol:     target.Symbol,
				StartLine:  target.StartLine,
				EndLine:    target.EndLine,
				Reason:     target.Reason,
				Confidence: target.Confidence,
			})
		}
		for _, intent := range step.VerificationIntent {
			current.VerificationIntent = append(current.VerificationIntent, runtime.VerificationIntent{
				Kind:    intent.Kind,
				Command: append([]string(nil), intent.Command...),
				Paths:   append([]string(nil), intent.Paths...),
				Reason:  intent.Reason,
			})
		}
		for _, evidence := range step.Evidence {
			current.Evidence = append(current.Evidence, runtime.PlanEvidence{
				ID:            evidence.ID,
				StepID:        evidence.StepID,
				Kind:          runtime.EvidenceKind(evidence.Kind),
				Status:        runtime.EvidenceStatus(evidence.Status),
				Summary:       evidence.Summary,
				ToolResultRef: evidence.ToolResultRef,
				ToolName:      evidence.ToolName,
				Command:       append([]string(nil), evidence.Command...),
				Paths:         append([]string(nil), evidence.Paths...),
				DiffRef:       evidence.DiffRef,
				ChildRunID:    evidence.ChildRunID,
				Error:         evidence.Error,
				SourceRunID:   evidence.SourceRunID,
				RecordedAt:    evidence.RecordedAt,
			})
		}
		steps = append(steps, current)
	}
	plan.Steps = steps
	return plan
}

// RuntimeWorkbench aggregates all runtime state for a session.
type RuntimeWorkbench struct {
	SessionID           string
	Title               string
	State               runtime.SessionState
	LatestRunID         string
	LatestRunStatus     events.RunStatus
	LatestRunMode       string
	LatestRunDepth      int
	ParentRunID         string
	Resumable           bool
	ResumeReason        string
	TraceSummary        *runtime.TraceSummary
	SelectedSkill       *runtime.SelectedSkill
	LatestDecision      *decision.Record
	SessionSummary      *runtimehistory.SessionSummary
	WorkspaceRoot       string
	GitStatus           WorkspaceGitStatus
	MutationCheckpoints []MutationCheckpointSummary
	RollbackResults     []RollbackSummary
	ContextEconomy      ContextEconomySummary
	ProviderUsage       ProviderUsageSummary
	Artifacts           []ArtifactSummary
	TerminalSessions    []TerminalSessionSummary
	Plan                *runtime.Plan
	Evidence            []runtime.PlanEvidence
	Subagents           []SubagentRun
	NextStepHint        string
}

// WorkspaceGitStatus captures the git state of the workspace.
type WorkspaceGitStatus struct {
	WorkspaceRoot string
	Available     bool
	Branch        string
	Clean         bool
	Error         string
	Entries       []workspace.GitStatusEntry
}

// SubagentRun represents a child run spawned by a parent run.
type SubagentRun struct {
	SubRunID          string
	ParentRunID       string
	SessionID         string
	Depth             int
	Task              string
	ChildRunMode      string
	WorkspaceMode     string
	WorktreePath      string
	ContextMessages   int
	OrchestrationMode string
	ParentStepID      string
	State             string
	FinalStatus       string
	AcceptanceStatus  string
	AcceptanceReasons []string
	EvidenceRefs      []string
	Summary           string
	UpdatedAt         time.Time
}

// MutationCheckpointSummary represents a workspace mutation checkpoint.
type MutationCheckpointSummary struct {
	CheckpointID     string    `json:"checkpoint_id"`
	ToolResultRef    string    `json:"tool_result_ref"`
	ToolName         string    `json:"tool_name"`
	Status           string    `json:"status"`
	Paths            []string  `json:"paths,omitempty"`
	Summary          string    `json:"summary,omitempty"`
	VerifiedDiffStat string    `json:"verified_diff_stat,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// RollbackSummary represents the result of a workspace rollback.
type RollbackSummary struct {
	RollbackID    string    `json:"rollback_id"`
	CheckpointID  string    `json:"checkpoint_id,omitempty"`
	ToolResultRef string    `json:"tool_result_ref"`
	ToolName      string    `json:"tool_name"`
	Status        string    `json:"status"`
	RestoredPaths []string  `json:"restored_paths,omitempty"`
	ConflictPaths []string  `json:"conflict_paths,omitempty"`
	Summary       string    `json:"summary,omitempty"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ContextEconomySummary aggregates context-window usage metrics.
type ContextEconomySummary struct {
	LatestPressure          *ContextPressureSummary
	LatestCompression       *ContextCompressionSummary
	ToolResults             []ContextToolResultSummary
	ToolResultCount         int
	ElidedToolResultCount   int
	ToolResultTokenEstimate int
	MemoryRefs              []string
	ProcedureRefs           []string
}

// ContextPressureSummary captures a single context-pressure measurement.
type ContextPressureSummary struct {
	State                 string
	EstimatedInputTokens  int
	EffectiveWindowTokens int
	PercentUsed           int
}

// ContextCompressionSummary captures a single compression event.
type ContextCompressionSummary struct {
	BoundaryID   string
	TokensBefore int
	TokensAfter  int
	Summary      string
}

// ContextToolResultSummary captures tool-result accounting in context economy.
type ContextToolResultSummary struct {
	ResultRef     string
	ToolName      string
	Status        string
	Preview       string
	TokenEstimate int
	FullTextBytes int
	Elided        bool
	EvidenceRefs  []string
}

// ProviderUsageSummary aggregates LLM provider token usage.
type ProviderUsageSummary struct {
	CallCount        int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	Records          []ProviderUsageCallSummary
}

// ProviderUsageCallSummary is a single provider call record.
type ProviderUsageCallSummary struct {
	UsageID          string
	CallSite         string
	ProviderName     string
	ModelName        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
	CreatedAt        time.Time
}

// ArtifactSummary represents a stored artifact.
type ArtifactSummary struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                string
	Title               string
	MIMEType            string
	SizeBytes           int64
	SHA256              string
	CreatedAt           time.Time
}

// TerminalSessionSummary represents a terminal session.
type TerminalSessionSummary struct {
	TerminalSessionID string
	RunID             string
	SessionID         string
	Label             string
	CommandJSON       string
	Cwd               string
	Interactive       bool
	PTY               bool
	Status            string
	ProcessID         *int
	ProcessGroupID    *int
	ExitCode          *int
	Signal            string
	StdoutArtifactID  string
	StderrArtifactID  string
	PTYArtifactID     string
	StartedAt         *time.Time
	EndedAt           *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Logs              []TerminalSessionLogSummary
}

// TerminalSessionLogSummary represents a single terminal log entry.
type TerminalSessionLogSummary struct {
	LogID             string
	TerminalSessionID string
	Stream            string
	ArtifactID        string
	StartOffset       int64
	SizeBytes         int64
	CreatedAt         time.Time
}
