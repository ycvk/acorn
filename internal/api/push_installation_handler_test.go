package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

type recordingPushInstallations struct {
	deviceID, fid string
}

func (r *recordingPushInstallations) SetPushInstallation(_ context.Context, deviceID, fid string) error {
	r.deviceID, r.fid = deviceID, fid
	return nil
}

func TestSetPushInstallationStoresFIDForAuthenticatedDevice(t *testing.T) {
	installations := &recordingPushInstallations{}
	server := &Server{
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}).WithPushInstallations(installations),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)

	const fid = "cR_8zVYZTfq3ElKmD-3r0o"
	rec := performClientRequest(router, http.MethodPut, "/v1/devices/self/push-installation", `{"fid":"`+fid+`"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if installations.deviceID != "device_test" || installations.fid != fid {
		t.Fatalf("stored %q for %q", installations.fid, installations.deviceID)
	}
	for name, body := range map[string]string{
		"empty":              `{"fid":"  "}`,
		"registration token": `{"fid":"bk3RNwTe3H0:CI2k_HHwgIpoDKCIZvvDMExUdFQ3P1"}`,
		"wrong prefix":       `{"fid":"aR_8zVYZTfq3ElKmD-3r0o"}`,
		"too long":           `{"fid":"` + fid + `x"}`,
		"old field":          `{"token":"` + fid + `"}`,
	} {
		if rec := performClientRequest(router, http.MethodPut, "/v1/devices/self/push-installation", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", name, rec.Code)
		}
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodPut, "/v1/devices/self/push-installation", `{"fid":"`+fid+`"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}
}
