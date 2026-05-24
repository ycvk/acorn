package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/tooling"
)

type auditedTool struct {
	spec      tooling.ToolSpec
	tool      einotool.BaseTool
	invokable einotool.InvokableTool
	progress  tooling.ProgressTool
	store     runtimeapi.EventAppender
	validator *ToolArgumentValidator
}

type toolAuditCallIDKey struct{}

func withRunID(ctx context.Context, runID string) context.Context {
	return runtimeapi.WithRunID(ctx, runID)
}

func getRunID(ctx context.Context) string {
	return runtimeapi.GetRunID(ctx)
}

func withToolAuditCallID(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, toolAuditCallIDKey{}, strings.TrimSpace(callID))
}

func toolAuditCallID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, ok := ctx.Value(toolAuditCallIDKey{}).(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func wrapToolForAudit(ctx context.Context, store runtimeapi.EventAppender, spec tooling.ToolSpec) (einotool.BaseTool, error) {
	info, err := spec.Tool.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tool info for audit: %w", err)
	}
	invokable, ok := spec.Tool.(einotool.InvokableTool)
	if !ok {
		return spec.Tool, nil
	}
	var validator *ToolArgumentValidator
	if info != nil {
		validator, err = NewToolArgumentValidatorFromToolInfo(info)
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

func (t *auditedTool) InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	return t.run(ctx, argumentsInJSON, emit, opts...)
}

func (t *auditedTool) run(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	runID := getRunID(ctx)
	startedAt := time.Now().UTC()
	if runID != "" {
		if _, err := AppendStreamItem(ctx, t.store, streamSinkFromContext(ctx), StreamItem{
			RunID:     runID,
			Kind:      StreamKindToolCallStarted,
			CreatedAt: startedAt,
			Payload:   &ToolCallStartedPayload{ToolCall: t.streamToolCall(ctx, argumentsInJSON)},
		}); err != nil {
			return "", fmt.Errorf("append tool.call.started audit event: %w", err)
		}
	}

	if t.validator != nil {
		validationErrors, validateErr := t.validator.Validate(argumentsInJSON)
		if validateErr != nil {
			output := validateErr.Error()
			if runID != "" {
				if _, auditErr := AppendStreamItem(ctx, t.store, streamSinkFromContext(ctx), StreamItem{
					RunID:     runID,
					Kind:      StreamKindToolCallFailed,
					CreatedAt: time.Now().UTC(),
					Payload:   &ToolCallFailedPayload{ToolCall: t.failedStreamToolCall(ctx, argumentsInJSON, output, time.Since(startedAt).Milliseconds())},
				}); auditErr != nil {
					return "", fmt.Errorf("append validation error audit event: %w", auditErr)
				}
			}
			return output, fmt.Errorf("validate arguments for %q: %w", t.spec.Name, validateErr)
		}
		if len(validationErrors) > 0 {
			output := FormatValidationError(t.spec.Name, validationErrors)
			if runID != "" {
				if _, auditErr := AppendStreamItem(ctx, t.store, streamSinkFromContext(ctx), StreamItem{
					RunID:     runID,
					Kind:      StreamKindToolCallFailed,
					CreatedAt: time.Now().UTC(),
					Payload:   &ToolCallFailedPayload{ToolCall: t.failedStreamToolCall(ctx, argumentsInJSON, output, time.Since(startedAt).Milliseconds())},
				}); auditErr != nil {
					return "", fmt.Errorf("append tool.call.failed validation event: %w", auditErr)
				}
			}
			return output, fmt.Errorf("tool %q argument validation failed", t.spec.Name)
		}
	}

	output, err := t.invoke(ctx, argumentsInJSON, emit, opts...)
	durationMS := time.Since(startedAt).Milliseconds()
	if runID == "" {
		return output, err
	}

	if interruptCount, interrupted := interruptContextCount(err); interrupted {
		if _, auditErr := AppendStreamItem(ctx, t.store, streamSinkFromContext(ctx), StreamItem{
			RunID:     runID,
			Kind:      StreamKindToolCallInterrupted,
			CreatedAt: time.Now().UTC(),
			Payload:   &ToolCallInterruptedPayload{ToolCall: t.interruptedStreamToolCall(ctx, argumentsInJSON, err.Error(), durationMS, interruptCount)},
		}); auditErr != nil {
			return output, errors.Join(err, fmt.Errorf("append tool.call.interrupted audit event: %w", auditErr))
		}
		return output, err
	}

	if err != nil {
		if _, auditErr := AppendStreamItem(ctx, t.store, streamSinkFromContext(ctx), StreamItem{
			RunID:     runID,
			Kind:      StreamKindToolCallFailed,
			CreatedAt: time.Now().UTC(),
			Payload:   &ToolCallFailedPayload{ToolCall: t.failedStreamToolCall(ctx, argumentsInJSON, err.Error(), durationMS)},
		}); auditErr != nil {
			return output, errors.Join(err, fmt.Errorf("append tool.call.failed audit event: %w", auditErr))
		}
		return output, err
	}

	if _, err := AppendStreamItem(ctx, t.store, streamSinkFromContext(ctx), StreamItem{
		RunID:     runID,
		Kind:      StreamKindToolCallSucceeded,
		CreatedAt: time.Now().UTC(),
		Payload:   &ToolCallSucceededPayload{ToolCall: t.succeededStreamToolCall(ctx, argumentsInJSON, output, durationMS)},
	}); err != nil {
		return output, fmt.Errorf("append tool.call.succeeded audit event: %w", err)
	}

	return output, nil
}

func progressToolFromBase(tool einotool.BaseTool) tooling.ProgressTool {
	progress, ok := tool.(tooling.ProgressTool)
	if !ok {
		return nil
	}
	return progress
}

func (t *auditedTool) invoke(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	if t.progress != nil {
		return t.progress.InvokableRunWithProgress(ctx, argumentsInJSON, t.progressEmitter(ctx, argumentsInJSON, emit), opts...)
	}
	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}

func (t *auditedTool) progressEmitter(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter) tooling.ToolProgressEmitter {
	sequence := 0
	var mu sync.Mutex
	return func(progressCtx context.Context, event tooling.ToolProgressEvent) error {
		mu.Lock()
		defer mu.Unlock()
		sequence++
		runID := getRunID(progressCtx)
		if strings.TrimSpace(runID) == "" {
			runID = getRunID(ctx)
		}
		if runID != "" {
			if _, err := AppendStreamItem(ctx, t.store, streamSinkFromContext(ctx), StreamItem{
				RunID:     runID,
				Kind:      StreamKindToolCallProgress,
				CreatedAt: time.Now().UTC(),
				Payload: &ToolCallProgressPayload{ToolCall: &StreamToolCallProgress{
					Provider:      t.spec.Source,
					Name:          t.spec.Name,
					CallID:        toolAuditCallID(ctx),
					ArgumentsJSON: truncateAudit(argumentsInJSON, 8000),
					Delta:         event.Delta,
					Sequence:      sequence,
				}},
			}); err != nil {
				return fmt.Errorf("append tool.call.progress audit event: %w", err)
			}
		}
		if emit != nil {
			return emit(progressCtx, event)
		}
		return nil
	}
}

func (t *auditedTool) streamToolCall(ctx context.Context, argumentsInJSON string) *StreamToolCall {
	return &StreamToolCall{
		Provider:      t.spec.Source,
		Name:          t.spec.Name,
		CallID:        toolAuditCallID(ctx),
		ArgumentsJSON: truncateAudit(argumentsInJSON, 8000),
	}
}

func (t *auditedTool) failedStreamToolCall(ctx context.Context, argumentsInJSON string, message string, durationMS int64) *StreamToolCall {
	toolCall := t.streamToolCall(ctx, argumentsInJSON)
	toolCall.Error = message
	toolCall.DurationMS = durationMS
	return toolCall
}

func (t *auditedTool) succeededStreamToolCall(ctx context.Context, argumentsInJSON string, output string, durationMS int64) *StreamToolCall {
	toolCall := t.streamToolCall(ctx, argumentsInJSON)
	toolCall.Output = truncateAudit(output, 12000)
	toolCall.DurationMS = durationMS
	return toolCall
}

func (t *auditedTool) interruptedStreamToolCall(ctx context.Context, argumentsInJSON string, message string, durationMS int64, interruptCount int) *StreamToolCall {
	toolCall := t.failedStreamToolCall(ctx, argumentsInJSON, message, durationMS)
	toolCall.InterruptContexts = interruptCount
	return toolCall
}

func truncateAudit(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func interruptContextCount(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	if interruptInfo, ok := compose.ExtractInterruptInfo(err); ok {
		return len(interruptInfo.InterruptContexts), true
	}
	interruptSignal, ok := errors.AsType[*adk.InterruptSignal](err)
	if ok && interruptSignal != nil {
		return 1, true
	}
	return 0, false
}

func buildAuditedTools(
	ctx context.Context,
	store runtimeapi.EventAppender,
	specs []tooling.ToolSpec,
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
