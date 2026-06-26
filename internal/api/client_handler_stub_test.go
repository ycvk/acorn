package api

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type clientHandlerStub struct {
	thread    Thread
	message   Message
	run       Run
	events    []core.RunEvent
	artifacts []ArtifactSummary
	err       error

	eventBatches              []*core.RunEventBatch
	loadEventCalls            int
	lastAfterSeq              int64
	statusChecks              int
	terminalAfterStatusChecks int
	createThreadTitle         string
	updateThreadID            string
	updateThreadTitle         string
	deleteThreadID            string
	createMessageThreadID     string
	createMessageContent      string
	createRunThreadID         string
	createRunSkillID          string
}

func (s *clientHandlerStub) ListThreads(context.Context, int) ([]Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []Thread{s.thread}, nil
}

func (s *clientHandlerStub) CreateThread(_ context.Context, title string) (*Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.createThreadTitle = title
	return &s.thread, nil
}

func (s *clientHandlerStub) GetThread(context.Context, string) (*Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &s.thread, nil
}

func (s *clientHandlerStub) UpdateThread(_ context.Context, threadID, title string) (*Thread, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.updateThreadID = threadID
	s.updateThreadTitle = title
	return &s.thread, nil
}

func (s *clientHandlerStub) DeleteThread(_ context.Context, threadID string) error {
	if s.err != nil {
		return s.err
	}
	s.deleteThreadID = threadID
	return nil
}

func (s *clientHandlerStub) ListMessages(context.Context, string, int) ([]Message, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []Message{s.message}, nil
}

func (s *clientHandlerStub) CreateMessage(_ context.Context, threadID, content string) (*Message, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.createMessageThreadID = threadID
	s.createMessageContent = content
	return &s.message, nil
}

func (s *clientHandlerStub) CreateRun(_ context.Context, threadID, skillID, _ string) (*Run, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.createRunThreadID = threadID
	s.createRunSkillID = skillID
	return &s.run, nil
}

func (s *clientHandlerStub) GetRun(context.Context, string) (*Run, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &s.run, nil
}

func (s *clientHandlerStub) LoadRunEventsAfter(_ context.Context, _ string, afterSeq int64) (*core.RunEventBatch, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.lastAfterSeq = afterSeq
	s.loadEventCalls++
	if len(s.eventBatches) > 0 {
		batch := s.eventBatches[0]
		s.eventBatches = s.eventBatches[1:]
		if batch == nil {
			return &core.RunEventBatch{CursorSeq: afterSeq}, nil
		}
		return &core.RunEventBatch{
			Events:    append([]core.RunEvent(nil), batch.Events...),
			CursorSeq: batch.CursorSeq,
		}, nil
	}
	cursorSeq := afterSeq
	if len(s.events) > 0 {
		cursorSeq = s.events[len(s.events)-1].Seq
	}
	return &core.RunEventBatch{
		Events:    append([]core.RunEvent(nil), s.events...),
		CursorSeq: cursorSeq,
	}, nil
}

func (s *clientHandlerStub) LoadRunEventsForDetail(context.Context, string) (*core.RunEventDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &core.RunEventDetail{
		Events: append([]core.RunEvent(nil), s.events...),
	}, nil
}

func (s *clientHandlerStub) ListRunArtifacts(context.Context, string) ([]ArtifactSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]ArtifactSummary(nil), s.artifacts...), nil
}

func (s *clientHandlerStub) RunIsTerminal(context.Context, string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	s.statusChecks++
	if s.terminalAfterStatusChecks > 0 {
		return s.statusChecks >= s.terminalAfterStatusChecks, nil
	}
	return true, nil
}

func (s *clientHandlerStub) InterruptRun(context.Context, string) error {
	if s.err != nil {
		return s.err
	}
	return nil
}

func (s *clientHandlerStub) EventPollInterval() time.Duration {
	return time.Millisecond
}

type inboxHandlerStub struct {
	item *MobileInbox
	err  error
}

func (s *inboxHandlerStub) Load(context.Context) (*MobileInbox, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.item, nil
}
