package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/runtime"
	storecore "github.com/ycvk/acorn/internal/store"
)

func (s *Server) handleClientListThreads(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.client.ListThreads(r.Context(), limit)
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
	item, err := s.client.CreateThread(r.Context(), req.Title)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, threadDTOFromDomain(*item))
}

func (s *Server) handleClientGetThread(w http.ResponseWriter, r *http.Request) {
	item, err := s.client.GetThread(r.Context(), chi.URLParam(r, "thread_id"))
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
	item, err := s.client.UpdateThread(r.Context(), chi.URLParam(r, "thread_id"), req.Title)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, threadDTOFromDomain(*item))
}

func (s *Server) handleClientDeleteThread(w http.ResponseWriter, r *http.Request) {
	if err := s.client.DeleteThread(r.Context(), chi.URLParam(r, "thread_id")); err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleClientListMessages(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.client.ListMessages(r.Context(), chi.URLParam(r, "thread_id"), limit)
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
	item, err := s.client.CreateMessage(r.Context(), chi.URLParam(r, "thread_id"), req.Content.Text)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, messageDTOFromDomain(*item))
}

func (s *Server) handleClientCreateRun(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeCreateRunRequest(w, r)
	if !ok {
		return
	}
	item, err := s.client.CreateRun(r.Context(), chi.URLParam(r, "thread_id"), req.SkillID, req.Mode)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, runDTOFromDomain(*item))
}

func (s *Server) handleClientGetRun(w http.ResponseWriter, r *http.Request) {
	item, err := s.client.GetRun(r.Context(), chi.URLParam(r, "run_id"))
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, runDTOFromDomain(*item))
}

func (s *Server) handleClientInterruptRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	if err := s.run.InterruptRun(r.Context(), runID); err != nil {
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
	_, ok := s.decodeResumeRunRequest(w, r)
	if !ok {
		return
	}
	result, err := s.resume.Resume(r.Context(), runID, nil)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, result)
}

func (s *Server) handleDecidePendingAction(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeDecidePendingActionRequest(w, r)
	if !ok {
		return
	}
	record, err := s.pendingAction.Decide(r.Context(), chi.URLParam(r, "action_id"), req.Decision)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, pendingActionDecisionDTOFromDomain(*record))
}

func (s *Server) handleListPendingActions(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.pendingAction.List(r.Context(), limit)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, pendingActionListResponseFromDomain(items))
}

func (s *Server) handleGetPendingAction(w http.ResponseWriter, r *http.Request) {
	item, err := s.pendingAction.Get(r.Context(), chi.URLParam(r, "action_id"))
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	if item == nil {
		s.respondInternalError(w, r, errors.New("pending action service returned nil"))
		return
	}
	s.respondJSON(w, r, http.StatusOK, pendingActionDetailDTOFromDomain(*item))
}

