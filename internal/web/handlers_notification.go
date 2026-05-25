package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/app"
)

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
