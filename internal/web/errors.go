package web

import (
	"errors"
	"net/http"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/runtime"
	storecore "github.com/ycvk/acorn/internal/store"
)

func (s *Server) respondKnownError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, app.ErrUnauthenticated):
		s.respondError(w, r, http.StatusUnauthorized, "unauthenticated", err.Error())
	case errors.Is(err, app.ErrDeviceRevoked):
		s.respondError(w, r, http.StatusForbidden, "device_revoked", err.Error())
	case errors.Is(err, app.ErrInvalidPairingCode):
		s.respondError(w, r, http.StatusBadRequest, "invalid_pairing_code", err.Error())
	case errors.Is(err, app.ErrDeviceNotFound):
		s.respondNotFound(w, r, "device_not_found", err.Error())
	case errors.Is(err, app.ErrDevicePushTokenForbidden):
		s.respondError(w, r, http.StatusForbidden, "device_push_token_forbidden", err.Error())
	case errors.Is(err, app.ErrInvalidPushProvider):
		s.respondError(w, r, http.StatusBadRequest, "invalid_push_provider", err.Error())
	case errors.Is(err, storecore.ErrDevicePushTokenNotFound):
		s.respondNotFound(w, r, "device_push_token_not_found", err.Error())
	case errors.Is(err, storecore.ErrSessionNotFound):
		s.respondNotFound(w, r, "session_not_found", err.Error())
	case errors.Is(err, storecore.ErrRunNotFound):
		s.respondNotFound(w, r, "run_not_found", err.Error())
	case errors.Is(err, storecore.ErrPendingActionNotFound):
		s.respondNotFound(w, r, "pending_action_not_found", err.Error())
	case errors.Is(err, storecore.ErrPendingActionDecided):
		s.respondConflict(w, r, "pending_action_already_decided", err.Error())
	case errors.Is(err, storecore.ErrFactNotFound):
		s.respondNotFound(w, r, "fact_not_found", err.Error())
	case errors.Is(err, storecore.ErrPlanNotFound):
		s.respondNotFound(w, r, "plan_not_found", err.Error())
	case errors.Is(err, app.ErrSkillAlreadyExists):
		s.respondConflict(w, r, "skill_already_exists", err.Error())
	case errors.Is(err, app.ErrSkillNotFound):
		s.respondNotFound(w, r, "skill_not_found", err.Error())
	case errors.Is(err, runtime.ErrRunNotActive):
		s.respondConflict(w, r, "run_not_active", err.Error())
	case errors.Is(err, runtime.ErrRunNotInterrupted):
		s.respondError(w, r, http.StatusBadRequest, "run_not_resumable", err.Error())
	case errors.Is(err, runtime.ErrExecutionNotReady):
		s.respondError(w, r, http.StatusServiceUnavailable, "execution_not_ready", err.Error())
	default:
		s.respondInternalError(w, r, err)
	}
}
