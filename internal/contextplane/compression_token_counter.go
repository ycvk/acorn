package contextplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/localit-io/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"

	"github.com/ycvk/acorn/internal/config"
)

var compressionTokenLoaderOnce sync.Once

type CompressionTokenCounter struct {
	encodingName string
	encoder      *tiktoken.Tiktoken
}

type TokenCounter interface {
	CountText(context.Context, string) (int, error)
	CountMessages(context.Context, []adk.Message, []*schema.ToolInfo) (int, error)
}

func NewCompressionTokenCounter(cfg config.ContextConfig) (*CompressionTokenCounter, error) {
	if err := ensureCompressionTokenLoader(); err != nil {
		return nil, err
	}
	encoding := strings.TrimSpace(cfg.TokenEncoding)
	if encoding == "" {
		return nil, errors.New("compression token encoding is required")
	}
	encoder, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, fmt.Errorf("initialize tiktoken encoding %q: %w", encoding, err)
	}
	return &CompressionTokenCounter{
		encodingName: encoding,
		encoder:      encoder,
	}, nil
}

func (c *CompressionTokenCounter) CountText(_ context.Context, text string) (int, error) {
	return len(c.encoder.Encode(text, nil, nil)), nil
}

func (c *CompressionTokenCounter) CountMessages(ctx context.Context, messages []adk.Message, tools []*schema.ToolInfo) (int, error) {
	return c.count(ctx, messages, tools)
}

func (c *CompressionTokenCounter) CountReduction(ctx context.Context, messages []adk.Message, tools []*schema.ToolInfo) (int64, error) {
	total, err := c.count(ctx, messages, tools)
	if err != nil {
		return 0, err
	}
	return int64(total), nil
}

func (c *CompressionTokenCounter) count(_ context.Context, messages []adk.Message, tools []*schema.ToolInfo) (int, error) {
	total := 0
	for _, msg := range messages {
		payload, err := json.Marshal(normalizeCompressionMessage(msg))
		if err != nil {
			return 0, fmt.Errorf("marshal compression message: %w", err)
		}
		total += len(c.encoder.Encode(string(payload), nil, nil))
	}
	for _, tool := range tools {
		payload, err := json.Marshal(normalizeCompressionTool(tool))
		if err != nil {
			return 0, fmt.Errorf("marshal compression tool: %w", err)
		}
		total += len(c.encoder.Encode(string(payload), nil, nil))
	}
	return total, nil
}

func ensureCompressionTokenLoader() error {
	compressionTokenLoaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
	return nil
}

func normalizeCompressionMessage(msg adk.Message) *schema.Message {
	if msg == nil {
		return &schema.Message{}
	}
	return &schema.Message{
		Role:                     msg.Role,
		Content:                  msg.Content,
		UserInputMultiContent:    append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...),
		AssistantGenMultiContent: append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...),
		Name:                     msg.Name,
		ToolCalls:                append([]schema.ToolCall(nil), msg.ToolCalls...),
		ToolCallID:               msg.ToolCallID,
		ToolName:                 msg.ToolName,
		ReasoningContent:         msg.ReasoningContent,
	}
}

func normalizeCompressionTool(tool *schema.ToolInfo) *schema.ToolInfo {
	if tool == nil {
		return &schema.ToolInfo{}
	}
	clone := *tool
	clone.Extra = nil
	return &clone
}
