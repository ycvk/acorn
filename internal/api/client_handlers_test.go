package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mem "github.com/ycvk/acorn/internal/memory"

	"github.com/ycvk/acorn/internal/skills"
)

func TestThreadMessageRunHandlers(t *testing.T) {
	service := &clientHandlerStub{
		thread: Thread{
			ID:            "thread_1",
			Title:         "Inspect repo",
			WorkspaceRoot: "/repo",
			CreatedAt:     time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 5, 2, 10, 1, 0, 0, time.UTC),
			State:         "new",
		},
		message: Message{
			ID:       "7",
			ThreadID: "thread_1",
			Role:     "user",
			Content: MessageContent{
				Type: "text",
				Text: "look around",
			},
			CreatedAt: time.Date(2026, 5, 2, 10, 2, 0, 0, time.UTC),
		},
		run: Run{
			ID:        "run_1",
			ThreadID:  "thread_1",
			Status:    "running",
			Mode:      "direct",
			CreatedAt: time.Date(2026, 5, 2, 10, 3, 0, 0, time.UTC),
		},
	}
	router := newClientHandlerTestRouter(service)

	listThreads := performClientRequest(router, http.MethodGet, "/v1/threads", "")
	if listThreads.Code != http.StatusOK {
		t.Fatalf("list threads status = %d body=%s", listThreads.Code, listThreads.Body.String())
	}
	var threads ThreadListResponse
	decodeClientTestJSON(t, listThreads, &threads)
	if len(threads.Items) != 1 || threads.Items[0].ID != "thread_1" {
		t.Fatalf("unexpected threads response: %#v", threads)
	}

	createThread := performClientRequest(router, http.MethodPost, "/v1/threads", `{"title":" Inspect repo "}`)
	if createThread.Code != http.StatusCreated {
		t.Fatalf("create thread status = %d body=%s", createThread.Code, createThread.Body.String())
	}
	var thread ThreadDTO
	decodeClientTestJSON(t, createThread, &thread)
	if thread.ID != "thread_1" || thread.Title != "Inspect repo" || thread.WorkspaceRoot != "/repo" || thread.State != "new" {
		t.Fatalf("unexpected create thread response: %#v", thread)
	}
	if strings.Contains(createThread.Body.String(), "session_id") {
		t.Fatalf("thread response leaked session_id: %s", createThread.Body.String())
	}
	if service.createThreadTitle != "Inspect repo" {
		t.Fatalf("create thread title = %q, want trimmed title", service.createThreadTitle)
	}

	getThread := performClientRequest(router, http.MethodGet, "/v1/threads/thread_1", "")
	if getThread.Code != http.StatusOK {
		t.Fatalf("get thread status = %d body=%s", getThread.Code, getThread.Body.String())
	}

	updateThread := performClientRequest(router, http.MethodPatch, "/v1/threads/thread_1", `{"title":" New title "}`)
	if updateThread.Code != http.StatusOK {
		t.Fatalf("update thread status = %d body=%s", updateThread.Code, updateThread.Body.String())
	}
	if service.updateThreadID != "thread_1" || service.updateThreadTitle != "New title" {
		t.Fatalf("unexpected update request: id=%q title=%q", service.updateThreadID, service.updateThreadTitle)
	}

	deleteThread := performClientRequest(router, http.MethodDelete, "/v1/threads/thread_1", "")
	if deleteThread.Code != http.StatusNoContent {
		t.Fatalf("delete thread status = %d body=%s", deleteThread.Code, deleteThread.Body.String())
	}
	if service.deleteThreadID != "thread_1" {
		t.Fatalf("delete thread id = %q, want thread_1", service.deleteThreadID)
	}

	createMessage := performClientRequest(router, http.MethodPost, "/v1/threads/thread_1/messages", `{"content":{"type":"text","text":" look around "}}`)
	if createMessage.Code != http.StatusCreated {
		t.Fatalf("create message status = %d body=%s", createMessage.Code, createMessage.Body.String())
	}
	var message MessageDTO
	decodeClientTestJSON(t, createMessage, &message)
	if message.ID != "7" || message.ThreadID != "thread_1" || message.Content.Type != "text" || message.Content.Text != "look around" {
		t.Fatalf("unexpected create message response: %#v", message)
	}
	if service.createMessageThreadID != "thread_1" || service.createMessageContent != "look around" {
		t.Fatalf("unexpected create message call: thread=%q content=%q", service.createMessageThreadID, service.createMessageContent)
	}

	listMessages := performClientRequest(router, http.MethodGet, "/v1/threads/thread_1/messages", "")
	if listMessages.Code != http.StatusOK {
		t.Fatalf("list messages status = %d body=%s", listMessages.Code, listMessages.Body.String())
	}
	var messages MessageListResponse
	decodeClientTestJSON(t, listMessages, &messages)
	if len(messages.Items) != 1 || messages.Items[0].ID != "7" {
		t.Fatalf("unexpected messages response: %#v", messages)
	}

	createRun := performClientRequest(router, http.MethodPost, "/v1/threads/thread_1/runs", `{"skill_id":" skill.inspect ","mode":" plan_execute "}`)
	if createRun.Code != http.StatusCreated {
		t.Fatalf("create run status = %d body=%s", createRun.Code, createRun.Body.String())
	}
	var run RunDTO
	decodeClientTestJSON(t, createRun, &run)
	if run.ID != "run_1" || run.ThreadID != "thread_1" || run.Status != "running" || run.Mode != "direct" {
		t.Fatalf("unexpected create run response: %#v", run)
	}
	if service.createRunThreadID != "thread_1" || service.createRunSkillID != "skill.inspect" {
		t.Fatalf("unexpected create run call: thread=%q skill=%q", service.createRunThreadID, service.createRunSkillID)
	}

	getRun := performClientRequest(router, http.MethodGet, "/v1/runs/run_1", "")
	if getRun.Code != http.StatusOK {
		t.Fatalf("get run status = %d body=%s", getRun.Code, getRun.Body.String())
	}
}

