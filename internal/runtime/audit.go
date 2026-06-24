package runtime

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/tools"

	"github.com/ycvk/acorn/internal/port"
)

type auditedTool struct {
	spec      port.ToolSpec
	tool      einotool.BaseTool
	invokable einotool.InvokableTool
	progress  tools.ProgressTool
	store     domain.EventAppender
	validator *toolArgumentValidator
}

func getRunID(ctx context.Context) string {
	return domain.GetRunID(ctx)
}

func wrapToolForAudit(ctx context.Context, store domain.EventAppender, spec port.ToolSpec) (einotool.BaseTool, error) {
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
		progress:  progressToolFromBase(spec.Tool),
		store:     store,
		validator: validator,
	}, nil
}

func (t *auditedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.tool.Info(ctx)
}

func (t *auditedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return t.run(ctx, argumentsInJSON, nil, opts...)
}

func (t *auditedTool) InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit tools.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	return t.run(ctx, argumentsInJSON, emit, opts...)
}

func (t *auditedTool) run(ctx context.Context, argumentsInJSON string, emit tools.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	if t.validator != nil {
		validationErrors, validateErr := t.validator.validate(argumentsInJSON)
		if validateErr != nil {
			return validateErr.Error(), fmt.Errorf("validate arguments for %q: %w", t.spec.Name, validateErr)
		}
		if len(validationErrors) > 0 {
			output := formatValidationError(t.spec.Name, validationErrors)
			return output, fmt.Errorf("tool %q argument validation failed", t.spec.Name)
		}
	}

	output, err := t.invoke(ctx, argumentsInJSON, emit, opts...)
	return output, err
}

func progressToolFromBase(tool einotool.BaseTool) tools.ProgressTool {
	progress, ok := tool.(tools.ProgressTool)
	if !ok {
		return nil
	}
	return progress
}

func (t *auditedTool) invoke(ctx context.Context, argumentsInJSON string, emit tools.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	if t.progress != nil {
		return t.progress.InvokableRunWithProgress(ctx, argumentsInJSON, emit, opts...)
	}
	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}

func BuildAuditedTools(
	ctx context.Context,
	store domain.EventAppender,
	specs []port.ToolSpec,
	excludedToolNames []string,
	allowedToolNames []string,
	_ string,
) ([]einotool.BaseTool, error) {
	excluded := make(map[string]struct{}, len(excludedToolNames))
	for _, name := range excludedToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		excluded[trimmed] = struct{}{}
	}
	allowed := make(map[string]struct{}, len(allowedToolNames))
	for _, name := range allowedToolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	items := make([]einotool.BaseTool, 0, len(specs))
	for _, spec := range specs {
		if !spec.Enabled() || spec.Tool == nil {
			continue
		}
		if err := spec.ToolContract.Validate(); err != nil {
			return nil, fmt.Errorf("audit tool contract %q: %w", spec.Name, err)
		}
		if _, skip := excluded[spec.Name]; skip {
			continue
		}
		if len(allowed) > 0 {
			if _, keep := allowed[spec.Name]; !keep {
				continue
			}
		}
		wrapped, err := wrapToolForAudit(ctx, store, spec)
		if err != nil {
			return nil, err
		}
		items = append(items, wrapped)
	}
	return items, nil
}
