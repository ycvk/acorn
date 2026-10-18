package api

import (
	"errors"
	"net/http"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Server) handleGetOwner(w http.ResponseWriter, r *http.Request) {
	owner, err := s.owner.Current(r.Context())
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, owner)
}

func (s *Server) handleSetOwner(w http.ResponseWriter, r *http.Request) {
	var req SetOwnerRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	owner, err := s.owner.SetName(r.Context(), req.Name)
	if errors.Is(err, core.ErrOwnerNameInvalid) {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, owner)
}
