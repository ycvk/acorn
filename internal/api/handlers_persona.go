package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/core"
)

func (s *Server) handleGetPersona(w http.ResponseWriter, r *http.Request) {
	persona, err := s.persona.Current(r.Context())
	if err != nil {
		s.respondPersonaError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, persona)
}

func (s *Server) handleSavePersona(w http.ResponseWriter, r *http.Request) {
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		s.respondInternalError(w, r, errors.New("persona request reached the handler without an authenticated device"))
		return
	}
	var req SavePersonaRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if req.BaseRevision < 1 {
		s.respondBadRequest(w, r, "base_revision must be a positive integer")
		return
	}
	persona, err := s.persona.Save(r.Context(), device.Device.DeviceID, req.Content, req.BaseRevision)
	if err != nil {
		s.respondPersonaError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, persona)
}

func (s *Server) handleListPersonaRevisions(w http.ResponseWriter, r *http.Request) {
	revisions, err := s.persona.Revisions(r.Context())
	if err != nil {
		s.respondPersonaError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, revisions)
}

func (s *Server) handleGetPersonaRevision(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(chi.URLParam(r, "revision"), 10, 64)
	if err != nil || revision < 1 {
		s.respondBadRequest(w, r, "revision must be a positive integer")
		return
	}
	persona, err := s.persona.Revision(r.Context(), revision)
	if err != nil {
		s.respondPersonaError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, persona)
}

func (s *Server) respondPersonaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, core.ErrPersonaInvalid):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, core.ErrPersonaConflict):
		s.respondConflict(w, r, "persona_changed", "the persona changed since it was read; reload it and try again")
	case errors.Is(err, core.ErrPersonaNotFound):
		s.respondNotFound(w, r, "persona_not_found", err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}