func TestDecidePendingActionHandler(t *testing.T) {
	service := &pendingActionHandlerStub{
		record: core.PendingActionRecord{
			ActionID:     "action_1",
			RunID:        "run_1",
			Status:       core.PendingActionStatusApproved,
			DecisionJSON: `{"action":"accept"}`,
		},
	}
	server := &Server{
		pendingAction: service,
		deviceAuth:    &deviceAuthHandlerStub{},
		logger:        slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodPost, "/v1/pending-actions/action_1:decide", `{"decision":"accept"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if service.actionID != "action_1" || service.decision.Decision != "accept" {
		t.Fatalf("unexpected decide call: action=%q decision=%#v", service.actionID, service.decision)
	}
	var response PendingActionDecisionDTO
	decodeClientTestJSON(t, rec, &response)
	if response.ActionID != "action_1" || response.RunID != "run_1" || response.Status != "approved" || response.Decision != "accept" {
		t.Fatalf("unexpected decide response: %#v", response)
	}
}

func TestPendingActionListAndDetailHandlers(t *testing.T) {
	service := &pendingActionHandlerStub{
		summaries: []PendingActionSummary{{
			ActionID: "action_1",
			RunID:    "run_1",
			ThreadID: "thread_1",
			Kind:     "elicitation",
			Status:   "pending",
			Title:    "Approval required",
			Body:     "Allow Acorn to continue?",
			Options: []PendingActionOption{
				{ID: "accept", Label: "Accept"},
				{ID: "decline", Label: "Decline"},
			},
			CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		}},
		detail: &PendingActionDetail{
			PendingActionSummary: PendingActionSummary{
				ActionID: "action_1",
				RunID:    "run_1",
				ThreadID: "thread_1",
				Kind:     "elicitation",
				Status:   "pending",
				Title:    "Approval required",
				Body:     "Allow Acorn to continue?",
				Options: []PendingActionOption{
					{ID: "accept", Label: "Accept"},
					{ID: "decline", Label: "Decline"},
				},
				CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			},
			Payload: map[string]any{"message": "Allow Acorn to continue?"},
			Reason:  "needs owner approval",
			Rule:    "mobile_control",
		},
	}
	server := &Server{
		pendingAction: service,
		deviceAuth:    &deviceAuthHandlerStub{},
		logger:        slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	list := performClientRequest(router, http.MethodGet, "/v1/pending-actions?limit=5", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	if service.listLimit != 5 {
		t.Fatalf("list limit = %d, want 5", service.listLimit)
	}
	var listResponse PendingActionListResponse
	decodeClientTestJSON(t, list, &listResponse)
	if len(listResponse.Items) != 1 || listResponse.Items[0].ActionID != "action_1" {
		t.Fatalf("unexpected list response: %#v", listResponse)
	}

	detail := performClientRequest(router, http.MethodGet, "/v1/pending-actions/action_1", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", detail.Code, detail.Body.String())
	}
	var detailResponse PendingActionDetailDTO
	decodeClientTestJSON(t, detail, &detailResponse)
	if detailResponse.ActionID != "action_1" || detailResponse.Payload["message"] != "Allow Acorn to continue?" || detailResponse.Reason != "needs owner approval" {
		t.Fatalf("unexpected detail response: %#v", detailResponse)
	}
	if service.getActionID != "action_1" {
		t.Fatalf("get action id = %q, want action_1", service.getActionID)
	}
}

func TestDecidePendingActionHandlerRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMessage string
	}{
		{
			name:        "missing body",
			body:        "",
			wantMessage: "request body is required",
		},
		{
			name:        "missing decision",
			body:        `{}`,
			wantMessage: "decision is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &pendingActionHandlerStub{}
			server := &Server{
				pendingAction: service,
				deviceAuth:    &deviceAuthHandlerStub{},
				logger:        slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
			}
			router := chi.NewRouter()
			server.registerRoutes(router)

			rec := performClientRequest(router, http.MethodPost, "/v1/pending-actions/action_1:decide", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var response ErrorResponse
			decodeClientTestJSON(t, rec, &response)
			if response.Error.Code != "invalid_request" {
				t.Fatalf("code = %q, want invalid_request", response.Error.Code)
			}
			if response.Error.Message != tt.wantMessage {
				t.Fatalf("message = %q, want %q", response.Error.Message, tt.wantMessage)
			}
			if service.actionID != "" || service.decision.Decision != "" {
				t.Fatalf("service should not be called for invalid input: action=%q decision=%#v", service.actionID, service.decision)
			}
		})
	}
}

func TestDecidePendingActionHandlerMapsInvalidDecision(t *testing.T) {
	service := &pendingActionHandlerStub{err: ErrPendingActionDecisionInvalid}
	server := &Server{
		pendingAction: service,
		deviceAuth:    &deviceAuthHandlerStub{},
		logger:        slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodPost, "/v1/pending-actions/action_1:decide", `{"decision":"maybe"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	decodeClientTestJSON(t, rec, &response)
	if response.Error.Code != "invalid_request" || response.Error.Message != "pending action decision invalid" {
		t.Fatalf("unexpected error response: %#v", response.Error)
	}
	if service.actionID != "action_1" || service.decision.Decision != "maybe" {
		t.Fatalf("unexpected service call: action=%q decision=%#v", service.actionID, service.decision)
	}
}

func TestDeviceAuthProtectsV1Routes(t *testing.T) {
	server := &Server{
		threads:    &clientHandlerStub{},
		runs:       &clientHandlerStub{},
		events:     &clientHandlerStub{},
		deviceAuth: &deviceAuthHandlerStub{},
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequestWithoutAuth(router, http.MethodGet, "/v1/threads", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	decodeClientTestJSON(t, rec, &response)
	if response.Error.Code != "unauthenticated" {
		t.Fatalf("missing auth code = %q, want unauthenticated", response.Error.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/threads", nil)
	req.Header.Set("Authorization", "Token nope")
	malformed := httptest.NewRecorder()
	router.ServeHTTP(malformed, req)
	if malformed.Code != http.StatusUnauthorized {
		t.Fatalf("malformed auth status = %d body=%s", malformed.Code, malformed.Body.String())
	}
}

func TestDeviceAuthPairListAndRevokeHandlers(t *testing.T) {
	auth := &deviceAuthHandlerStub{
		pairResult: &PairDeviceResult{
			Device: DeviceView{
				DeviceID:   "device_1",
				Name:       "iPhone",
				Platform:   "ios",
				CreatedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
				LastSeenAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			},
			AccessToken: "acorn_dev_token",
		},
		devices: []DeviceView{{
			DeviceID:   "device_1",
			Name:       "iPhone",
			Platform:   "ios",
			CreatedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			LastSeenAt: time.Date(2026, 5, 15, 10, 1, 0, 0, time.UTC),
		}},
	}
	server := &Server{
		deviceAuth: auth,
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	pair := performClientRequestWithoutAuth(router, http.MethodPost, "/v1/devices:pair", `{"pairing_code":"ABCD-EFGH-IJKL-MNOP","device_name":"iPhone","platform":"ios"}`)
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status = %d body=%s", pair.Code, pair.Body.String())
	}
	var pairResponse PairDeviceResponse
	decodeClientTestJSON(t, pair, &pairResponse)
	if pairResponse.AccessToken != "acorn_dev_token" || pairResponse.Device.DeviceID != "device_1" {
		t.Fatalf("unexpected pair response: %#v", pairResponse)
	}
	if auth.pairInput.PairingCode != "ABCD-EFGH-IJKL-MNOP" || auth.pairInput.DeviceName != "iPhone" || auth.pairInput.Platform != "ios" {
		t.Fatalf("unexpected pair input: %#v", auth.pairInput)
	}

	list := performClientRequest(router, http.MethodGet, "/v1/devices", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var listResponse DeviceListResponse
	decodeClientTestJSON(t, list, &listResponse)
	if len(listResponse.Items) != 1 || listResponse.Items[0].DeviceID != "device_1" {
		t.Fatalf("unexpected list response: %#v", listResponse)
	}
	if auth.lastToken != "test-token" {
		t.Fatalf("auth token = %q, want test-token", auth.lastToken)
	}

	revoke := performClientRequest(router, http.MethodDelete, "/v1/devices/device_1", "")
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d body=%s", revoke.Code, revoke.Body.String())
	}
	if auth.revokedDeviceID != "device_1" {
		t.Fatalf("revoked device id = %q, want device_1", auth.revokedDeviceID)
	}
}

func TestDeviceAuthRevokedTokenFailsProtectedRoutes(t *testing.T) {
	server := &Server{
		threads:    &clientHandlerStub{},
		runs:       &clientHandlerStub{},
		events:     &clientHandlerStub{},
		deviceAuth: &deviceAuthHandlerStub{authErr: ErrDeviceRevoked},
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodGet, "/v1/threads", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("revoked auth status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response ErrorResponse
	decodeClientTestJSON(t, rec, &response)
	if response.Error.Code != "device_revoked" {
		t.Fatalf("revoked auth code = %q, want device_revoked", response.Error.Code)
	}
}

func TestClientHandlersReturnClientErrorCodes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "unknown thread",
			method:     http.MethodGet,
			path:       "/v1/threads/missing",
			err:        core.ErrSessionNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "thread_not_found",
		},
		{
			name:       "unknown run",
			method:     http.MethodGet,
			path:       "/v1/runs/missing",
			err:        core.ErrRunNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "run_not_found",
		},
		{
			name:       "execution not ready",
			method:     http.MethodPost,
			path:       "/v1/threads/thread_1/runs",
			body:       `{}`,
			err:        core.ErrExecutionNotReady,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "execution_not_ready",
		},
		{
			name:       "no pending message",
			method:     http.MethodPost,
			path:       "/v1/threads/thread_1/runs",
			body:       `{}`,
			err:        ErrClientNoPendingMessage,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &clientHandlerStub{err: tt.err}
			router := newClientHandlerTestRouter(service)
			rec := performClientRequest(router, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var response ErrorResponse
			decodeClientTestJSON(t, rec, &response)
			if response.Error.Code != tt.wantCode {
				t.Fatalf("error code = %q, want %q body=%s", response.Error.Code, tt.wantCode, rec.Body.String())
			}
		})
	}
}

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
				EventID: "run_1:2\nbroken",
				RunID:   "run_1",
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

func TestClientResourceSurfaceHandlers(t *testing.T) {
	service := &clientHandlerStub{
		thread: Thread{
			ID:            "thread_1",
			Title:         "Inspect repo",
			WorkspaceRoot: "/repo",
			CreatedAt:     time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 5, 2, 10, 1, 0, 0, time.UTC),
			State:         "completed",
		},
		run: Run{
			ID:        "run_1",
			ThreadID:  "thread_1",
			Status:    "completed",
			Mode:      "direct",
			CreatedAt: time.Date(2026, 5, 2, 10, 3, 0, 0, time.UTC),
		},
		events: []core.RunEvent{
			{
				EventID: "run_1:1",
				RunID:   "run_1",
				Seq:     1,
				TS:      time.Date(2026, 5, 2, 10, 3, 0, 0, time.UTC),
				Type:    "run.started",
				Data:    core.RunStartedData{Input: "hello"},
			},
		},
		artifacts: []ArtifactSummary{{
			ArtifactID:          "artifact_report",
			RunID:               "run_1",
			SessionID:           "thread_1",
			SourceToolResultRef: "tool_result:run_1:call_1",
			Kind:                "markdown",
			Title:               "Report",
			MIMEType:            "text/markdown",
			SizeBytes:           42,
			SHA256:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt:           time.Date(2026, 5, 2, 10, 4, 0, 0, time.UTC),
		}},
	}
	capabilities := &clientCapabilityStub{snapshot: SystemCapabilities{
		RuntimeReadiness: &RuntimeReadiness{Status: RuntimeReadinessReady},
		ProviderReadiness: []ProviderReadinessSummary{{
			Scope:         "mcp",
			Provider:      "fixture",
			Status:        ProviderReadinessPassed,
			StartupStatus: "healthy",
			AuthStatus:    "env",
		}},
		Model: SystemModelCapabilities{Name: "gpt-test"},
		Summary: SystemCapabilitySummary{
			ToolCount:        1,
			EnabledToolCount: 1,
			SkillCount:       1,
		},
		Features: SystemFeatureCapabilities{InterruptResume: true, SessionHistory: true},
		Tools: []SystemToolCapability{{
			Name:        "run_command",
			Source:      "builtin",
			Kind:        "function",
			Category:    "workspace",
			Enabled:     true,
			HealthState: "ok",
			Risk:        "high",
		}},
	}}
	cfg := config.DefaultConfig()
	cfg.Tools.Workspace.RootDir = "/repo"
	cfg.Providers[0].Model = "gpt-test"
	cfg.Providers[0].ReasoningEffort = "high"
	cfg.Providers[0].APIKey = "redacted-test-key"
	cfg.Web.ListenAddr = "127.0.0.1:9999"
	memory := &clientMemoryStub{
		facts: []mem.Record{{
			Ref:        "facts/workspaces/acorn/repo.md#repo-root",
			Kind:       mem.KindFact,
			RelPath:    "facts/workspaces/acorn/repo.md",
			Title:      "Repo root",
			Status:     mem.StatusVerified,
			Scope:      "workspace:acorn",
			Tags:       []string{"repo"},
			Body:       "repo root is /repo",
			Created:    "2026-05-02T10:00:00Z",
			Updated:    "2026-05-02T10:00:00Z",
			SourceRun:  "run_1",
			SourceRefs: []string{"history/thread_1.md#summary"},
		}},
		skills: []mem.Record{{
			Ref:         "skills/learned/release-closeout.md#release-closeout",
			Kind:        mem.KindSkill,
			RelPath:     "skills/learned/release-closeout.md",
			Title:       "Release closeout",
			Status:      mem.StatusUnverified,
			Origin:      "agent_draft",
			TaskPattern: "release closeout",
			Tags:        []string{"release", "closeout"},
			Body:        "先验证再提交",
			Created:     "2026-05-02T10:00:00Z",
			Updated:     "2026-05-02T10:00:00Z",
			SourceRun:   "run_1",
			SourceRefs:  []string{"facts/workspaces/acorn/repo.md#repo-root"},
		}},
		history: []mem.Record{{
			Ref:       "history/thread_1.md",
			Kind:      mem.KindHistory,
			RelPath:   "history/thread_1.md",
			Title:     "thread_1",
			Status:    mem.StatusVerified,
			Body:      "history hit from previous run",
			Created:   "2026-05-02T10:00:00Z",
			Updated:   "2026-05-02T10:00:00Z",
			SourceRun: "run_1",
		}},
		search: []mem.SearchItem{{
			Ref:       "history/thread_1.md",
			Kind:      string(mem.KindHistory),
			Title:     "thread_1",
			Status:    string(mem.StatusVerified),
			Scope:     "workspace:acorn",
			Tags:      []string{"history"},
			Path:      "history/thread_1.md",
			Snippet:   "history hit from previous run",
			Score:     1,
			Created:   "2026-05-02T10:00:00Z",
			Updated:   "2026-05-02T10:00:00Z",
			SourceRun: "run_1",
		}},
	}
	server := &Server{
		threads:      service,
		runs:         service,
		events:       service,
		runResume:    &clientRunResumeStub{result: &RunResult{RunID: "run_1", Status: "interrupted"}},
		capabilities: capabilities,
		pendingAction: &pendingActionHandlerStub{
			summaries: []PendingActionSummary{{
				ActionID: "action_1",
				RunID:    "run_1",
				ThreadID: "thread_1",
				Kind:     "elicitation",
				Status:   "pending",
				Title:    "Approval required",
				Body:     "Allow Acorn to continue?",
				Options: []PendingActionOption{
					{ID: "accept", Label: "Accept"},
					{ID: "decline", Label: "Decline"},
				},
				CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			}},
			detail: &PendingActionDetail{
				PendingActionSummary: PendingActionSummary{
					ActionID: "action_1",
					RunID:    "run_1",
					ThreadID: "thread_1",
					Kind:     "elicitation",
					Status:   "pending",
					Title:    "Approval required",
					Body:     "Allow Acorn to continue?",
					Options: []PendingActionOption{
						{ID: "accept", Label: "Accept"},
						{ID: "decline", Label: "Decline"},
					},
					CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
				},
				Payload: map[string]any{"message": "Allow Acorn to continue?"},
			},
		},
		inbox: &inboxHandlerStub{item: &MobileInbox{
			PendingActions: []PendingActionSummary{{
				ActionID: "action_1",
				RunID:    "run_1",
				ThreadID: "thread_1",
				Kind:     "elicitation",
				Status:   "pending",
				Title:    "Approval required",
				Body:     "Allow Acorn to continue?",
				Options: []PendingActionOption{
					{ID: "accept", Label: "Accept"},
					{ID: "decline", Label: "Decline"},
				},
				CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			}},
			ActiveRuns: []RunSummary{{
				RunID:          "run_1",
				ThreadID:       "thread_1",
				ThreadTitle:    "Deploy Acorn",
				Status:         "running",
				Mode:           "plan_execute",
				Preview:        "Run the release workflow",
				LastEventLabel: "Run is running",
				AttentionLevel: "running",
				DurationMS:     60000,
				CreatedAt:      time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 5, 15, 10, 1, 0, 0, time.UTC),
			}},
			RecentTerminalRuns: []RunSummary{{
				RunID:          "run_terminal",
				ThreadID:       "thread_1",
				ThreadTitle:    "Release Acorn",
				Status:         "completed",
				Mode:           "direct",
				Preview:        "Release completed",
				LastEventLabel: "Run completed",
				AttentionLevel: "normal",
				DurationMS:     300000,
				CreatedAt:      time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
				UpdatedAt:      time.Date(2026, 5, 15, 9, 5, 0, 0, time.UTC),
			}},
			System: capabilities.snapshot,
		}},
		skills: &clientSkillStub{items: []skills.View{{
			Spec: skills.Spec{
				ID:      "skill.inspect",
				Name:    "Inspect",
				Version: "1.0.0",
				Source:  "local",
			},
			Eligible: true,
		}}},
		memory:     memory,
		deviceAuth: &deviceAuthHandlerStub{},
		cfg:        cfg,
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	for _, tc := range []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		want       string
	}{
		{name: "interrupt", method: http.MethodPost, path: "/v1/runs/run_1:interrupt", wantStatus: http.StatusAccepted, want: "interrupt_requested"},
		{name: "resume", method: http.MethodPost, path: "/v1/runs/run_1:resume", body: `{}`, wantStatus: http.StatusOK, want: `"run_id":"run_1"`},
		{name: "detail", method: http.MethodGet, path: "/v1/runs/run_1/detail", wantStatus: http.StatusOK, want: `"artifacts"`},
		{name: "inbox", method: http.MethodGet, path: "/v1/inbox", wantStatus: http.StatusOK, want: `"pending_actions":[{"action_id":"action_1"`},
		{name: "pending actions", method: http.MethodGet, path: "/v1/pending-actions", wantStatus: http.StatusOK, want: `"items":[{"action_id":"action_1"`},
		{name: "system status", method: http.MethodGet, path: "/v1/system/status", wantStatus: http.StatusOK, want: `"runtime_readiness":{"status":"ready"}`},
		{name: "tools", method: http.MethodGet, path: "/v1/tools", wantStatus: http.StatusOK, want: "run_command"},
		{name: "skills", method: http.MethodGet, path: "/v1/skills", wantStatus: http.StatusOK, want: "skill.inspect"},
		{name: "skill create removed", method: http.MethodPost, path: "/v1/skills", body: `{"id":"skill.inspect","name":"Inspect","instruction":"Use repo inspection."}`, wantStatus: http.StatusMethodNotAllowed},
		{name: "skill detail", method: http.MethodGet, path: "/v1/skills/skill.inspect", wantStatus: http.StatusOK, want: "Inspect"},
		{name: "skill patch removed", method: http.MethodPatch, path: "/v1/skills/skill.inspect", body: `{"content":"extra instruction"}`, wantStatus: http.StatusMethodNotAllowed},
		{name: "skill delete removed", method: http.MethodDelete, path: "/v1/skills/skill.inspect", wantStatus: http.StatusMethodNotAllowed},
		{name: "skill files", method: http.MethodGet, path: "/v1/skills/skill.inspect/files", wantStatus: http.StatusOK, want: "SKILL.md"},
		{name: "core memory removed", method: http.MethodGet, path: "/v1/memory/core", wantStatus: http.StatusNotFound},
		{name: "core memory update removed", method: http.MethodPatch, path: "/v1/memory/core/core.about_you", body: `{"body":"updated core"}`, wantStatus: http.StatusNotFound},
		{name: "profile blocks deleted", method: http.MethodGet, path: "/v1/memory/profile-blocks", wantStatus: http.StatusNotFound},
		{name: "memory facts", method: http.MethodGet, path: "/v1/memory/facts?limit=5&include_inactive=true", wantStatus: http.StatusOK, want: `"title":"Repo root"`},
		{name: "memory skills", method: http.MethodGet, path: "/v1/memory/skills?limit=5&include_retired=true", wantStatus: http.StatusOK, want: `"origin":"agent_draft"`},
		{name: "memory history", method: http.MethodGet, path: "/v1/memory/history?limit=5", wantStatus: http.StatusOK, want: "history hit"},
		{name: "memory search", method: http.MethodGet, path: "/v1/memory/search?query=repo&kind=history&scope=workspace:acorn&include_inactive=true&include_retired=true", wantStatus: http.StatusOK, want: `"snippet":"history hit from previous run"`},
		{name: "memory invalid include flag", method: http.MethodGet, path: "/v1/memory/facts?include_inactive=yes", wantStatus: http.StatusBadRequest, want: "include_inactive must be true or false"},
		{name: "add memory fact removed", method: http.MethodPost, path: "/v1/memory/facts", body: `{"content":"repo root is /repo","labels":["repo"]}`, wantStatus: http.StatusMethodNotAllowed},
		{name: "delete memory fact removed", method: http.MethodDelete, path: "/v1/memory/facts/42", wantStatus: http.StatusNotFound},
		{name: "episodic memory removed", method: http.MethodGet, path: "/v1/memory/episodes", wantStatus: http.StatusNotFound},
		{name: "memory candidates removed", method: http.MethodGet, path: "/v1/memory/candidates?status=pending", wantStatus: http.StatusNotFound},
		{name: "memory candidate update removed", method: http.MethodPatch, path: "/v1/memory/candidates/memcand_1", body: `{"payload_json":"{\"content\":\"edited\"}","reason":"edited reason","scope":{"type":"workspace","key":"acorn"}}`, wantStatus: http.StatusNotFound},
		{name: "memory candidate delete removed", method: http.MethodDelete, path: "/v1/memory/candidates/memcand_1", wantStatus: http.StatusNotFound},
		{name: "history search removed", method: http.MethodGet, path: "/v1/history/search?query=repo", wantStatus: http.StatusNotFound},
		{name: "codeintel status removed", method: http.MethodGet, path: "/v1/codeintel/status", wantStatus: http.StatusNotFound},
		{name: "codeintel repo map removed", method: http.MethodGet, path: "/v1/codeintel/repo-map?query=routes", wantStatus: http.StatusNotFound},
		{name: "codeintel symbols removed", method: http.MethodGet, path: "/v1/codeintel/symbols?query=registerRoutes", wantStatus: http.StatusNotFound},
		{name: "codeintel file symbols removed", method: http.MethodGet, path: "/v1/codeintel/file-symbols?path=internal/web/routes.go", wantStatus: http.StatusNotFound},
		{name: "codeintel references removed", method: http.MethodGet, path: "/v1/codeintel/references?symbol=registerRoutes", wantStatus: http.StatusNotFound},
		{name: "reflection list removed", method: http.MethodGet, path: "/v1/reflections", wantStatus: http.StatusNotFound},
		{name: "reflection findings removed", method: http.MethodGet, path: "/v1/reflections/findings", wantStatus: http.StatusNotFound},
		{name: "reflection approve removed", method: http.MethodPost, path: "/v1/reflections/7:approve", body: `{}`, wantStatus: http.StatusNotFound},
		{name: "reflection reject removed", method: http.MethodPost, path: "/v1/reflections/7:reject", body: `{}`, wantStatus: http.StatusNotFound},
		{name: "reflection rollback removed", method: http.MethodPost, path: "/v1/reflections/7:rollback", wantStatus: http.StatusNotFound},
		{name: "settings", method: http.MethodGet, path: "/v1/settings", wantStatus: http.StatusOK, want: "gpt-test"},
		{name: "settings patch unsupported", method: http.MethodPatch, path: "/v1/settings", body: `{}`, wantStatus: http.StatusNotImplemented, want: "settings_write_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := performClientRequest(router, tc.method, tc.path, tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if tc.want != "" && !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body missing %q: %s", tc.want, rec.Body.String())
			}
		})
	}

	detailRec := performClientRequest(router, http.MethodGet, "/v1/runs/run_1/detail", "")
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", detailRec.Code, detailRec.Body.String())
	}
	if strings.Contains(detailRec.Body.String(), `"raw"`) {
		t.Fatalf("run detail should not expose raw diagnostic events: %s", detailRec.Body.String())
	}
	if strings.Contains(detailRec.Body.String(), `"workbench"`) {
		t.Fatalf("run detail should not expose runtime workbench: %s", detailRec.Body.String())
	}
	if !strings.Contains(detailRec.Body.String(), `"artifacts":[{"artifact_id":"artifact_report"`) {
		t.Fatalf("run detail should expose top-level artifacts: %s", detailRec.Body.String())
	}

	systemStatusRec := performClientRequest(router, http.MethodGet, "/v1/system/status", "")
	if systemStatusRec.Code != http.StatusOK {
		t.Fatalf("system status code = %d body=%s", systemStatusRec.Code, systemStatusRec.Body.String())
	}
	if !strings.Contains(systemStatusRec.Body.String(), `"provider_readiness":[{"scope":"mcp","provider":"fixture","status":"passed"`) {
		t.Fatalf("system status should include provider readiness, got %s", systemStatusRec.Body.String())
	}
	if !memory.factSelection.IncludeInactive || memory.factSelection.IncludeRetired {
		t.Fatalf("fact selection = %#v, want include_inactive only", memory.factSelection)
	}
	if memory.skillSelection.IncludeInactive || !memory.skillSelection.IncludeRetired {
		t.Fatalf("skill selection = %#v, want include_retired only", memory.skillSelection)
	}
	if memory.historySelection.IncludeInactive || memory.historySelection.IncludeRetired {
		t.Fatalf("history selection = %#v, want active only", memory.historySelection)
	}
	if memory.searchReq.Query != "repo" || memory.searchReq.Scope != "workspace:acorn" || !memory.searchReq.IncludeInactive || !memory.searchReq.IncludeRetired {
		t.Fatalf("search request = %#v, want repo scoped retired-inclusive search", memory.searchReq)
	}
	if len(memory.searchReq.Kinds) != 1 || memory.searchReq.Kinds[0] != mem.KindHistory {
		t.Fatalf("search kinds = %#v, want history", memory.searchReq.Kinds)
	}

	settingsRec := performClientRequest(router, http.MethodGet, "/v1/settings", "")
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("settings status = %d body=%s", settingsRec.Code, settingsRec.Body.String())
	}
	var settings ClientSettingsDTO
	decodeClientTestJSON(t, settingsRec, &settings)
	if got, want := len(settings.Providers), 1; got != want {
		t.Fatalf("len(settings.Providers) = %d, want %d", got, want)
	}
	if got, want := settings.Providers[0].ReasoningEffort, "high"; got != want {
		t.Fatalf("settings.providers[0].reasoning_effort = %q, want %q", got, want)
	}
}

