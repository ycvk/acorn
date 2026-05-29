package app

import (
	"context"
	"errors"
	"strings"

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

	latestRunProjection, err := projectLatestRun(ctx, s.store, s.trace, latestRun)
	if err != nil {
		return nil, err
	}

	workbench := &RuntimeWorkbench{
		SessionID: trimmedSessionID,
		Title:     session.Title,
		State:     latestRunProjection.State,
	}
	if latestRun != nil {
		workbench.LatestRunID = latestRunProjection.LatestRunID
		workbench.LatestRunStatus = latestRunProjection.LatestRunStatus
		workbench.LatestRunMode = latestRunProjection.LatestRunMode
		workbench.LatestRunDepth = latestRunProjection.LatestRunDepth
		workbench.ParentRunID = latestRunProjection.ParentRunID
		workbench.Resumable = latestRunProjection.Resumable
		workbench.ResumeReason = latestRunProjection.ResumeReason
		workbench.TraceSummary = latestRunProjection.TraceSummary
		workbench.SelectedSkill = latestRunProjection.SelectedSkill
		workbench.LatestDecision = latestRunProjection.LatestDecision
	}

	summary, summaryErr := s.store.GetSessionSummary(ctx, trimmedSessionID)
	if summaryErr != nil {
		return nil, summaryErr
	}
	workbench.SessionSummary = summary

	if latestRun != nil {
		rawEvents := latestRunProjection.RawEvents
		workbench.Subagents = buildSubagentRuns(rawEvents)

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
