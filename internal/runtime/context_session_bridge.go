package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

func (e *Executor) bootstrapContextSessionMessages(
	ctx context.Context,
	req runtimeapi.ExecuteRequest,
	runID string,
	active *ActiveRunner,
) ([]adk.Message, error) {
	if err := e.validateBootstrapDeps(active); err != nil {
		return nil, err
	}
	counter, err := contextplane.NewCompressionTokenCounter()
	if err != nil {
		return nil, fmt.Errorf("build context session token counter: %w", err)
	}
	session := e.buildContextSession(active, e.runRuntime.Config().Context, counter)
	input, err := session.Bootstrap(ctx, contextplane.BootstrapRequest{
		SessionID:       req.SessionID,
		RunID:           runID,
		TurnIndex:       req.TurnIndex,
		InitialMessages: prepareInitialMessages(req, active),
		Assembly:        active.ContextResult,
	})
	if err != nil {
		return nil, fmt.Errorf("bootstrap context session: %w", err)
	}
	active.ContextSession = session
	return input.Messages, nil
}

func (e *Executor) validateBootstrapDeps(active *ActiveRunner) error {
	if e == nil || e.runRuntime == nil || e.runRuntime.Config() == nil {
		return fmt.Errorf("context session bootstrap requires runtime config")
	}
	if active == nil {
		return fmt.Errorf("context session bootstrap requires active runner")
	}
	return nil
}

func (e *Executor) buildContextSession(active *ActiveRunner, contextPolicy config.ContextConfig, counter *contextplane.CompressionTokenCounter) contextplane.ContextSession {
	return contextplane.NewDefaultContextSession(contextplane.ContextSessionOptions{
		PreservePolicy: contextplane.PreservePolicy{
			RecentTurns:       contextPolicy.PreserveRecentTurns,
			PreserveToolPairs: true,
		},
		State: active.CompressionState,
	})
}

func prepareInitialMessages(req runtimeapi.ExecuteRequest, active *ActiveRunner) []adk.Message {
	initialMessages := append([]adk.Message(nil), req.Messages...)
	if instruction := strings.TrimSpace(active.Instruction); instruction != "" {
		initialMessages = append([]adk.Message{schema.SystemMessage(instruction)}, initialMessages...)
	}
	return initialMessages
}