func TestLegacyRouteGroupIsNotMounted(t *testing.T) {
	server := &Server{
		threads: &clientHandlerStub{},
		runs:    &clientHandlerStub{},
		events:  &clientHandlerStub{},
		skills:  &clientSkillStub{},
		memory:  &clientMemoryStub{},
		logger:  slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	legacyPrefix := "/" + "api"
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/system/capabilities"},
		{http.MethodGet, "/sessions"},
		{http.MethodPost, "/sessions"},
		{http.MethodGet, "/sessions/last-pending"},
		{http.MethodGet, "/sessions/thread_1"},
		{http.MethodDelete, "/sessions/thread_1"},
		{http.MethodGet, "/sessions/thread_1/messages"},
		{http.MethodPost, "/sessions/thread_1/messages"},
		{http.MethodGet, "/sessions/thread_1/workbench"},
		{http.MethodGet, "/sessions/thread_1/plan"},
		{http.MethodGet, "/runs/run_1/trace"},
		{http.MethodGet, "/runs/run_1/plan"},
		{http.MethodPost, "/runs/run_1/resume"},
		{http.MethodPost, "/runs/run_1/interrupt"},
		{http.MethodGet, "/codeintel/status"},
		{http.MethodGet, "/memory/profile-blocks"},
		{http.MethodGet, "/reflections"},
		{http.MethodGet, "/skills"},
		{http.MethodGet, "/sessions/thread_1/working-checkpoint"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := performClientRequest(router, tc.method, legacyPrefix+tc.path, `{}`)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func newClientHandlerTestRouter(service *clientHandlerStub) http.Handler {
	server := &Server{
		threads:    service,
		runs:       service,
		events:     service,
		deviceAuth: &deviceAuthHandlerStub{},
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)
	return router
}

func performClientRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return performClientRequestWithAuth(handler, method, path, body, "test-token")
}

func performClientRequestWithoutAuth(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return performClientRequestWithAuth(handler, method, path, body, "")
}

func performClientRequestWithAuth(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeClientTestJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode JSON: %v body=%s", err, rec.Body.String())
	}
}

type deviceAuthHandlerStub struct {
	authErr         error
	pairErr         error
	listErr         error
	revokeErr       error
	lastToken       string
	pairInput       PairDeviceInput
	pairResult      *PairDeviceResult
	devices         []DeviceView
	revokedDeviceID string
}

func (s *deviceAuthHandlerStub) PairDevice(_ context.Context, input PairDeviceInput) (*PairDeviceResult, error) {
	if s.pairErr != nil {
		return nil, s.pairErr
	}
	s.pairInput = input
	if s.pairResult != nil {
		return s.pairResult, nil
	}
	return &PairDeviceResult{
		Device: DeviceView{
			DeviceID:   "device_test",
			Name:       "Test device",
			Platform:   "test",
			CreatedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			LastSeenAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		},
		AccessToken: "acorn_dev_test",
	}, nil
}

func (s *deviceAuthHandlerStub) Authenticate(_ context.Context, token string) (*DeviceAuthContext, error) {
	if s.authErr != nil {
		return nil, s.authErr
	}
	s.lastToken = token
	return &DeviceAuthContext{Device: DeviceView{
		DeviceID:   "device_test",
		Name:       "Test device",
		Platform:   "test",
		CreatedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		LastSeenAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}}, nil
}

func (s *deviceAuthHandlerStub) ListDevices(context.Context) ([]DeviceView, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.devices, nil
}

func (s *deviceAuthHandlerStub) RevokeDevice(_ context.Context, deviceID string) error {
	if s.revokeErr != nil {
		return s.revokeErr
	}
	s.revokedDeviceID = deviceID
	return nil
}

type clientHandlerStub struct {
	thread    Thread
	message   Message
	run       Run
	events    []core.RunEvent
	artifacts []ArtifactSummary
	err       error

	eventBatches              []*core.RunEventBatch
	loadEventCalls            int
	lastAfterSeq              int64
	statusChecks              int
	terminalAfterStatusChecks int
	createThreadTitle         string
	updateThreadID            string
	updateThreadTitle         string
	deleteThreadID            string
	createMessageThreadID     string
	createMessageContent      string
	createRunThreadID         string
	createRunSkillID          string
}

func (s *clientHandlerStub) ListThreads(context.Context, int) ([]Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []Thread{s.thread}, nil
}

func (s *clientHandlerStub) CreateThread(_ context.Context, title string) (*Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.createThreadTitle = title
	return &s.thread, nil
}

func (s *clientHandlerStub) GetThread(context.Context, string) (*Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &s.thread, nil
}

func (s *clientHandlerStub) UpdateThread(_ context.Context, threadID, title string) (*Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.updateThreadID = threadID
	s.updateThreadTitle = title
	return &s.thread, nil
}

func (s *clientHandlerStub) DeleteThread(_ context.Context, threadID string) error {
	if s.err != nil {
		return s.err
	}
	s.deleteThreadID = threadID
	return nil
}

func (s *clientHandlerStub) ListMessages(context.Context, string, int) ([]Message, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []Message{s.message}, nil
}

func (s *clientHandlerStub) CreateMessage(_ context.Context, threadID, content string) (*Message, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.createMessageThreadID = threadID
	s.createMessageContent = content
	return &s.message, nil
}

func (s *clientHandlerStub) CreateRun(_ context.Context, threadID, skillID, _ string) (*Run, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.createRunThreadID = threadID
	s.createRunSkillID = skillID
	return &s.run, nil
}

func (s *clientHandlerStub) GetRun(context.Context, string) (*Run, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &s.run, nil
}

func (s *clientHandlerStub) LoadRunEventsAfter(_ context.Context, _ string, afterSeq int64) (*core.RunEventBatch, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.lastAfterSeq = afterSeq
	s.loadEventCalls++
	if len(s.eventBatches) > 0 {
		batch := s.eventBatches[0]
		s.eventBatches = s.eventBatches[1:]
		if batch == nil {
			return &core.RunEventBatch{CursorSeq: afterSeq}, nil
		}
		return &core.RunEventBatch{
			Events:    append([]core.RunEvent(nil), batch.Events...),
			CursorSeq: batch.CursorSeq,
		}, nil
	}
	cursorSeq := afterSeq
	if len(s.events) > 0 {
		cursorSeq = s.events[len(s.events)-1].Seq
	}
	return &core.RunEventBatch{
		Events:    append([]core.RunEvent(nil), s.events...),
		CursorSeq: cursorSeq,
	}, nil
}

func (s *clientHandlerStub) LoadRunEventsForDetail(context.Context, string) (*core.RunEventDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &core.RunEventDetail{
		Events: append([]core.RunEvent(nil), s.events...),
	}, nil
}

func (s *clientHandlerStub) ListRunArtifacts(context.Context, string) ([]ArtifactSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]ArtifactSummary(nil), s.artifacts...), nil
}

