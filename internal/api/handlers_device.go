package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) requireDeviceAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deviceAuth == nil {
			s.respondInternalError(w, r, errors.New("web device auth service is required"))
			return
		}
		token, err := bearerToken(r.Header.Get("Authorization"))
		if err != nil {
			s.respondKnownError(w, r, err)
			return
		}
		device, err := s.deviceAuth.Authenticate(r.Context(), token)
		if err != nil {
			s.respondKnownError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(withDevice(r.Context(), device)))
	})
}

type deviceContextKey struct{}

func withDevice(ctx context.Context, device *DeviceAuthContext) context.Context {
	return context.WithValue(ctx, deviceContextKey{}, device)
}

// DeviceFromContext returns the device that authenticated the request.
func DeviceFromContext(ctx context.Context) (*DeviceAuthContext, bool) {
	device, ok := ctx.Value(deviceContextKey{}).(*DeviceAuthContext)
	return device, ok && device != nil
}

// PushInstallationRequest registers the Firebase installation ID under which
// the calling device's app is registered with FCM.
type PushInstallationRequest struct {
	FID string `json:"fid"`
}

// firebaseInstallationID matches the 22 base64url characters of a Firebase
// installation ID, whose first four bits are 0111.
var firebaseInstallationID = regexp.MustCompile(`^[cdef][A-Za-z0-9_-]{21}$`)

func (s *Server) handleSetPushInstallation(w http.ResponseWriter, r *http.Request) {
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		s.respondInternalError(w, r, errors.New("push installation request reached the handler without an authenticated device"))
		return
	}
	var req PushInstallationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if !firebaseInstallationID.MatchString(req.FID) {
		s.respondBadRequest(w, r, "fid must be a Firebase installation ID")
		return
	}
	if err := s.deviceAuth.SetPushInstallation(r.Context(), device.Device.DeviceID, req.FID); err != nil {
		s.respondInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePairDevice(w http.ResponseWriter, r *http.Request) {
	if s.deviceAuth == nil {
		s.respondInternalError(w, r, errors.New("web device auth service is required"))
		return
	}
	var req PairDeviceRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if strings.TrimSpace(req.PairingCode) == "" {
		s.respondKnownError(w, r, ErrInvalidPairingCode)
		return
	}
	if strings.TrimSpace(req.DeviceName) == "" {
		s.respondBadRequest(w, r, "device_name is required")
		return
	}
	if strings.TrimSpace(req.Platform) == "" {
		s.respondBadRequest(w, r, "platform is required")
		return
	}
	result, err := s.deviceAuth.PairDevice(r.Context(), PairDeviceInput(req))
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, PairDeviceResponse{
		Device:      deviceDTOFromView(result.Device),
		AccessToken: result.AccessToken,
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.deviceAuth.ListDevices(r.Context())
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	items := make([]DeviceDTO, 0, len(devices))
	for _, device := range devices {
		items = append(items, deviceDTOFromView(device))
	}
	s.respondJSON(w, r, http.StatusOK, DeviceListResponse{Items: items})
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(chi.URLParam(r, "device_id"))
	if err := s.deviceAuth.RevokeDevice(r.Context(), deviceID); err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
