package orchestration

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
)

// CompactFn recovers base messages after a context-overflow error. Returning a
// non-nil error aborts the round (callers wrap with call-site context). Callers
// close over their session/state so RunActionRound stays free of mode-specific
// session details.
type CompactFn func(ctx context.Context, streamErr error) ([]*schema.Message, error)

// RunActionRound is the shared action-round primitive used by both direct_response
// (agent_loop.RunOneIteration) and plan/single_agent (act_node). It runs one
// ExecuteRound and, on a context-overflow error, recovers via compact and retries
// once. BeforeModelCall and recording stay with callers (mode-specific).
func RunActionRound(
	ctx context.Context,
	model einomodel.BaseChatModel,
	streamer AssistantStreamer,
	toolNode ToolInvoker,
	baseMessages []*schema.Message,
	toolInfos []*schema.ToolInfo,
	runID string,
	messageID string,
	allowCompact bool,
	compact CompactFn,
	opts RoundOptions,
) (*schema.Message, []*schema.Message, bool, error) {
	msg, toolMessages, outputLimitReached, err := ExecuteRound(ctx, model, streamer, toolNode, baseMessages, toolInfos, runID, messageID, opts)
	if err == nil {
		return msg, toolMessages, outputLimitReached, nil
	}
	if !contextplane.IsContextOverflowError(err) || !allowCompact || compact == nil {
		return msg, toolMessages, false, err
	}
	recovered, compactErr := compact(ctx, err)
	if compactErr != nil {
		return nil, nil, false, compactErr
	}
	msg, toolMessages, outputLimitReached, err = ExecuteRound(ctx, model, streamer, toolNode, recovered, toolInfos, runID, messageID, opts)
	if err != nil {
		return msg, toolMessages, false, err
	}
	return msg, toolMessages, outputLimitReached, nil
}
