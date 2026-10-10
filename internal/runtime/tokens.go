package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/localit-io/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
	"github.com/ycvk/acorn/internal/core"
)

var tokenLoaderOnce sync.Once

// TokenCounter counts tokens for text and messages.
type TokenCounter interface {
	CountText(context.Context, string) (int, error)
	CountMessages(context.Context, []adk.AgenticMessage, []*schema.ToolInfo) (int, error)
}

// tiktokenCounter is the production TokenCounter backed by tiktoken-go.
type tiktokenCounter struct {
	encodingName string
	encoder      *tiktoken.Tiktoken
}

// NewTokenCounter creates a tiktoken-backed TokenCounter using o200k_base
// (the encoding used by GPT-4o / o1 and a reasonable approximation for other providers).
func NewTokenCounter() (TokenCounter, error) {
	if err := ensureTokenLoader(); err != nil {
		return nil, err
	}
	encoding := "o200k_base"
	encoder, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		return nil, fmt.Errorf("initialize tiktoken encoding %q: %w", encoding, err)
	}
	return &tiktokenCounter{
		encodingName: encoding,
		encoder:      encoder,
	}, nil
}

func (c *tiktokenCounter) CountText(_ context.Context, text string) (int, error) {
	return len(c.encoder.Encode(text, nil, nil)), nil
}

func (c *tiktokenCounter) CountMessages(ctx context.Context, messages []adk.AgenticMessage, tools []*schema.ToolInfo) (int, error) {
	total := 0
	for _, msg := range messages {
		normalized, images := normalizeMessage(msg)
		payload, err := json.Marshal(normalized)
		if err != nil {
			return 0, fmt.Errorf("marshal message for token count: %w", err)
		}
		total += len(c.encoder.Encode(string(payload), nil, nil)) + images*core.ImageInputTokens
	}
	for _, tool := range tools {
		payload, err := json.Marshal(normalizeTool(tool))
		if err != nil {
			return 0, fmt.Errorf("marshal tool for token count: %w", err)
		}
		total += len(c.encoder.Encode(string(payload), nil, nil))
	}
	return total, nil
}

func ensureTokenLoader() error {
	tokenLoaderOnce.Do(func() {
		tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	})
	return nil
}

// normalizeMessage keeps what a model reads as text and counts image inputs
// separately; their encoded bytes say nothing about their token cost.
func normalizeMessage(msg adk.AgenticMessage) (*schema.AgenticMessage, int) {
	if msg == nil {
		return &schema.AgenticMessage{}, 0
	}
	blocks := make([]*schema.ContentBlock, 0, len(msg.ContentBlocks))
	images := 0
	for _, block := range msg.ContentBlocks {
		if block != nil && block.Type == schema.ContentBlockTypeUserInputImage {
			images++
			continue
		}
		blocks = append(blocks, block)
	}
	return &schema.AgenticMessage{Role: msg.Role, ContentBlocks: blocks}, images
}

func normalizeTool(tool *schema.ToolInfo) *schema.ToolInfo {
	if tool == nil {
		return &schema.ToolInfo{}
	}
	clone := *tool
	clone.Extra = nil
	return &clone
}
