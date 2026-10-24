package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Server) handleGetNow(w http.ResponseWriter, r *http.Request) {
	now, err := s.now.Load(r.Context())
	if err != nil {
		s.respondInternalError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, now)
}

func (s *Server) handleCancelCommitment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "commitment_id")
	if !ok {
		return
	}
	s.respondNowChange(w, r, s.now.CancelCommitment(r.Context(), id))
}

func (s *Server) handlePauseWatch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "watch_id")
	if !ok {
		return
	}
	s.respondNowChange(w, r, s.now.PauseWatch(r.Context(), id))
}

func (s *Server) handleResumeWatch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "watch_id")
	if !ok {
		return
	}
	s.respondNowChange(w, r, s.now.ResumeWatch(r.Context(), id))
}

// StoryReactionRequest is PUT /v1/news/stories/{story_id}/reaction.
type StoryReactionRequest struct {
	Reaction string `json:"reaction"`
}

func (s *Server) handleReactToStory(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "story_id")
	if !ok {
		return
	}
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		s.respondInternalError(w, r, errors.New("story reaction reached the handler without an authenticated device"))
		return
	}
	var req StoryReactionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	reaction := core.NewsReaction(req.Reaction)
	if !reaction.Valid() {
		s.respondBadRequest(w, r, "reaction must be more or less")
		return
	}
	s.respondNowChange(w, r, s.now.ReactToStory(r.Context(), id, reaction, device.Device.DeviceID))
}

func (s *Server) handleClearStoryReaction(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "story_id")
	if !ok {
		return
	}
	s.respondNowChange(w, r, s.now.ClearStoryReaction(r.Context(), id))
}

// pathID reads a positive integer path parameter, answering 400 when it is not one.
func (s *Server) pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		s.respondBadRequest(w, r, name+" must be a positive integer")
		return 0, false
	}
	return id, true
}

func (s *Server) respondNowChange(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, core.ErrCommitmentNotFound):
		s.respondNotFound(w, r, "commitment_not_found", err.Error())
	case errors.Is(err, core.ErrCommitmentEnded):
		s.respondConflict(w, r, "commitment_ended", err.Error())
	case errors.Is(err, core.ErrWatchNotFound):
		s.respondNotFound(w, r, "watch_not_found", err.Error())
	case errors.Is(err, core.ErrNewsStoryNotFound):
		s.respondNotFound(w, r, "story_not_found", err.Error())
	default:
		s.respondInternalError(w, r, err)
	}
}
