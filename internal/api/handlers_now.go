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
	default:
		s.respondInternalError(w, r, err)
	}
}
