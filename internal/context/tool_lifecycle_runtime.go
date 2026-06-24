package context

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

// ToolCallRejectedError indicates a tool call was rejected by the lifecycle
// guard (e.g. deferred tool not yet loaded).
type ToolCallRejectedError struct {
	ToolName string
	Reason   string
}

func (e *ToolCallRejectedError) Error() string {
	if e == nil {
		return "tool call rejected"
	}
	toolName := strings.TrimSpace(e.ToolName)
	reason := strings.TrimSpace(e.Reason)
	if toolName == "" && reason == "" {
		return "tool call rejected"
	}
	if toolName == "" {
		return "tool call rejected: " + reason
	}
	if reason == "" {
		return fmt.Sprintf("tool %q rejected by lifecycle", toolName)
	}
	return fmt.Sprintf("tool %q rejected by lifecycle: %s", toolName, reason)
}

// OnToolCall validates that a tool is loaded (not deferred) before execution.
func OnToolCall(ctx context.Context, event ToolCallEvent) error {
	toolName := strings.TrimSpace(event.ToolName)
	if toolName == "" {
		return errors.New("tool call event requires tool_name")
	}
	lifecycleCtx := ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil || lifecycleCtx.State == nil {
		return errors.New("tool lifecycle state is not initialized")
	}
	lifecycleCtx.State.Mu().Lock()
	_, loaded := lifecycleCtx.State.LoadedTools[toolName]
	_, deferred := lifecycleCtx.State.DeferredTools[toolName]
	lifecycleCtx.State.Mu().Unlock()
	if loaded {
		return nil
	}
	if deferred {
		return &ToolCallRejectedError{
			ToolName: toolName,
			Reason:   "deferred; load it with load_tools before calling it",
		}
	}
	return &ToolCallRejectedError{
		ToolName: toolName,
		Reason:   "not loaded or enabled for this run",
	}
}

// OnToolResult records a tool result event. The durable ledger was removed;
// this now only validates the event payload. The result content stays in the
// message stream and is subject to observation masking by Session.
func OnToolResult(ctx context.Context, event ToolResultEvent) error {
	if strings.TrimSpace(event.ToolName) == "" {
		return errors.New("tool result event requires tool_name")
	}
	if strings.TrimSpace(event.CallID) == "" {
		return errors.New("tool result event requires call_id")
	}
	if strings.TrimSpace(event.RunID) == "" {
		return errors.New("tool result event requires run_id")
	}
	return nil
}

// DeferredLoad loads deferred tools into the loaded set by name or query match.
func DeferredLoad(ctx context.Context, req DeferredLoadRequest) (*DeferredLoadResult, error) {
	if len(req.ToolNames) == 0 && strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("deferred load requires tool_names or query")
	}
	limit := req.Limit
	if limit == 0 {
		limit = 5
	}
	if limit <= 0 || limit > 5 {
		return nil, fmt.Errorf("deferred load limit must be between 1 and %d", 5)
	}
	lifecycleCtx := ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil || lifecycleCtx.State == nil {
		return nil, errors.New("tool lifecycle state is not initialized")
	}
	if lifecycleCtx.Catalog == nil {
		return nil, errors.New("tool lifecycle catalog is not initialized")
	}
	lifecycleCtx.State.Mu().Lock()
	selected, alreadyLoaded, err := resolveDeferredLoadTargets(lifecycleCtx, req, limit)
	if err != nil {
		lifecycleCtx.State.Mu().Unlock()
		return nil, err
	}
	if len(selected) == 0 && len(alreadyLoaded) == 0 {
		lifecycleCtx.State.Mu().Unlock()
		return nil, errors.New("deferred load found no matching tools")
	}
	now := time.Now().UTC()
	loadedNames := make([]string, 0, len(selected))
	records := make([]DeferredToolRecord, 0, len(selected))
	for _, name := range selected {
		record, ok := lifecycleCtx.State.DeferredTools[name]
		if !ok {
			continue
		}
		delete(lifecycleCtx.State.DeferredTools, name)
		lifecycleCtx.State.LoadedTools[name] = LoadedToolRecord{
			Name:       name,
			LoadedAt:   now,
			LoadSource: "deferred",
		}
		loadedNames = append(loadedNames, name)
		records = append(records, record)
	}
	lifecycleCtx.State.Mu().Unlock()
	var messages []*schema.Message
	if msg := formatDeferredToolDefinitions(records); msg != nil {
		messages = append(messages, msg)
	}
	sort.Strings(loadedNames)
	sort.Strings(alreadyLoaded)
	return &DeferredLoadResult{
		Messages:        messages,
		LoadedToolNames: loadedNames,
		AlreadyLoaded:   alreadyLoaded,
	}, nil
}

func formatDeferredToolDefinitions(records []DeferredToolRecord) *schema.Message {
	if len(records) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("<deferred-tool-definitions>\n")
	for _, record := range records {
		if strings.TrimSpace(record.Name) == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(record.Name)
		if desc := strings.TrimSpace(record.Description); desc != "" {
			b.WriteString(": ")
			b.WriteString(desc)
		}
		if reason := strings.TrimSpace(record.Reason); reason != "" {
			b.WriteString(" [")
			b.WriteString(reason)
			b.WriteString("]")
		}
		b.WriteString("\n")
	}
	b.WriteString("</deferred-tool-definitions>")
	return schema.UserMessage(b.String())
}

func resolveDeferredLoadTargets(lifecycleCtx *ToolLifecycleContext, req DeferredLoadRequest, limit int) ([]string, []string, error) {
	selectedSet := make(map[string]struct{})
	alreadySet := make(map[string]struct{})
	for _, raw := range req.ToolNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, nil, errors.New("deferred load tool_names must not contain empty entries")
		}
		if _, ok := lifecycleCtx.State.LoadedTools[name]; ok {
			alreadySet[name] = struct{}{}
			continue
		}
		record, ok := lifecycleCtx.State.DeferredTools[name]
		if !ok {
			if spec, found := lifecycleCtx.Catalog.Find(name); found && !spec.Enabled() {
				return nil, nil, fmt.Errorf("tool %q is disabled", name)
			}
			return nil, nil, fmt.Errorf("tool %q is not available for deferred load", name)
		}
		selectedSet[record.Name] = struct{}{}
	}
	query := strings.TrimSpace(strings.ToLower(req.Query))
	if query != "" {
		for name, record := range lifecycleCtx.State.DeferredTools {
			if matchesDeferredToolQuery(record, query) {
				selectedSet[name] = struct{}{}
			}
		}
		for name := range lifecycleCtx.State.LoadedTools {
			if strings.Contains(strings.ToLower(name), query) {
				alreadySet[name] = struct{}{}
			}
		}
	}
	selected := mapKeys(selectedSet)
	alreadyLoaded := mapKeys(alreadySet)
	if len(selected) > limit {
		return nil, nil, fmt.Errorf("deferred load selected %d tools, exceeds limit %d", len(selected), limit)
	}
	sort.Strings(selected)
	sort.Strings(alreadyLoaded)
	return selected, alreadyLoaded, nil
}

func matchesDeferredToolQuery(record DeferredToolRecord, query string) bool {
	if strings.Contains(strings.ToLower(record.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(record.Description), query) {
		return true
	}
	return strings.Contains(strings.ToLower(record.Reason), query)
}

func mapKeys(items map[string]struct{}) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return keys
}
