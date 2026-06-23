package toolkit

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"
)

type ToolProgressEvent struct {
	Delta string
}

type ToolProgressEmitter func(ctx context.Context, event ToolProgressEvent) error

type ProgressTool interface {
	einotool.BaseTool
	InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit ToolProgressEmitter, opts ...einotool.Option) (string, error)
}
