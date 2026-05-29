package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

type PendingResumeInfo struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Input     string    `json:"input"`
	CreatedAt time.Time `json:"created_at"`
}

func FindPendingResume(ctx context.Context, store PendingResumeStore) (*PendingResumeInfo, error) {
	run, err := store.FindLatestInterruptedRun(ctx)
	if err != nil {
		return nil, fmt.Errorf("find latest interrupted run: %w", err)
	}
	if run == nil {
		return nil, nil
	}
	return &PendingResumeInfo{
		RunID:     run.RunID,
		SessionID: run.SessionID,
		Input:     run.Input,
		CreatedAt: run.CreatedAt,
	}, nil
}

func resolveRootOrchestrationMode(req runtimeapi.ExecuteRequest) events.OrchestrationMode {
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if req.OrchestrationMode != "" {
		return mode
	}
	if strings.TrimSpace(req.ParentRunID) != "" {
		return events.ModeSingleAgent
	}
	if strings.TrimSpace(req.SkillID) != "" {
		return events.ModePlanExecute
	}
	return events.ModeDirectResponse
}
