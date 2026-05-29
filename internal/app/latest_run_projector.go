package app

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

type latestRunProjectionStore interface {
	LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
	LoadRunDecision(ctx context.Context, runID string) (*decision.Record, error)
}

type latestRunProjection struct {
	LatestRunID     string
	LatestRunStatus events.RunStatus
	LatestRunMode   string
	LatestRunDepth  int
	ParentRunID     string
	State           runtimeapi.SessionState
	Resumable       bool
	ResumeReason    string
	InterruptIDs    []string
	RawEvents       []events.EventRecord
	TraceSummary    *runtime.TraceSummary
	SelectedSkill   *runtime.SelectedSkill
	LatestDecision  *decision.Record
}

func projectLatestRun(ctx context.Context, store latestRunProjectionStore, traceSvc *TraceService, latestRun *events.RunRecord) (latestRunProjection, error) {
	projection := latestRunProjection{
		State: runtimeapi.DeriveSessionState(latestRun, false),
	}
	if latestRun == nil {
		return projection, nil
	}

	projection.LatestRunID = latestRun.RunID
	projection.LatestRunStatus = latestRun.Status
	projection.LatestRunMode = string(latestRun.OrchestrationMode)
	projection.LatestRunDepth = latestRun.Depth
	projection.ParentRunID = latestRun.ParentRunID

	if latestRun.Status == events.RunStatusInterrupted {
		if traceSvc == nil {
			return latestRunProjection{}, fmt.Errorf("load resume status for run %s: trace service is nil", latestRun.RunID)
		}
		resumeStatus, err := traceSvc.ResumeStatus(ctx, latestRun.RunID)
		if err != nil {
			return latestRunProjection{}, fmt.Errorf("load resume status for run %s: %w", latestRun.RunID, err)
		}
		if resumeStatus == nil {
			return latestRunProjection{}, fmt.Errorf("load resume status for run %s: resume status is nil", latestRun.RunID)
		}
		projection.Resumable = resumeStatus.Resumable
		projection.ResumeReason = resumeStatus.Reason
		projection.InterruptIDs = append(projection.InterruptIDs, resumeStatus.InterruptIDs...)
	}
	if projection.ResumeReason == "" {
		projection.ResumeReason = defaultResumeReason(latestRun)
	}

	if store == nil {
		return latestRunProjection{}, fmt.Errorf("load events for run %s: app projection store is nil", latestRun.RunID)
	}
	rawEvents, err := store.LoadEvents(ctx, latestRun.RunID)
	if err != nil {
		return latestRunProjection{}, fmt.Errorf("load events for run %s: %w", latestRun.RunID, err)
	}
	projection.RawEvents = rawEvents
	if len(rawEvents) > 0 {
		projection.TraceSummary = runtime.BuildTraceSummary(rawEvents)
		projection.SelectedSkill = runtime.SelectedSkillFromEvents(rawEvents)
	}

	decisionRecord, err := store.LoadRunDecision(ctx, latestRun.RunID)
	if err != nil {
		return latestRunProjection{}, fmt.Errorf("load decision for run %s: %w", latestRun.RunID, err)
	}
	projection.LatestDecision = decisionRecord

	return projection, nil
}
