package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workspace"
)

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
	if typed, ok := store.(runtimePlanPersistenceStore); ok {
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
	workbench.State = runtimeapi.DeriveSessionState(latestRun, false)

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
