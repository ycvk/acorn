package api

import (
	"context"
	"errors"
	"net/http"
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

// PushTokenRequest registers the calling device's FCM token.
type PushTokenRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleSetPushToken(w http.ResponseWriter, r *http.Request) {
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		s.respondInternalError(w, r, errors.New("push token request reached the handler without an authenticated device"))
		return
	}
	var req PushTokenRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		s.respondBadRequest(w, r, "token is required")
		return
	}
	if err := s.deviceAuth.SetPushToken(r.Context(), device.Device.DeviceID, strings.TrimSpace(req.Token)); err != nil {
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
