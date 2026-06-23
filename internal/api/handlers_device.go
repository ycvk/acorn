package api

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
		if _, err := s.deviceAuth.Authenticate(r.Context(), token); err != nil {
			s.respondKnownError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
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
