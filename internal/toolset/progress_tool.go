package toolset

import (
	"context"
	"encoding/json"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/toolkit"
)

type progressToolFunc[I, O any] func(ctx context.Context, input I, emit toolkit.ToolProgressEmitter) (O, error)

type localProgressTool[I, O any] struct {
	infoSource einotool.BaseTool
	name       string
	fn         progressToolFunc[I, O]
}

func inferProgressTool[I, O any](name string, desc string, fn progressToolFunc[I, O]) (einotool.BaseTool, error) {
	infoSource, err := toolutils.InferTool(name, desc, func(ctx context.Context, input I) (O, error) {
		return fn(ctx, input, nil)
	})
	if err != nil {
		return nil, err
	}
	return &localProgressTool[I, O]{
		infoSource: infoSource,
		name:       name,
		fn:         fn,
	}, nil
}

func (t *localProgressTool[I, O]) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.infoSource.Info(ctx)
}

func (t *localProgressTool[I, O]) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return t.InvokableRunWithProgress(ctx, argumentsInJSON, nil, opts...)
}

func (t *localProgressTool[I, O]) InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit toolkit.ToolProgressEmitter, _ ...einotool.Option) (string, error) {
	var input I
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse %s arguments: %w", t.name, err)
	}
	output, err := t.fn(ctx, input, emit)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("marshal %s output: %w", t.name, err)
	}
	return string(body), nil
}

func emitToolProgress(ctx context.Context, emit toolkit.ToolProgressEmitter, delta string) error {
	if emit == nil || delta == "" {
		return nil
	}
	return emit(ctx, toolkit.ToolProgressEvent{Delta: delta})
}
