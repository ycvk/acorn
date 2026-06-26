package api

import (
	"net/http"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

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
