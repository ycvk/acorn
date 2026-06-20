package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

func (s *ClientService) ListMessages(ctx context.Context, threadID string, limit int) ([]Message, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	records, err := s.store.ListSessionMessages(ctx, threadID, limit)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, 0, len(records))
	for _, record := range records {
		message, err := projectMessage(record)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (s *ClientService) CreateMessage(ctx context.Context, threadID, content string) (*Message, error) {
	record, err := s.createUserMessage(ctx, threadID, content)
	if err != nil {
		return nil, err
	}
	message, err := projectMessage(*record)
	if err != nil {
		return nil, err
	}
	return &message, nil
}

// createUserMessage records a pending user message and returns the stored record
// (including its id and turn index) so a run can bind to that exact message id.
func (s *ClientService) createUserMessage(ctx context.Context, threadID, content string) (*events.SessionMessageRecord, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("client store is nil")
	}
	if _, err := s.store.LoadSession(ctx, threadID); err != nil {
		return nil, err
	}
	turnIndex, err := s.store.NextSessionMessageTurnIndex(ctx, threadID)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(content)
	record, err := s.store.AppendSessionMessage(ctx, threadID, turnIndex, "user", trimmed, "")
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateSessionTitleIfEmpty(ctx, threadID, generatedThreadTitle(trimmed)); err != nil {
		return nil, err
	}
	return record, nil
}

func projectMessage(record events.SessionMessageRecord) (Message, error) {
	switch record.Role {
	case "user", "assistant", "system", "tool":
	default:
		return Message{}, projectionError("message %d has unsupported role %q", record.ID, record.Role)
	}
	parts, err := projectMessageParts(record)
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID:       fmt.Sprintf("%d", record.ID),
		ThreadID: record.SessionID,
		Role:     record.Role,
		Content: MessageContent{
			Type:  "text",
			Text:  record.Content,
			Parts: parts,
		},
		CreatedAt: record.CreatedAt,
		RunID:     record.RunID,
	}, nil
}

func projectMessageParts(record events.SessionMessageRecord) ([]MessagePart, error) {
	if len(record.ContentParts) == 0 {
		return nil, nil
	}
	var parts []MessagePart
	if err := json.Unmarshal(record.ContentParts, &parts); err != nil {
		return nil, projectionError("message %d has invalid content_parts: %v", record.ID, err)
	}
	for index, part := range parts {
		if err := validateMessagePart(part); err != nil {
			return nil, projectionError("message %d content_parts[%d]: %v", record.ID, index, err)
		}
	}
	return parts, nil
}
