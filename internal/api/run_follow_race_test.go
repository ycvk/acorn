package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

// finishingRunStore finishes its run right after the first follow poll has
// read the events, the order the executor uses: it appends run.completed and
// then marks the run finished.
type finishingRunStore struct {
	core.SessionStore
	loads    int
	finished bool
}

func (s *finishingRunStore) LoadRun(_ context.Context, runID string) (*core.RunRecord, error) {
	status := core.RunStatusRunning
	if s.finished {
		status = core.RunStatusSucceeded
	}
	return &core.RunRecord{RunID: runID, Status: status}, nil
}

func (s *finishingRunStore) LoadEventsAfter(_ context.Context, runID string, afterSeq int64) ([]core.EventRecord, error) {
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	events := []core.EventRecord{{Sequence: 1, RunID: runID, Kind: "run.started", Payload: map[string]any{"input": "hi"}, CreatedAt: at}}
	if s.finished {
		events = append(events, core.EventRecord{Sequence: 2, RunID: runID, Kind: "run.completed", Payload: map[string]any{"message": map[string]any{"content": "done"}}, CreatedAt: at})
	}
	s.loads++
	if s.loads == 2 {
		s.finished = true
	}
	var out []core.EventRecord
	for _, e := range events {
		if e.Sequence > afterSeq {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestFollowSendsTheTerminalEventOfARunThatFinishesBetweenPolls(t *testing.T) {
	store := &finishingRunStore{}
	server := &Server{
		events:     NewEventService(store, nil),
		runs:       NewRunService(store, nil, nil, nil),
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodGet, "/v1/runs/run_1/events?after_seq=0&follow=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "event: run.completed\n") {
		t.Fatalf("the stream ended without run.completed:\n%s", body)
	}
}
