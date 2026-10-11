package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := defaultMemoryListLimit
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxMemoryListLimit {
			s.respondBadRequest(w, r, fmt.Sprintf("limit must be between 1 and %d", maxMemoryListLimit))
			return
		}
		limit = value
	}
	kind := query.Get("kind")
	if kind != "" && kind != "fact" && kind != "insight" && kind != "thought" {
		s.respondBadRequest(w, r, "kind must be fact, insight or thought")
		return
	}
	memories, err := s.memories.List(r.Context(), kind, query.Get("q"), limit)
	if err != nil {
		s.respondMemoryError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, memories)
}

func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	memory, err := s.memories.Get(r.Context(), chi.URLParam(r, "memory_id"))
	if err != nil {
		s.respondMemoryError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, memory)
}

func (s *Server) handleForgetMemory(w http.ResponseWriter, r *http.Request) {
	result, err := s.memories.Forget(r.Context(), chi.URLParam(r, "memory_id"))
	if err != nil {
		s.respondMemoryError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleCorrectMemory(w http.ResponseWriter, r *http.Request) {
	var req CorrectMemoryRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if req.Revision < 1 {
		s.respondBadRequest(w, r, "revision must be a positive integer")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		s.respondBadRequest(w, r, "content is required")
		return
	}
	memory, err := s.memories.Correct(r.Context(), chi.URLParam(r, "memory_id"), req)
	if err != nil {
		s.respondMemoryError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, memory)
}

func (s *Server) respondMemoryError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, core.ErrMemoryNotFound), errors.Is(err, core.ErrMemoryExcluded):
		s.respondNotFound(w, r, "memory_not_found", err.Error())
	case errors.Is(err, core.ErrMemoryConflict):
		s.respondConflict(w, r, "memory_changed", "the memory changed since it was read; reload it and try again")
	case errors.Is(err, core.ErrMemoryNotFact):
		s.respondBadRequest(w, r, err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}
