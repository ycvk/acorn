package api

import (
	"errors"
	"net/http"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
)

func (s *Server) respondKnownError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		s.respondError(w, r, http.StatusUnauthorized, "unauthenticated", err.Error())
	case errors.Is(err, ErrDeviceRevoked):
		s.respondError(w, r, http.StatusForbidden, "device_revoked", err.Error())
	case errors.Is(err, ErrInvalidPairingCode):
		s.respondError(w, r, http.StatusBadRequest, "invalid_pairing_code", err.Error())
	case errors.Is(err, ErrDeviceNotFound):
		s.respondNotFound(w, r, "device_not_found", err.Error())
	case errors.Is(err, store.ErrSessionNotFound):
		s.respondNotFound(w, r, "session_not_found", err.Error())
	case errors.Is(err, store.ErrRunNotFound):
		s.respondNotFound(w, r, "run_not_found", err.Error())
	case errors.Is(err, store.ErrPendingActionNotFound):
		s.respondNotFound(w, r, "pending_action_not_found", err.Error())
	case errors.Is(err, store.ErrPendingActionDecided):
		s.respondConflict(w, r, "pending_action_already_decided", err.Error())
	case errors.Is(err, store.ErrFactNotFound):
		s.respondNotFound(w, r, "fact_not_found", err.Error())
	case errors.Is(err, ErrSkillAlreadyExists):
		s.respondConflict(w, r, "skill_already_exists", err.Error())
	case errors.Is(err, ErrSkillNotFound):
		s.respondNotFound(w, r, "skill_not_found", err.Error())
	case errors.Is(err, domain.ErrRunNotActive):
		s.respondConflict(w, r, "run_not_active", err.Error())
	case errors.Is(err, domain.ErrRunNotInterrupted):
		s.respondError(w, r, http.StatusBadRequest, "run_not_resumable", err.Error())
	case errors.Is(err, domain.ErrExecutionNotReady):
		s.respondError(w, r, http.StatusServiceUnavailable, "execution_not_ready", err.Error())
	default:
		s.respondInternalError(w, r, err)
	}
}
