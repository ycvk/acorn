package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/app"
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
		auth, err := s.deviceAuth.Authenticate(r.Context(), token)
		if err != nil {
			s.respondKnownError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(app.ContextWithDeviceAuth(r.Context(), auth)))
	})
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
		s.respondKnownError(w, r, app.ErrInvalidPairingCode)
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
	result, err := s.deviceAuth.PairDevice(r.Context(), app.PairDeviceInput{
		PairingCode: req.PairingCode,
		DeviceName:  req.DeviceName,
		Platform:    req.Platform,
	})
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

func (s *Server) handleRegisterDevicePushToken(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		s.respondInternalError(w, r, errors.New("web notification service is required"))
		return
	}
	auth, ok := app.DeviceAuthFromContext(r.Context())
	if !ok {
		s.respondKnownError(w, r, app.ErrUnauthenticated)
		return
	}
	var req RegisterDevicePushTokenRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	result, err := s.notifications.RegisterDevicePushToken(r.Context(), auth, app.DevicePushTokenInput{
		DeviceID: strings.TrimSpace(chi.URLParam(r, "device_id")),
		Provider: req.Provider,
		Platform: req.Platform,
		Token:    req.Token,
	})
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, devicePushTokenDTOFromView(*result))
}

func (s *Server) handleRevokeDevicePushToken(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		s.respondInternalError(w, r, errors.New("web notification service is required"))
		return
	}
	auth, ok := app.DeviceAuthFromContext(r.Context())
	if !ok {
		s.respondKnownError(w, r, app.ErrUnauthenticated)
		return
	}
	if err := s.notifications.RevokeDevicePushToken(r.Context(), auth, strings.TrimSpace(chi.URLParam(r, "device_id")), strings.TrimSpace(chi.URLParam(r, "provider"))); err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bearerToken(header string) (string, error) {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return "", app.ErrUnauthenticated
	}
	parts := strings.Fields(trimmed)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", app.ErrUnauthenticated
	}
	return parts[1], nil
}
