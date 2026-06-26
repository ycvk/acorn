package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
)

var errUnexpectedClientStoreCall = errors.New("unexpected client hot-path store call")

type clientHandlerStore struct {
	stub *clientHandlerStub
}

func newClientHotPathServices(stub *clientHandlerStub) (*ThreadService, *RunService, *EventService) {
	store := &clientHandlerStore{stub: stub}
	workspaceRoot := strings.TrimSpace(stub.thread.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = "/repo"
	}
	threads := NewThreadService(store, workspaceRoot)
	controller := runtime.NewRunController()
	controller.Register(clientHotPathRunID(stub), func() {})
	runs := NewRunService(store, threads, stub.executeRun, controller)
	events := NewEventService(store)
	return threads, runs, events
}

func newClientHotPathServer(stub *clientHandlerStub) *Server {
	threads, runs, events := newClientHotPathServices(stub)
	return &Server{
		threads: threads,
		runs:    runs,
		events:  events,
	}
}

func newRunResumeTestService(result *RunResult, err error) *RunResumeService {
	store := &clientHandlerStore{stub: &clientHandlerStub{
		run: Run{ID: "run_1", Status: "interrupted", ThreadID: "thread_1"},
		events: []core.RunEvent{{
			EventID: "run_1:1",
			RunID:   "run_1",
			Seq:     1,
			TS:      time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			Type:    "run.interrupted",
			Data: core.RunInterruptedData{
				Interrupt: map[string]any{
					"contexts": []map[string]any{{
						"id":            "interrupt_1",
						"is_root_cause": true,
					}},
				},
			},
		}},
	}}
	svc := NewRunResumeService(store)
	svc.WithResume(func(context.Context, string, map[string]any, core.StreamSink) (*runtime.Result, error) {
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
		}
		status := core.RunStatusInterrupted
		switch result.Status {
		case "completed":
			status = core.RunStatusSucceeded
		case "failed":
			status = core.RunStatusFailed
		case "running":
			status = core.RunStatusRunning
		}
		return &runtime.Result{
			RunID:       result.RunID,
			Status:      status,
			Output:      result.Output,
			Error:       result.Error,
			Interrupted: cloneMap(result.Interrupted),
		}, nil
	})
	return svc
}

func clientHotPathRunID(stub *clientHandlerStub) string {
	if stub != nil && strings.TrimSpace(stub.run.ID) != "" {
		return strings.TrimSpace(stub.run.ID)
	}
	return "run_1"
}

func (s *clientHandlerStub) executeRun(_ context.Context, req core.ExecuteRequest, sink core.StreamSink) (*runtime.Result, error) {
	if s != nil && s.err != nil {
		return nil, s.err
	}
	if s != nil {
		s.createRunThreadID = req.SessionID
		s.createRunSkillID = req.SkillID
		if strings.TrimSpace(s.run.ID) == "" {
			s.run = Run{
				ID:        req.RunID,
				ThreadID:  req.SessionID,
				Status:    "running",
				CreatedAt: time.Date(2026, 5, 2, 10, 3, 0, 0, time.UTC),
			}
		}
	}
	if sink != nil {
		if err := sink(core.StreamItem{RunID: req.RunID, Kind: core.StreamKindRunStarted}); err != nil {
			return nil, err
		}
	}
	return &runtime.Result{RunID: req.RunID, Status: core.RunStatusRunning}, nil
}

func (s *clientHandlerStore) stubOrErr() (*clientHandlerStub, error) {
	if s == nil || s.stub == nil {
		return nil, errUnexpectedClientStoreCall
	}
	if s.stub.err != nil {
		return nil, s.stub.err
	}
	return s.stub, nil
}

func (s *clientHandlerStore) sessionRecord() core.SessionRecord {
	stub := s.stub
	return core.SessionRecord{
		SessionID: strings.TrimSpace(stub.thread.ID),
		Title:     strings.TrimSpace(stub.thread.Title),
		CreatedAt: stub.thread.CreatedAt,
		UpdatedAt: stub.thread.UpdatedAt,
	}
}

