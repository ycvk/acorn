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
	threadID := chi.URLParam(r, "thread_id")
	items, err := s.threads.ListMessages(r.Context(), threadID, limit)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	queued, err := s.threads.QueuedMessages(r.Context(), threadID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, MessageListResponse{Items: DefaultConverter.messageDTOsFromDomain(items), Queued: DefaultConverter.queuedMessageDTOsFromDomain(queued)})
}