func (s *clientHandlerStub) RunIsTerminal(context.Context, string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	s.statusChecks++
	if s.terminalAfterStatusChecks > 0 {
		return s.statusChecks >= s.terminalAfterStatusChecks, nil
	}
	return true, nil
}

func (s *clientHandlerStub) InterruptRun(context.Context, string) error {
	if s.err != nil {
		return s.err
	}
	return nil
}

func (s *clientHandlerStub) EventPollInterval() time.Duration {
	return time.Millisecond
}

var _ ThreadServiceAPI = (*clientHandlerStub)(nil)
var _ RunServiceAPI = (*clientHandlerStub)(nil)
var _ EventServiceAPI = (*clientHandlerStub)(nil)

type pendingActionHandlerStub struct {
	record      core.PendingActionRecord
	summaries   []PendingActionSummary
	detail      *PendingActionDetail
	err         error
	actionID    string
	getActionID string
	decision    PendingActionDecisionInput
	listLimit   int
}

func (s *pendingActionHandlerStub) List(_ context.Context, limit int) ([]PendingActionSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.listLimit = limit
	return append([]PendingActionSummary(nil), s.summaries...), nil
}

func (s *pendingActionHandlerStub) Get(_ context.Context, actionID string) (*PendingActionDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.getActionID = actionID
	return s.detail, nil
}

