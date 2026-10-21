package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/views"
)

// handleSendViewAction sends what the owner pressed or chose in a view as
// their message and starts a run on it.
func (s *Server) handleSendViewAction(w http.ResponseWriter, r *http.Request) {
	var req SendViewActionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if strings.TrimSpace(req.ComponentID) == "" {
		s.respondBadRequest(w, r, "component_id is required")
		return
	}
	result, err := s.runs.SendViewAction(r.Context(), chi.URLParam(r, "thread_id"), chi.URLParam(r, "view_id"), views.ActionRequest{
		ComponentID: req.ComponentID,
		Scope:       req.Scope,
		Selected:    req.Selected,
	})
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, ViewActionResponse{Run: runDTOFromDomain(result.Run), Text: result.Text, View: result.View})
}

// handlePinView pins a view to the now page as a panel.
func (s *Server) handlePinView(w http.ResponseWriter, r *http.Request) {
	s.respondViewPin(w, r, s.threads.PinView)
}

// handleUnpinView takes a view off the now page.
func (s *Server) handleUnpinView(w http.ResponseWriter, r *http.Request) {
	s.respondViewPin(w, r, s.threads.UnpinView)
}

func (s *Server) respondViewPin(w http.ResponseWriter, r *http.Request, change func(context.Context, string, string) (core.ViewData, error)) {
	view, err := change(r.Context(), chi.URLParam(r, "thread_id"), chi.URLParam(r, "view_id"))
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, view)
}
