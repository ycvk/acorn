package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/app"
)

func (s *Server) handleDecidePendingAction(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeDecidePendingActionRequest(w, r)
	if !ok {
		return
	}
	record, err := s.pendingAction.Decide(r.Context(), chi.URLParam(r, "action_id"), app.PendingActionDecisionInput{
		Decision:         req.Decision,
		SelectedOptionID: req.SelectedOptionID,
		Answer:           req.Answer,
	})
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, pendingActionDecisionDTOFromDomain(*record))
}

func (s *Server) handleListPendingActions(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.pendingAction.List(r.Context(), limit)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, pendingActionListResponseFromDomain(items))
}

func (s *Server) handleGetPendingAction(w http.ResponseWriter, r *http.Request) {
	item, err := s.pendingAction.Get(r.Context(), chi.URLParam(r, "action_id"))
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	if item == nil {
		s.respondInternalError(w, r, errors.New("pending action service returned nil"))
		return
	}
	s.respondJSON(w, r, http.StatusOK, pendingActionDetailDTOFromDomain(*item))
}
