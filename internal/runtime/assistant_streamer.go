package runtime

import (
	"context"

	"github.com/ycvk/acorn/internal/orchestration"
)

type directAssistantStreamer struct {
	appender eventAppender
}

func newDirectAssistantStreamer(appender eventAppender) *directAssistantStreamer {
	return &directAssistantStreamer{appender: appender}
}

func (s *directAssistantStreamer) StreamAssistantMessage(ctx context.Context, req orchestration.AssistantStreamRequest) (*orchestration.AssistantStreamResult, error) {
	return streamAssistantMessage(ctx, req.Model, req.Messages, assistantStreamOptions{
		MessageID: req.MessageID,
		RunID:     req.RunID,
		Appender:  s.appender,
		Sink:      streamSinkFromContext(ctx),
		ToolInfos: req.ToolInfos,
		CallSite:  req.CallSite,
	})
}

func (s *directAssistantStreamer) StreamAssistantInterleaved(ctx context.Context, req orchestration.AssistantStreamRequest) *orchestration.InterleavedStream {
	return streamAssistantInterleaved(ctx, req.Model, req.Messages, assistantStreamOptions{
		MessageID: req.MessageID,
		RunID:     req.RunID,
		Appender:  s.appender,
		Sink:      streamSinkFromContext(ctx),
		ToolInfos: req.ToolInfos,
		CallSite:  req.CallSite,
	})
}
