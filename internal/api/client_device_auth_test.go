package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestDeviceAuthProtectsV1Routes(t *testing.T) {
	server := newClientHotPathServer(&clientHandlerStub{})
	server.deviceAuth = newDeviceAuthTestService(&deviceAuthHandlerStub{})
	server.logger = slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
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
	auth := &deviceAuthHandlerStub{}
	server := &Server{
		deviceAuth: newDeviceAuthTestService(auth),
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
	if !strings.HasPrefix(pairResponse.AccessToken, "acorn_dev_") {
		t.Fatalf("pair access token = %q, want acorn_dev_*", pairResponse.AccessToken)
	}
	if !strings.HasPrefix(pairResponse.Device.DeviceID, "device_") || pairResponse.Device.Name != "iPhone" || pairResponse.Device.Platform != "ios" {
		t.Fatalf("unexpected pair response: %#v", pairResponse)
	}

	list := performClientRequest(router, http.MethodGet, "/v1/devices", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var listResponse DeviceListResponse
	decodeClientTestJSON(t, list, &listResponse)
	if len(listResponse.Items) != 1 || listResponse.Items[0].DeviceID != pairResponse.Device.DeviceID {
		t.Fatalf("unexpected list response: %#v", listResponse)
	}

	revoke := performClientRequest(router, http.MethodDelete, "/v1/devices/"+pairResponse.Device.DeviceID, "")
	if revoke.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d body=%s", revoke.Code, revoke.Body.String())
	}
	if auth.revokedDeviceID != pairResponse.Device.DeviceID {
		t.Fatalf("revoked device id = %q, want %s", auth.revokedDeviceID, pairResponse.Device.DeviceID)
	}
}

func TestDeviceAuthRevokedTokenFailsProtectedRoutes(t *testing.T) {
	server := newClientHotPathServer(&clientHandlerStub{})
	server.deviceAuth = newDeviceAuthTestService(&deviceAuthHandlerStub{authErr: ErrDeviceRevoked})
	server.logger = slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
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
