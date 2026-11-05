package api

import (
	"errors"
	"net/http"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

func (s *Server) handleListKnowledgeNotes(w http.ResponseWriter, r *http.Request) {
	limit, err := parseBoundedLimit(r.URL.Query().Get("limit"), defaultKnowledgeListLimit, maxKnowledgeListLimit)
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	query := r.URL.Query()
	notes, err := s.knowledge.ListNotes(r.Context(), query.Get("q"), query.Get("prefix"), limit)
	if err != nil {
		s.respondKnowledgeError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, notes)
}

func (s *Server) handleGetKnowledgeNote(w http.ResponseWriter, r *http.Request) {
	note, err := s.knowledge.GetNote(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		s.respondKnowledgeError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, note)
}

func (s *Server) handleDeleteKnowledgeNote(w http.ResponseWriter, r *http.Request) {
	if err := s.knowledge.DeleteNote(r.Context(), r.URL.Query().Get("path")); err != nil {
		s.respondKnowledgeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) respondKnowledgeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, knowledge.ErrInvalidPath):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, core.ErrKnowledgeNoteNotFound):
		s.respondNotFound(w, r, "knowledge_note_not_found", err.Error())
	case errors.Is(err, core.ErrKnowledgeConflict):
		s.respondConflict(w, r, "knowledge_note_conflict", err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}
