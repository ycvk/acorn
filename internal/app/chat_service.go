package app

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
)

const chatHistoryLimit = 12

type ChatStore interface {
	PrepareChatTurn(ctx context.Context, sessionID, input, title string, historyLimit int) (int, []events.SessionMessageRecord, error)
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
}

type ChatService struct {
	store       ChatStore
	newExecutor func(context.Context) (executorHandle, error)
}

func NewChatService(store ChatStore, newExecutor func(context.Context) (executorHandle, error)) *ChatService {
	return &ChatService{store: store, newExecutor: newExecutor}
}

func (s *ChatService) Send(ctx context.Context, sessionID, input, skillID string, sink runtime.StreamSink) (*runtime.Result, int, error) {
	if s == nil || s.store == nil {
		return nil, 0, errors.New("chat store is nil")
	}
	if s.newExecutor == nil {
		return nil, 0, errors.New("chat executor factory is nil")
	}
	turnIndex, history, err := s.store.PrepareChatTurn(ctx, sessionID, input, deriveSessionTitle(input), chatHistoryLimit)
	if err != nil {
		return nil, 0, err
	}
	exec, err := s.newExecutor(ctx)
	if err != nil {
		return nil, 0, err
	}
	result, err := exec.ExecuteMessages(ctx, runtime.ExecuteRequest{
		SessionID: sessionID,
		TurnIndex: turnIndex,
		Input:     input,
		SkillID:   skillID,
		Messages:  buildChatMessages(history),
	}, sink)
	if err != nil {
		return nil, 0, err
	}
	if err := s.store.SyncAssistantMessageForRun(ctx, result.RunID); err != nil {
		return nil, 0, err
	}
	return result, turnIndex, nil
}

func buildChatMessages(items []events.SessionMessageRecord) []adk.Message {
	messages := make([]adk.Message, 0, len(items))
	for _, item := range items {
		switch item.Role {
		case "user":
			messages = append(messages, schema.UserMessage(item.Content))
		case "assistant":
			messages = append(messages, schema.AssistantMessage(item.Content, nil))
		}
	}
	return messages
}

func deriveSessionTitle(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 48 {
		return trimmed
	}
	return string(runes[:48]) + "..."
}
