package api

import (
	"errors"
	"net/http"

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
	id, ok := s.pathID(w, r, "line_id")
	if !ok {
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
