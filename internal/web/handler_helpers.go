package web

import (
	"errors"
	"net/http"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

func (s *Server) respondClientKnownError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrSessionNotFound):
		s.respondError(w, r, http.StatusNotFound, "thread_not_found", err.Error())
	case errors.Is(err, store.ErrRunNotFound):
		s.respondError(w, r, http.StatusNotFound, "run_not_found", err.Error())
	case errors.Is(err, app.ErrClientNoPendingMessage):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrPendingActionDecisionInvalid):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrClientInvalidRunMode):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrClientProjectionFailed):
		s.respondError(w, r, http.StatusInternalServerError, "run_event_projection_failed", err.Error())
	case errors.Is(err, runtime.ErrExecutionNotReady):
		s.respondError(w, r, http.StatusServiceUnavailable, "execution_not_ready", err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}

func runtimeWorkbenchDTOPointer(item *app.RuntimeWorkbench) *RuntimeWorkbenchDTO {
	if item == nil {
		return nil
	}
	return new(runtimeWorkbenchDTOFromDomain(item))
}

func clientWorkspaceRoot(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.WorkspaceRoot()
}