func (s *clientHandlerStore) messageRecord() *core.SessionMessageRecord {
	stub := s.stub
	if stub == nil || strings.TrimSpace(stub.message.ThreadID) == "" && strings.TrimSpace(stub.message.Content.Text) == "" {
		return nil
	}
	id := int64(7)
	if strings.TrimSpace(stub.message.ID) != "" {
		id = 7
	}
	return &core.SessionMessageRecord{
		ID:        id,
		SessionID: strings.TrimSpace(stub.message.ThreadID),
		TurnIndex: 1,
		Role:      strings.TrimSpace(stub.message.Role),
		Content:   strings.TrimSpace(stub.message.Content.Text),
		RunID:     strings.TrimSpace(stub.message.RunID),
		CreatedAt: stub.message.CreatedAt,
	}
}

func (s *clientHandlerStore) runRecord(runID string) *core.RunRecord {
	stub := s.stub
	if stub == nil {
		return nil
	}
	id := strings.TrimSpace(stub.run.ID)
	if id == "" {
		id = strings.TrimSpace(runID)
	}
	if id == "" {
		id = "run_1"
	}
	sessionID := strings.TrimSpace(stub.run.ThreadID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(stub.thread.ID)
	}
	if sessionID == "" {
		sessionID = "thread_1"
	}
	status := core.RunStatusRunning
	switch strings.TrimSpace(stub.run.Status) {
	case "completed":
		status = core.RunStatusSucceeded
	case "interrupted":
		status = core.RunStatusInterrupted
	case "failed":
		status = core.RunStatusFailed
	case "running", "":
		status = core.RunStatusRunning
	}
	if stub.terminalAfterStatusChecks > 0 {
		stub.statusChecks++
		if stub.statusChecks >= stub.terminalAfterStatusChecks {
			status = core.RunStatusSucceeded
		}
	}
	return &core.RunRecord{
		RunID:      id,
		SessionID:  sessionID,
		TurnIndex:  1,
		Status:     status,
		Input:      strings.TrimSpace(stub.message.Content.Text),
		Output:     "",
		Error:      "",
		CreatedAt:  stub.run.CreatedAt,
		FinishedAt: stub.run.CompletedAt,
	}
}

func eventRecordFromRunEvent(item core.RunEvent) core.EventRecord {
	payload := map[string]any{}
	switch item.Type {
	case "run.started":
		if data, ok := item.Data.(core.RunStartedData); ok {
			payload["input"] = data.Input
		}
	case "assistant.delta":
		if data, ok := item.Data.(core.AssistantDeltaData); ok {
			payload["assistant_delta"] = data.AssistantDelta
		}
	case "agent.message":
		if data, ok := item.Data.(core.AgentMessageData); ok {
			payload["message"] = data.Message
		}
	case "run.completed":
		if data, ok := item.Data.(core.RunCompletedData); ok {
			payload["message"] = data.Message
		}
	case "run.failed":
		if data, ok := item.Data.(core.RunFailedData); ok {
			payload["error"] = data.Error
		}
	case "run.interrupted":
		if data, ok := item.Data.(core.RunInterruptedData); ok {
			payload["interrupt"] = data.Interrupt
		}
	case "run.resume_requested":
		if data, ok := item.Data.(core.RunResumeRequestedData); ok {
			payload["targets"] = data.Targets
		}
	default:
		if raw, ok := item.Data.(map[string]any); ok {
			payload = raw
		}
	}
	return core.EventRecord{
		Sequence:  item.Seq,
		RunID:     item.RunID,
		Kind:      item.Type,
		Payload:   payload,
		CreatedAt: item.TS,
	}
}

func artifactRecordFromSummary(item ArtifactSummary) core.ArtifactRecord {
	return core.ArtifactRecord{
		ArtifactID:          item.ArtifactID,
		RunID:               item.RunID,
		SessionID:           item.SessionID,
		SourceToolResultRef: item.SourceToolResultRef,
		Kind:                item.Kind,
		Title:               item.Title,
		MIMEType:            item.MIMEType,
		SizeBytes:           item.SizeBytes,
		SHA256:              item.SHA256,
		CreatedAt:           item.CreatedAt,
	}
}
