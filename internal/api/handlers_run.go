package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleClientCreateRun(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeCreateRunRequest(w, r)
	if !ok {
		return
	}
	item, err := s.runs.CreateRun(r.Context(), chi.URLParam(r, "thread_id"), req.SkillID, req.Input)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, runDTOFromDomain(*item))
}

func (s *Server) handleClientGetRun(w http.ResponseWriter, r *http.Request) {
	item, err := s.runs.GetRun(r.Context(), chi.URLParam(r, "run_id"))
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, runDTOFromDomain(*item))
}

func (s *Server) handleClientInterruptRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	if err := s.runs.InterruptRun(r.Context(), runID); err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusAccepted, InterruptRunResponse{
		RunID:  runID,
		Status: "interrupt_requested",
	})
}

func (s *Server) handleClientResumeRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	result, err := s.runResume.Resume(r.Context(), runID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleClientRunDetail(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	run, err := s.runs.GetRun(r.Context(), runID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	thread, err := s.threads.GetThread(r.Context(), run.ThreadID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	eventDetail, err := s.events.LoadRunEventsForDetail(r.Context(), runID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	artifacts, err := s.events.ListRunArtifacts(r.Context(), runID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	detail := RunDetailDTO{
		Run:       runDTOFromDomain(*run),
		Thread:    threadDTOFromDomain(*thread),
		Events:    eventDetail.Events,
		Artifacts: artifactSummaryDTOsFromDomain(artifacts),
	}
	s.respondJSON(w, r, http.StatusOK, detail)
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	afterSeq, err := parseClientAfterSeq(r.URL.Query().Get("after_seq"))
	if err != nil {
		s.respondError(w, r, http.StatusBadRequest, "invalid_after_seq", err.Error())
		return
	}
	follow, err := parseClientFollow(r.URL.Query().Get("follow"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	runID := chi.URLParam(r, "run_id")
	batch, err := s.events.LoadRunEventsAfter(r.Context(), runID, afterSeq)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	writer, err := newClientSSEWriter(w)
	if err != nil {
		s.respondInternalError(w, r, err)
		return
	}
	lastSeq := batch.CursorSeq
	if len(batch.Events) == 0 {
		writer.Start()
		if !follow {
			return
		}
		s.followRunEvents(r, writer, runID, lastSeq)
		return
	}
	for _, item := range batch.Events {
		if err := writer.Sink(item); err != nil {
			if !writer.started {
				s.respondInternalError(w, r, err)
				return
			}
			s.logInternalError(r, "client_sse_backlog_write_failed", err)
			return
		}
	}
	if !follow {
		return
	}
	s.followRunEvents(r, writer, runID, lastSeq)
}

func (s *Server) followRunEvents(r *http.Request, writer *clientSSEWriter, runID string, lastSeq int64) {
	interval := s.events.EventPollInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			batch, err := s.events.LoadRunEventsAfter(r.Context(), runID, lastSeq)
			if err != nil {
				s.logInternalError(r, "client_sse_follow_load_failed", err)
				return
			}
			lastSeq = batch.CursorSeq
			for _, item := range batch.Events {
				if err := writer.Sink(item); err != nil {
					s.logInternalError(r, "client_sse_follow_write_failed", err)
					return
				}
			}
			terminal, err := s.runs.RunIsTerminal(r.Context(), runID)
			if err != nil {
				s.logInternalError(r, "client_sse_follow_status_failed", err)
				return
			}
			if terminal && len(batch.Events) == 0 {
				return
			}
		}
	}
}
