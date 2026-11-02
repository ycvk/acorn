package runtime

import (
	"context"
	"errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

type auditedTool struct {
	spec      core.ToolSpec
	tool      einotool.BaseTool
	invokable einotool.InvokableTool
	validator *toolArgumentValidator
}

func wrapToolForAudit(ctx context.Context, spec core.ToolSpec) (einotool.BaseTool, error) {
	info, err := spec.Tool.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tool info for audit: %w", err)
	}
	invokable, ok := spec.Tool.(einotool.InvokableTool)
	if !ok {
		return spec.Tool, nil
	}
	var validator *toolArgumentValidator
	if info != nil {
		validator, err = newToolArgumentValidatorFromToolInfo(info)
		if err != nil {
			return nil, fmt.Errorf("create tool argument validator for %q: %w", info.Name, err)
		}
	}
	return &auditedTool{
		spec:      spec,
		tool:      spec.Tool,
		invokable: invokable,
		validator: validator,
	}, nil
}

func (t *auditedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.tool.Info(ctx)
}

func (t *auditedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	if t.validator != nil {
		validationErrors, validateErr := t.validator.validate(argumentsInJSON)
		if validateErr != nil {
			return "", fmt.Errorf("validate arguments for %q: %w", t.spec.Name, validateErr)
		}
		if len(validationErrors) > 0 {
			return "", errors.New(formatValidationError(t.spec.Name, validationErrors))
		}
	}

	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}

func BuildAuditedTools(ctx context.Context, specs []core.ToolSpec) ([]einotool.BaseTool, error) {
	items := make([]einotool.BaseTool, 0, len(specs))
	for _, spec := range specs {
		if !spec.Enabled() || spec.Tool == nil {
			continue
		}
		if err := spec.ToolContract.Validate(); err != nil {
			return nil, fmt.Errorf("audit tool contract %q: %w", spec.Name, err)
		}
		wrapped, err := wrapToolForAudit(ctx, spec)
		if err != nil {
			return nil, err
		}
		items = append(items, wrapped)
	}
	return items, nil
}
