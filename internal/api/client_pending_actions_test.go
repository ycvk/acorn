package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

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
		pendingAction: newPendingActionTestService(service),
		deviceAuth:    newDeviceAuthTestService(&deviceAuthHandlerStub{}),
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
		pendingAction: newPendingActionTestService(service),
		deviceAuth:    newDeviceAuthTestService(&deviceAuthHandlerStub{}),
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
		{name: "missing body", body: "", wantMessage: "request body is required"},
		{name: "missing decision", body: `{}`, wantMessage: "decision is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &pendingActionHandlerStub{}
			server := &Server{
				pendingAction: newPendingActionTestService(service),
				deviceAuth:    newDeviceAuthTestService(&deviceAuthHandlerStub{}),
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
		pendingAction: newPendingActionTestService(service),
		deviceAuth:    newDeviceAuthTestService(&deviceAuthHandlerStub{}),
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
}