func (s *Server) handleClientRunDetail(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	run, err := s.client.GetRun(r.Context(), runID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	thread, err := s.client.GetThread(r.Context(), run.ThreadID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	eventDetail, err := s.client.LoadRunEventsForDetail(r.Context(), runID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	workbench, err := s.workbench.Load(r.Context(), run.ThreadID)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	detail := RunDetailDTO{
		Run:       runDTOFromDomain(*run),
		Thread:    threadDTOFromDomain(*thread),
		Events:    runEventDTOsFromDomain(eventDetail.Events),
		Workbench: runtimeWorkbenchDTOPointer(workbench),
		Trace:     eventDetail.Trace,
	}
	if len(eventDetail.Unsupported) > 0 {
		detail.Raw = &RunDetailRawDTO{UnsupportedEvents: unsupportedRunEventDTOsFromDomain(eventDetail.Unsupported)}
	}
	s.respondJSON(w, r, http.StatusOK, detail)
}

func (s *Server) handleClientInbox(w http.ResponseWriter, r *http.Request) {
	inbox, err := s.inbox.Load(r.Context())
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	if inbox == nil {
		s.respondInternalError(w, r, errors.New("inbox service returned nil"))
		return
	}
	s.respondJSON(w, r, http.StatusOK, inboxDTOFromDomain(*inbox, clientWorkspaceRoot(s.cfg)))
}

func (s *Server) handleClientSystemStatus(w http.ResponseWriter, r *http.Request) {
	snapshot := s.capabilities.Snapshot(r.Context(), app.CapabilitySnapshotOptions{
		ProbeMCP: r.URL.Query().Get("probe_mcp") == "1",
	})
	s.respondJSON(w, r, http.StatusOK, systemStatusDTOFromSnapshot(snapshot, clientWorkspaceRoot(s.cfg)))
}

func (s *Server) handleClientTools(w http.ResponseWriter, r *http.Request) {
	snapshot := s.capabilities.Snapshot(r.Context(), app.CapabilitySnapshotOptions{
		ProbeMCP: r.URL.Query().Get("probe_mcp") == "1",
	})
	items := capabilitiesToolsDTOFromSnapshot(snapshot.Tools)
	s.respondJSON(w, r, http.StatusOK, ToolListResponse{
		Items: items,
		Total: len(items),
	})
}

func (s *Server) handleClientSettings(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, r, http.StatusOK, clientSettingsDTOFromConfig(s.cfg))
}

func (s *Server) handlePatchClientSettings(w http.ResponseWriter, r *http.Request) {
	s.respondError(w, r, http.StatusNotImplemented, "settings_write_unsupported", "client settings write endpoint is not implemented")
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
	items, err := s.client.LoadRunEventsAfter(r.Context(), runID, afterSeq)
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	writer, err := newClientSSEWriter(w)
	if err != nil {
		s.respondInternalError(w, r, err)
		return
	}
	lastSeq := afterSeq
	if len(items) == 0 {
		writer.Start()
		if !follow {
			return
		}
		s.followRunEvents(r, writer, runID, lastSeq)
		return
	}
	for _, item := range items {
		if err := writer.Sink(runEventDTOFromDomain(item)); err != nil {
			if !writer.started {
				s.respondInternalError(w, r, err)
				return
			}
			s.logInternalError(r, "client_sse_backlog_write_failed", err)
			return
		}
		lastSeq = item.Seq
	}
	if !follow {
		return
	}
	s.followRunEvents(r, writer, runID, lastSeq)
}

func (s *Server) respondClientKnownError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, storecore.ErrSessionNotFound):
		s.respondError(w, r, http.StatusNotFound, "thread_not_found", err.Error())
	case errors.Is(err, storecore.ErrRunNotFound):
		s.respondError(w, r, http.StatusNotFound, "run_not_found", err.Error())
	case errors.Is(err, app.ErrClientNoPendingMessage):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrPendingActionDecisionInvalid):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrClientInvalidRunMode):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrClientProjectionFailed):
		s.respondError(w, r, http.StatusInternalServerError, "run_event_projection_failed", err.Error())
	case errors.Is(err, runtime.ErrExecutionNotReady):
		s.respondError(w, r, http.StatusServiceUnavailable, "execution_not_ready", err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}

func (s *Server) followRunEvents(r *http.Request, writer *clientSSEWriter, runID string, lastSeq int64) {
	interval := s.client.EventPollInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			items, err := s.client.LoadRunEventsAfter(r.Context(), runID, lastSeq)
			if err != nil {
				s.logInternalError(r, "client_sse_follow_load_failed", err)
				return
			}
			for _, item := range items {
				if err := writer.Sink(runEventDTOFromDomain(item)); err != nil {
					s.logInternalError(r, "client_sse_follow_write_failed", err)
					return
				}
				lastSeq = item.Seq
			}
			terminal, err := s.client.RunIsTerminal(r.Context(), runID)
			if err != nil {
				s.logInternalError(r, "client_sse_follow_status_failed", err)
				return
			}
			if terminal && len(items) == 0 {
				return
			}
		}
	}
}

func runtimeWorkbenchDTOPointer(item *app.RuntimeWorkbench) *RuntimeWorkbenchDTO {
	if item == nil {
		return nil
	}
	return new(runtimeWorkbenchDTOFromDomain(item))
}

func clientSettingsDTOFromConfig(cfg *config.Config) ClientSettingsDTO {
	if cfg == nil {
		return ClientSettingsDTO{}
	}
	providers := make([]ClientProviderSettingsDTO, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		providers = append(providers, ClientProviderSettingsDTO{
			Name:                provider.Name,
			Model:               provider.Model,
			BaseURL:             provider.BaseURL,
			ReasoningEffort:     provider.ReasoningEffort,
			TimeoutSeconds:      provider.TimeoutSeconds,
			Temperature:         provider.Temperature,
			MaxCompletionTokens: provider.MaxCompletionTokens,
			Enabled:             provider.Enabled,
		})
	}
	return ClientSettingsDTO{
		ConfigPath:    cfg.ConfigPath,
		ConfigDir:     cfg.ConfigDir,
		WorkspaceRoot: clientWorkspaceRoot(cfg),
		Providers:     providers,
		Runtime: ClientRuntimeSettingsDTO{
			StorageDir:        cfg.Runtime.StorageDir,
			RunTimeoutSeconds: cfg.Runtime.RunTimeoutSeconds,
		},
		Web: ClientWebSettingsDTO{
			ListenAddr:     cfg.Web.ListenAddr,
			AllowedOrigins: append([]string(nil), cfg.Web.AllowedOrigins...),
		},
	}
}

func clientWorkspaceRoot(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.WorkspaceRoot()
}
