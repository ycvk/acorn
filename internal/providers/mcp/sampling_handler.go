package mcpprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SamplingExecutor is the interface for executing sampling sub-runs.
// It accepts converted Eino messages and returns the LLM text output.
// The runtime.Executor satisfies this interface via an adapter closure
// set through Manager.SetSamplingExecutor.
type SamplingExecutor interface {
	ExecuteMessages(ctx context.Context, messages []*schema.Message) (output string, err error)
}

// samplingDepthCap is the maximum depth for recursive sampling requests.
// Per D-05, the cap is 2 to prevent unbounded LLM recursion.
const samplingDepthCap int32 = 2

// SamplingHandler handles MCP server sampling/createMessage requests by creating
// sub-runs with the Executor lifecycle and returning LLM responses to the requesting
// server.
//
// Per D-04 (sub-run with full executor lifecycle), D-05 (depth cap=2 via atomic counter),
// D-06 (no operator approval for sampling responses):
//   - Each sampling request creates a sub-run using Executor.ExecuteMessages
//   - The sampling depth is tracked via Manager.samplingDepth atomic counter
//   - Depth is checked BEFORE sub-run creation and decremented in defer
//   - Sampling responses are returned directly without operator approval
type SamplingHandler struct {
	manager  *Manager
	executor SamplingExecutor
}

// newSamplingHandler creates a SamplingHandler bound to the given manager.
// The executor and store must be set before HandleCreateMessage is called.
func newSamplingHandler(manager *Manager) *SamplingHandler {
	return &SamplingHandler{
		manager: manager,
	}
}

// HandleCreateMessage handles an MCP sampling/createMessage request.
// Per D-04, D-05, D-06:
//
//  1. Atomically increment samplingDepth. If result > cap, decrement and return error.
//  2. Defer atomic decrement of samplingDepth.
//  3. Convert SamplingMessage array to []adk.Message for ExecuteMessages.
//     If system prompt is provided, prepend it as a system message.
//  4. Call executor.ExecuteMessages with the converted messages.
//  5. On success, return CreateMessageResult. On error, return the error.
func (h *SamplingHandler) HandleCreateMessage(ctx context.Context, req *mcp.CreateMessageRequest) (*mcp.CreateMessageResult, error) {
	newDepth := atomic.AddInt32(&h.manager.samplingDepth, 1)
	if newDepth > samplingDepthCap {
		atomic.AddInt32(&h.manager.samplingDepth, -1)
		return nil, errors.New("sampling depth cap exceeded")
	}
	defer atomic.AddInt32(&h.manager.samplingDepth, -1)

	if h.executor == nil {
		return nil, errors.New("sampling executor not configured")
	}

	params := req.Params
	messages := convertSamplingMessages(params.Messages, params.SystemPrompt)

	output, err := h.executor.ExecuteMessages(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("sampling sub-run execution failed: %w", err)
	}

	model := "acorn-default"
	return &mcp.CreateMessageResult{
		Content:    &mcp.TextContent{Text: output},
		Model:      model,
		Role:       mcp.Role("assistant"),
		StopReason: "endTurn",
	}, nil
}

// convertSamplingMessages converts MCP SamplingMessage array to Eino Message slice.
// If a system prompt is provided, it is prepended as a system message.
// Only TextContent is handled; ImageContent is deferred per plan.
func convertSamplingMessages(messages []*mcp.SamplingMessage, systemPrompt string) []*schema.Message {
	var result []*schema.Message

	if strings.TrimSpace(systemPrompt) != "" {
		result = append(result, schema.SystemMessage(systemPrompt))
	}

	for _, msg := range messages {
		if msg == nil {
			continue
		}
		role := samplingRoleToEino(msg.Role)
		content := extractTextContent(msg.Content)
		if content == "" {
			continue
		}
		result = append(result, &schema.Message{
			Role:    role,
			Content: content,
		})
	}

	return result
}

// samplingRoleToEino maps an MCP Role to an Eino RoleType string.
func samplingRoleToEino(role mcp.Role) schema.RoleType {
	switch role {
	case "user":
		return schema.User
	case "assistant":
		return schema.Assistant
	default:
		return schema.User
	}
}

// extractTextContent extracts text from an MCP Content value.
// Only TextContent is supported; other content types return empty string.
func extractTextContent(content mcp.Content) string {
	if tc, ok := content.(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}
