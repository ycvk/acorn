package orchestration

import (
	"context"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/cloudwego/eino/adk"
)

// RunActionRound runs one ExecuteRound. The reactive compact retry path was
// removed: auto-compact is now handled inside ContextSession.BeforeModelCall
// proactively (token threshold), not reactively after a provider overflow.
func RunActionRound(
	ctx context.Context,
	model einomodel.BaseChatModel,
	streamer AssistantStreamer,
	toolNode ToolInvoker,
	baseMessages []*schema.Message,
	toolInfos []*schema.ToolInfo,
	runID string,
	messageID string,
	_ RoundOptions,
) (*schema.Message, []*schema.Message, bool, error) {
	return ExecuteRound(ctx, model, streamer, toolNode, baseMessages, toolInfos, runID, messageID, RoundOptions{})
}

var _ = adk.Message(nil)
