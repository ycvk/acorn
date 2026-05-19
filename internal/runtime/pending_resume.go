package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

type PendingResumeStore interface {
	FindLatestInterruptedRun(ctx context.Context) (*events.RunRecord, error)
}

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