func (s *pendingActionHandlerStub) Decide(_ context.Context, actionID string, decision PendingActionDecisionInput) (*core.PendingActionRecord, error) {
	s.actionID = actionID
	s.decision = decision
	if s.err != nil {
		return nil, s.err
	}
	return &s.record, nil
}

var _ PendingActionServiceAPI = (*pendingActionHandlerStub)(nil)

type inboxHandlerStub struct {
	item *MobileInbox
	err  error
}

func (s *inboxHandlerStub) Load(context.Context) (*MobileInbox, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.item, nil
}

var _ InboxServiceAPI = (*inboxHandlerStub)(nil)

type clientRunResumeStub struct {
	result *RunResult
	err    error
}

func (s *clientRunResumeStub) Resume(context.Context, string) (*RunResult, error) {
	return s.result, s.err
}

type clientCapabilityStub struct {
	snapshot SystemCapabilities
}

func (s *clientCapabilityStub) Snapshot(context.Context, CapabilitySnapshotOptions) SystemCapabilities {
	return s.snapshot
}

type clientSkillStub struct {
	items []skills.View
}

func (s *clientSkillStub) Snapshot(context.Context) (*skills.Snapshot, error) {
	return nil, nil
}

func (s *clientSkillStub) List(context.Context, int) ([]skills.View, error) {
	return append([]skills.View(nil), s.items...), nil
}

