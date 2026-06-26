package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestRunEventsBacklogSSE(t *testing.T) {
	service := &clientHandlerStub{
		events: []core.RunEvent{
			{
				EventID: "run_1:2",
				RunID:   "run_1",
				Seq:     2,
				TS:      time.Date(2026, 5, 2, 10, 4, 0, 0, time.UTC),
				Type:    "assistant.delta",
				Data: core.AssistantDeltaData{
					AssistantDelta: map[string]any{"delta": "he"},
				},
			},
			{
				EventID: "run_1:3",
				RunID:   "run_1",
				Seq:     3,
				TS:      time.Date(2026, 5, 2, 10, 5, 0, 0, time.UTC),
				Type:    "run.completed",
				Data:    core.RunCompletedData{Message: map[string]any{"content": "done"}},
			},
		},
	}
	router := newClientHandlerTestRouter(service)
	rec := performClientRequest(router, http.MethodGet, "/v1/runs/run_1/events?after_seq=1&follow=false", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"id: run_1:2\n",
		"event: assistant.delta\n",
		`"event_id":"run_1:2"`,
		`"seq":2`,
		"id: run_1:3\n",
		"event: run.completed\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "run_1:1") {
		t.Fatalf("SSE body included filtered seq 1:\n%s", body)
	}
	if service.lastAfterSeq != 1 {
		t.Fatalf("after_seq passed to service = %d, want 1", service.lastAfterSeq)
	}
}

func TestRunEventsRejectInvalidSSEMetadata(t *testing.T) {
	service := &clientHandlerStub{
		events: []core.RunEvent{
			{
				EventID: "run_1:2",
				RunID:   "run_1\nbroken",
				Seq:     2,
				TS:      time.Date(2026, 5, 2, 10, 4, 0, 0, time.UTC),
				Type:    "assistant.delta",
				Data: core.AssistantDeltaData{
					AssistantDelta: map[string]any{"delta": "he"},
				},
			},
		},
	}
	router := newClientHandlerTestRouter(service)
	rec := performClientRequest(router, http.MethodGet, "/v1/runs/run_1/events?after_seq=0&follow=false", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q body=%s", got, rec.Body.String())
	}
	var response ErrorResponse
	decodeClientTestJSON(t, rec, &response)
	if response.Error.Code != "internal_error" {
		t.Fatalf("code = %q, want internal_error", response.Error.Code)
	}
}

func TestRunEventsFollowPollsUntilTerminal(t *testing.T) {
	service := &clientHandlerStub{
		eventBatches: []*core.RunEventBatch{
			{
				Events: []core.RunEvent{
					{
						EventID: "run_follow:1",
						RunID:   "run_follow",
						Seq:     1,
						TS:      time.Date(2026, 5, 2, 10, 4, 0, 0, time.UTC),
						Type:    "run.started",
						Data:    core.RunStartedData{Input: "hello"},
					},
				},
				CursorSeq: 1,
			},
			{
				Events: []core.RunEvent{
					{
						EventID: "run_follow:2",
						RunID:   "run_follow",
						Seq:     2,
						TS:      time.Date(2026, 5, 2, 10, 4, 1, 0, time.UTC),
						Type:    "run.completed",
						Data:    core.RunCompletedData{Message: map[string]any{"content": "done"}},
					},
				},
				CursorSeq: 2,
			},
			{CursorSeq: 3},
		},
		terminalAfterStatusChecks: 2,
	}
	router := newClientHandlerTestRouter(service)
	rec := performClientRequest(router, http.MethodGet, "/v1/runs/run_follow/events?after_seq=0&follow=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"id: run_follow:1\n", "id: run_follow:2\n", "event: run.completed\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("follow SSE body missing %q:\n%s", want, body)
		}
	}
	if service.loadEventCalls < 3 {
		t.Fatalf("load event calls = %d, want backlog + follow polls", service.loadEventCalls)
	}
}

func TestRunEventsReturnErrorsBeforeSSEStarts(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid cursor",
			path:       "/v1/runs/run_1/events?after_seq=-1",
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_after_seq",
		},
		{
			name:       "unknown run",
			path:       "/v1/runs/missing/events",
			err:        core.ErrRunNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "run_not_found",
		},
		{
			name:       "projection failure",
			path:       "/v1/runs/run_1/events",
			err:        ErrClientProjectionFailed,
			wantStatus: http.StatusInternalServerError,
			wantCode:   "run_event_projection_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &clientHandlerStub{err: tt.err}
			router := newClientHandlerTestRouter(service)
			rec := performClientRequest(router, http.MethodGet, tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q body=%s", got, rec.Body.String())
			}
			var response ErrorResponse
			decodeClientTestJSON(t, rec, &response)
			if response.Error.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q body=%s", response.Error.Code, tt.wantCode, rec.Body.String())
			}
		})
	}
}
