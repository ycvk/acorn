package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Server) handleGetPortrait(w http.ResponseWriter, r *http.Request) {
	portrait, err := s.portrait.Get(r.Context())
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, portrait)
}

func (s *Server) handleRemovePortraitLine(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "line_id"), 10, 64)
	if err != nil || id < 1 {
		s.respondBadRequest(w, r, "line_id must be a positive integer")
		return
	}
	if err := s.portrait.Remove(r.Context(), id); err != nil {
		if errors.Is(err, core.ErrPortraitLineNotFound) {
			s.respondNotFound(w, r, "portrait_line_not_found", "the line is no longer on the portrait; reload it")
			return
		}
		s.respondKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