func (s *clientSkillStub) ListFiltered(context.Context, SkillListFilter) ([]skills.View, int, error) {
	return append([]skills.View(nil), s.items...), len(s.items), nil
}

func (s *clientSkillStub) Get(context.Context, string) (*skills.View, error) {
	if len(s.items) == 0 {
		return nil, ErrSkillNotFound
	}
	return new(skills.CopyView(s.items[0])), nil
}

func (s *clientSkillStub) ReadFile(context.Context, string, string) (*SkillFileView, error) {
	return &SkillFileView{SkillID: "skill.inspect", Path: "SKILL.md", Content: "Use repo inspection."}, nil
}

type clientMemoryStub struct {
	facts            []mem.Record
	skills           []mem.Record
	history          []mem.Record
	search           []mem.SearchItem
	factSelection    mem.RecordSelection
	skillSelection   mem.RecordSelection
	historySelection mem.RecordSelection
	searchReq        mem.SearchRequest
}

func (s *clientMemoryStub) ListFacts(_ context.Context, selection mem.RecordSelection) ([]mem.Record, error) {
	s.factSelection = selection
	return append([]mem.Record(nil), s.facts...), nil
}

func (s *clientMemoryStub) ListSkills(_ context.Context, selection mem.RecordSelection) ([]mem.Record, error) {
	s.skillSelection = selection
	return append([]mem.Record(nil), s.skills...), nil
}

func (s *clientMemoryStub) ListHistory(_ context.Context, selection mem.RecordSelection) ([]mem.Record, error) {
	s.historySelection = selection
	return append([]mem.Record(nil), s.history...), nil
}

func (s *clientMemoryStub) Search(_ context.Context, req mem.SearchRequest) (*mem.SearchResult, error) {
	s.searchReq = req
	return &mem.SearchResult{Items: append([]mem.SearchItem(nil), s.search...)}, nil
}
