package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

type recordingPushTokens struct {
	deviceID, token string
}

func (r *recordingPushTokens) SetPushToken(_ context.Context, deviceID, token string) error {
	r.deviceID, r.token = deviceID, token
	return nil
}

func TestSetPushTokenStoresTokenForAuthenticatedDevice(t *testing.T) {
	tokens := &recordingPushTokens{}
	server := &Server{
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}).WithPushTokens(tokens),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	rec := performClientRequest(router, http.MethodPut, "/v1/devices/self/push-token", `{"token":"fcm_123"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if tokens.deviceID != "device_test" || tokens.token != "fcm_123" {
		t.Fatalf("stored %q for %q", tokens.token, tokens.deviceID)
	}
	if rec := performClientRequest(router, http.MethodPut, "/v1/devices/self/push-token", `{"token":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty token status = %d", rec.Code)
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodPut, "/v1/devices/self/push-token", `{"token":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}
}
