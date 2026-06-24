package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleClientListMessages(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.threads.ListMessages(r.Context(), chi.URLParam(r, "thread_id"), limit)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, MessageListResponse{Items: messageDTOsFromDomain(items)})
}

func (s *Server) handleClientCreateMessage(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeCreateMessageRequest(w, r)
	if !ok {
		return
	}
	item, err := s.threads.CreateMessage(r.Context(), chi.URLParam(r, "thread_id"), req.Content.Text)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, messageDTOFromDomain(*item))
}
