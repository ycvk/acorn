package api

import (
	"errors"
	"net/http"

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

func (s *Server) handleStopGoal(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "goal_id")
	if !ok {
		return
	}
	s.respondNowChange(w, r, s.now.StopGoal(r.Context(), id))
}

func (s *Server) handleCancelFollowUp(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r, "follow_up_id")
	if !ok {
		return
	}
	s.respondNowChange(w, r, s.now.CancelFollowUp(r.Context(), id))
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
	device, ok := s.authenticatedDevice(w, r)
	if !ok {
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

// StoryMessageResponse is the story's conversation, the run the owner's
// message started and the message's text.
type StoryMessageResponse struct {
	ThreadID string `json:"thread_id"`
	Run      RunDTO `json:"run"`
	Text     string `json:"text"`
}

// handleDiscussStory asks Acorn to explain a story in its conversation.
func (s *Server) handleDiscussStory(w http.ResponseWriter, r *http.Request) {
	s.respondStoryMessage(w, r, core.StoryDiscuss)
}

// handleFollowStory asks Acorn to keep following a story in its
// conversation.
func (s *Server) handleFollowStory(w http.ResponseWriter, r *http.Request) {
	s.respondStoryMessage(w, r, core.StoryFollow)
}

func (s *Server) respondStoryMessage(w http.ResponseWriter, r *http.Request, intent core.StoryIntent) {
	id, ok := s.pathID(w, r, "story_id")
	if !ok {
		return
	}
	result, err := s.runs.SendStoryMessage(r.Context(), id, intent)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, StoryMessageResponse{ThreadID: result.ThreadID, Run: runDTOFromDomain(result.Run), Text: result.Text})
}

// pathID reads a positive integer path parameter, answering 400 when it is not one.
func (s *Server) pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := parsePositiveInt(chi.URLParam(r, name), name)
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
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
	case errors.Is(err, core.ErrGoalNotFound):
		s.respondNotFound(w, r, "goal_not_found", err.Error())
	case errors.Is(err, core.ErrGoalEnded):
		s.respondConflict(w, r, "goal_ended", err.Error())
	case errors.Is(err, core.ErrFollowUpNotFound):
		s.respondNotFound(w, r, "follow_up_not_found", err.Error())
	case errors.Is(err, core.ErrFollowUpNotWaiting):
		s.respondConflict(w, r, "follow_up_not_waiting", err.Error())
	case errors.Is(err, core.ErrWatchNotFound):
		s.respondNotFound(w, r, "watch_not_found", err.Error())
	case errors.Is(err, core.ErrNewsStoryNotFound):
		s.respondNotFound(w, r, "story_not_found", err.Error())
	default:
		s.respondInternalError(w, r, err)
	}
}
