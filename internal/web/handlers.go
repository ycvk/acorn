package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/app"
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

func (s *Server) handleDecidePendingAction(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeDecidePendingActionRequest(w, r)
	if !ok {
		return
	}
	record, err := s.pendingAction.Decide(r.Context(), chi.URLParam(r, "action_id"), app.PendingActionDecisionInput{
		Decision:         req.Decision,
		SelectedOptionID: req.SelectedOptionID,
		Answer:           req.Answer,
	})
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

func (s *Server) requireDeviceAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deviceAuth == nil {
			s.respondInternalError(w, r, errors.New("web device auth service is required"))
			return
		}
		token, err := bearerToken(r.Header.Get("Authorization"))
		if err != nil {
			s.respondKnownError(w, r, err)
			return
		}
		auth, err := s.deviceAuth.Authenticate(r.Context(), token)
		if err != nil {
			s.respondKnownError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(app.ContextWithDeviceAuth(r.Context(), auth)))
	})
}

func (s *Server) handlePairDevice(w http.ResponseWriter, r *http.Request) {
	if s.deviceAuth == nil {
		s.respondInternalError(w, r, errors.New("web device auth service is required"))
		return
	}
	var req PairDeviceRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if strings.TrimSpace(req.PairingCode) == "" {
		s.respondKnownError(w, r, app.ErrInvalidPairingCode)
		return
	}
	if strings.TrimSpace(req.DeviceName) == "" {
		s.respondBadRequest(w, r, "device_name is required")
		return
	}
	if strings.TrimSpace(req.Platform) == "" {
		s.respondBadRequest(w, r, "platform is required")
		return
	}
	result, err := s.deviceAuth.PairDevice(r.Context(), app.PairDeviceInput{
		PairingCode: req.PairingCode,
		DeviceName:  req.DeviceName,
		Platform:    req.Platform,
	})
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, PairDeviceResponse{
		Device:      deviceDTOFromView(result.Device),
		AccessToken: result.AccessToken,
	})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.deviceAuth.ListDevices(r.Context())
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	items := make([]DeviceDTO, 0, len(devices))
	for _, device := range devices {
		items = append(items, deviceDTOFromView(device))
	}
	s.respondJSON(w, r, http.StatusOK, DeviceListResponse{Items: items})
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(chi.URLParam(r, "device_id"))
	if err := s.deviceAuth.RevokeDevice(r.Context(), deviceID); err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRegisterDevicePushToken(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		s.respondInternalError(w, r, errors.New("web notification service is required"))
		return
	}
	auth, ok := app.DeviceAuthFromContext(r.Context())
	if !ok {
		s.respondKnownError(w, r, app.ErrUnauthenticated)
		return
	}
	var req RegisterDevicePushTokenRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	result, err := s.notifications.RegisterDevicePushToken(r.Context(), auth, app.DevicePushTokenInput{
		DeviceID: strings.TrimSpace(chi.URLParam(r, "device_id")),
		Provider: req.Provider,
		Platform: req.Platform,
		Token:    req.Token,
	})
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, devicePushTokenDTOFromView(*result))
}

func (s *Server) handleRevokeDevicePushToken(w http.ResponseWriter, r *http.Request) {
	if s.notifications == nil {
		s.respondInternalError(w, r, errors.New("web notification service is required"))
		return
	}
	auth, ok := app.DeviceAuthFromContext(r.Context())
	if !ok {
		s.respondKnownError(w, r, app.ErrUnauthenticated)
		return
	}
	if err := s.notifications.RevokeDevicePushToken(r.Context(), auth, strings.TrimSpace(chi.URLParam(r, "device_id")), strings.TrimSpace(chi.URLParam(r, "provider"))); err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bearerToken(header string) (string, error) {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return "", app.ErrUnauthenticated
	}
	parts := strings.Fields(trimmed)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", app.ErrUnauthenticated
	}
	return parts[1], nil
}
