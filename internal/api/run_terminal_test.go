package api

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

type runStatusStore struct {
	unimplementedStore
	status core.RunStatus
}

func (s runStatusStore) LoadRun(_ context.Context, runID string) (*core.RunRecord, error) {
	return &core.RunRecord{RunID: runID, Status: s.status}, nil
}

func TestInterruptedRunIsNotTerminal(t *testing.T) {
	for status, want := range map[core.RunStatus]bool{
		core.RunStatusRunning:     false,
		core.RunStatusInterrupted: false,
		core.RunStatusSucceeded:   true,
		core.RunStatusFailed:      true,
	} {
		runs := NewRunService(runStatusStore{status: status}, nil, nil, nil)
		got, err := runs.RunIsTerminal(context.Background(), "run_1")
		if err != nil || got != want {
			t.Fatalf("%s: terminal = %v err=%v, want %v", status, got, err, want)
		}
	}
}
