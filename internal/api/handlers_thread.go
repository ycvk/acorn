package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleClientListThreads(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.threads.ListThreads(r.Context(), limit)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, ThreadListResponse{Items: threadDTOsFromDomain(items)})
}

func (s *Server) handleClientCreateThread(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeCreateThreadRequest(w, r)
	if !ok {
		return
	}
	item, err := s.threads.CreateThread(r.Context(), req.Title)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, threadDTOFromDomain(*item))
}

func (s *Server) handleClientGetThread(w http.ResponseWriter, r *http.Request) {
	item, err := s.threads.GetThread(r.Context(), chi.URLParam(r, "thread_id"))
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, threadDTOFromDomain(*item))
}

func (s *Server) handleClientUpdateThread(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeUpdateThreadRequest(w, r)
	if !ok {
		return
	}
	item, err := s.threads.UpdateThread(r.Context(), chi.URLParam(r, "thread_id"), req.Title)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, threadDTOFromDomain(*item))
}

func (s *Server) handleClientDeleteThread(w http.ResponseWriter, r *http.Request) {
	if err := s.threads.DeleteThread(r.Context(), chi.URLParam(r, "thread_id")); err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
