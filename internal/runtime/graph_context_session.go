package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
)

func graphModelCallID(ctx context.Context, prefix string) string {
	runID := strings.TrimSpace(CurrentRunID(ctx))
	if runID == "" {
		return prefix
	}
	return prefix + "-" + runID
}

func graphSessionModelCallRequest(callID, source string, toolInfos []*schema.ToolInfo) contextplane.ModelCallRequest {
	return contextplane.ModelCallRequest{
		CallID:       callID,
		QuerySource:  source,
		AllowCompact: true,
		ToolInfos:    append([]*schema.ToolInfo(nil), toolInfos...),
	}
}

func graphSessionBaseMessages(ctx context.Context, state *agentGraphState, req contextplane.ModelCallRequest) (contextplane.ContextSession, []*schema.Message, error) {
	session := contextplane.ContextSessionFromContext(ctx)
	if session == nil {
		if state == nil {
			return nil, nil, fmt.Errorf("graph state is required")
		}
		return nil, append([]*schema.Message(nil), state.Messages...), nil
	}
	input, err := session.BeforeModelCall(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	base := schemaMessagesFromADK(input.Messages)
	if state != nil {
		state.Messages = append([]*schema.Message(nil), base...)
	}
	return session, base, nil
}

func graphSessionReactiveBaseMessages(ctx context.Context, session contextplane.ContextSession, state *agentGraphState, req contextplane.ModelCallRequest, cause error) ([]*schema.Message, error) {
	if session == nil {
		return nil, cause
	}
	input, err := session.ReactiveCompact(ctx, req, cause)
	if err != nil {
		return nil, err
	}
	base := schemaMessagesFromADK(input.Messages)
	if state != nil {
		state.Messages = append([]*schema.Message(nil), base...)
	}
	return base, nil
}

func graphSessionRecordAssistant(ctx context.Context, session contextplane.ContextSession, msg *schema.Message) error {
	if session == nil {
		return nil
	}
	if err := session.RecordAssistant(ctx, msg); err != nil {
		return fmt.Errorf("record graph assistant message: %w", err)
	}
	return nil
}

func graphSessionRecordMessages(ctx context.Context, session contextplane.ContextSession, messages []*schema.Message) error {
	if session == nil || len(messages) == 0 {
		return nil
	}
	adkMessages := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			adkMessages = append(adkMessages, msg)
		}
	}
	if err := session.RecordMessages(ctx, adkMessages); err != nil {
		return fmt.Errorf("record graph messages: %w", err)
	}
	return nil
}

func graphSessionRecordToolResults(ctx context.Context, session contextplane.ContextSession, messages []*schema.Message) error {
	if session == nil || len(messages) == 0 {
		return nil
	}
	adkMessages := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			adkMessages = append(adkMessages, msg)
		}
	}
	if err := session.RecordToolResults(ctx, adkMessages); err != nil {
		return fmt.Errorf("record graph tool results: %w", err)
	}
	return nil
}

func schemaMessagesFromADK(messages []adk.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			out = append(out, msg)
		}
	}
	return out
}
