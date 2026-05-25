package contextplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/store"
)

const (
	defaultDeferredLoadLimit     = 5
	defaultToolLifecycleMaxTurns = 2
	defaultToolLifecycleMaxRefs  = 32
)

type toolLifecycleContextKey struct{}

type ToolLifecycleContext struct {
	Plane           ContextPlane
	State           *ToolLifecycleState
	Catalog         *tooling.Catalog
	ToolInfosByName map[string]*schema.ToolInfo
}

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

func WithToolLifecycleContext(ctx context.Context, plane ContextPlane, state *ToolLifecycleState, catalog *tooling.Catalog, infos []*schema.ToolInfo) context.Context {
	infoMap := make(map[string]*schema.ToolInfo, len(infos))
	for _, info := range infos {
		if info == nil || strings.TrimSpace(info.Name) == "" {
			continue
		}
		infoMap[strings.TrimSpace(info.Name)] = info
	}
	return context.WithValue(ctx, toolLifecycleContextKey{}, &ToolLifecycleContext{
		Plane:           plane,
		State:           state,
		Catalog:         catalog,
		ToolInfosByName: infoMap,
	})
}

func ToolLifecycleContextFromContext(ctx context.Context) *ToolLifecycleContext {
	if ctx == nil {
		return nil
	}
	raw := ctx.Value(toolLifecycleContextKey{})
	lifecycleCtx, ok := raw.(*ToolLifecycleContext)
	if !ok {
		return nil
	}
	return lifecycleCtx
}

func LoadedToolInfosFromContext(ctx context.Context, always []string) []*schema.ToolInfo {
	lifecycleCtx := ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil || lifecycleCtx.State == nil {
		return nil
	}
	lifecycleCtx.State.mu.Lock()
	names := make([]string, 0, len(always)+len(lifecycleCtx.State.LoadedTools))
	names = append(names, always...)
	for name := range lifecycleCtx.State.LoadedTools {
		names = append(names, name)
	}
	lifecycleCtx.State.mu.Unlock()
	deduped := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		deduped = append(deduped, trimmed)
	}
	sort.Strings(deduped)
	infos := make([]*schema.ToolInfo, 0, len(deduped))
	for _, name := range deduped {
		info := lifecycleCtx.ToolInfosByName[name]
		if info == nil {
			continue
		}
		infos = append(infos, info)
	}
	return infos
}

func PruneToolMessages(ctx context.Context, messages []*schema.Message, currentTurn int) []*schema.Message {
	lifecycleCtx := ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil || lifecycleCtx.State == nil || len(messages) == 0 {
		return messages
	}
	lifecycleCtx.State.mu.Lock()
	records := make(map[string]ToolResultRecord, len(lifecycleCtx.State.RecentResults))
	maxAgeTurns := lifecycleCtx.State.MaxAgeTurns
	for _, item := range lifecycleCtx.State.RecentResults {
		if strings.TrimSpace(item.CallID) == "" {
			continue
		}
		records[item.CallID] = item
	}
	lifecycleCtx.State.mu.Unlock()
	if len(records) == 0 {
		return messages
	}
	pruned := append([]*schema.Message(nil), messages...)
	for i, msg := range pruned {
		if msg == nil || msg.Role != schema.Tool || strings.TrimSpace(msg.ToolCallID) == "" {
			continue
		}
		record, ok := records[msg.ToolCallID]
		if !ok {
			continue
		}
		shouldPrune := currentTurn-record.TurnIndex > maxAgeTurns
		if !shouldPrune {
			continue
		}
		clone := *msg
		clone.Content = formatPrunedToolResult(record)
		record.PrunedAt = new(time.Now().UTC())
		updateToolResultRecord(lifecycleCtx.State, record)
		pruned[i] = &clone
	}
	return pruned
}

func newToolLifecycleState(ctx context.Context, req AssembleRequest) *ToolLifecycleState {
	state := &ToolLifecycleState{
		RunID:         strings.TrimSpace(req.RunID),
		SessionID:     strings.TrimSpace(req.SessionID),
		LoadedTools:   make(map[string]LoadedToolRecord),
		DeferredTools: make(map[string]DeferredToolRecord),
		MaxAgeTurns:   defaultToolLifecycleMaxTurns,
		MaxResultRefs: defaultToolLifecycleMaxRefs,
	}
	if req.ToolCatalog == nil {
		return state
	}

	eagerNames, deferred := splitToolDefinitions(ctx, req.ToolCatalog.EnabledSpecsForProfile(tooling.ToolProfileRun))
	now := time.Now().UTC()
	for _, name := range eagerNames {
		state.LoadedTools[name] = LoadedToolRecord{
			Name:       name,
			LoadedAt:   now,
			LoadSource: "eager",
		}
	}
	for _, item := range deferred {
		state.DeferredTools[item.Name] = item
	}
	return state
}

func splitToolDefinitions(ctx context.Context, specs []tooling.ToolSpec) ([]string, map[string]DeferredToolRecord) {
	if len(specs) == 0 {
		return nil, nil
	}
	eager := make([]string, 0, len(specs))
	deferred := make(map[string]DeferredToolRecord)
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" || !spec.Enabled() {
			continue
		}
		switch spec.Loading.Mode {
		case tooling.ToolLoadingModeDeferred:
			deferred[name] = DeferredToolRecord{
				Name:        name,
				Reason:      spec.Loading.Reason,
				Description: toolDescription(ctx, spec),
			}
			continue
		case tooling.ToolLoadingModeHidden:
			continue
		}
		eager = append(eager, name)
	}
	sort.Strings(eager)
	return eager, deferred
}

func toolDescription(ctx context.Context, spec tooling.ToolSpec) string {
	if spec.Tool == nil {
		return ""
	}
	info, err := spec.Tool.Info(ctx)
	if err != nil || info == nil {
		return ""
	}
	return strings.TrimSpace(info.Desc)
}

func (p *defaultContextPlane) OnToolCall(ctx context.Context, event ToolCallEvent) error {
	if p == nil {
		return errors.New("context plane is not initialized")
	}
	toolName := strings.TrimSpace(event.ToolName)
	if toolName == "" {
		return errors.New("tool call event requires tool_name")
	}
	lifecycleCtx := ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil || lifecycleCtx.State == nil {
		return errors.New("tool lifecycle state is not initialized")
	}
	lifecycleCtx.State.mu.Lock()
	_, loaded := lifecycleCtx.State.LoadedTools[toolName]
	_, deferred := lifecycleCtx.State.DeferredTools[toolName]
	lifecycleCtx.State.mu.Unlock()
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

func (p *defaultContextPlane) OnToolResult(ctx context.Context, event ToolResultEvent) error {
	if p == nil {
		return errors.New("context plane is not initialized")
	}
	if strings.TrimSpace(event.ToolName) == "" {
		return errors.New("tool result event requires tool_name")
	}
	lifecycleCtx := ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil || lifecycleCtx.State == nil {
		return errors.New("tool lifecycle state is not initialized")
	}
	if strings.TrimSpace(event.CallID) == "" {
		return errors.New("tool result event requires call_id")
	}
	if strings.TrimSpace(event.RunID) == "" {
		return errors.New("tool result event requires run_id")
	}
	if p.toolResultLedger == nil {
		return errors.New("tool result ledger is not initialized")
	}
	status := store.ToolResultStatusSucceeded
	if event.IsError {
		status = store.ToolResultStatusFailed
	}
	ledgerRecord, err := p.toolResultLedger.Append(ctx, store.ToolResultAppendRequest{
		RunID:         event.RunID,
		SessionID:     event.SessionID,
		TurnIndex:     event.TurnIndex,
		CallID:        event.CallID,
		ToolName:      event.ToolName,
		ArgumentsJSON: event.Arguments,
		Status:        status,
		ErrorReason:   event.ErrorReason,
		FullText:      event.Result,
		TokenEstimate: event.ResultTokens,
		SideEffects:   append([]store.SideEffectRef(nil), event.SideEffects...),
	})
	if err != nil {
		return fmt.Errorf("append tool result ledger: %w", err)
	}
	record := ToolResultRecord{
		CallID:    strings.TrimSpace(event.CallID),
		ToolName:  strings.TrimSpace(event.ToolName),
		TurnIndex: event.TurnIndex,
		ResultRef: ledgerRecord.ResultRef,
		Summary:   ledgerRecord.Preview,
		FullText:  event.Result,
		IsError:   event.IsError,
		Prunable:  true,
	}
	updateToolResultRecord(lifecycleCtx.State, record)
	return nil
}

func (p *defaultContextPlane) DeferredLoad(ctx context.Context, req DeferredLoadRequest) (*DeferredLoadResult, error) {
	if p == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if len(req.ToolNames) == 0 && strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("deferred load requires tool_names or query")
	}
	limit := req.Limit
	if limit == 0 {
		limit = defaultDeferredLoadLimit
	}
	if limit <= 0 || limit > defaultDeferredLoadLimit {
		return nil, fmt.Errorf("deferred load limit must be between 1 and %d", defaultDeferredLoadLimit)
	}
	lifecycleCtx := ToolLifecycleContextFromContext(ctx)
	if lifecycleCtx == nil || lifecycleCtx.State == nil {
		return nil, errors.New("tool lifecycle state is not initialized")
	}
	if lifecycleCtx.Catalog == nil {
		return nil, errors.New("tool lifecycle catalog is not initialized")
	}
	lifecycleCtx.State.mu.Lock()
	selected, alreadyLoaded, err := resolveDeferredLoadTargets(lifecycleCtx, req, limit)
	if err != nil {
		lifecycleCtx.State.mu.Unlock()
		return nil, err
	}
	if len(selected) == 0 && len(alreadyLoaded) == 0 {
		lifecycleCtx.State.mu.Unlock()
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
	lifecycleCtx.State.mu.Unlock()
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

func formatPrunedToolResult(record ToolResultRecord) string {
	var b strings.Builder
	b.WriteString("[tool result pruned]\n")
	b.WriteString("tool: ")
	b.WriteString(strings.TrimSpace(record.ToolName))
	b.WriteString("\ncall_id: ")
	b.WriteString(strings.TrimSpace(record.CallID))
	if ref := strings.TrimSpace(record.ResultRef); ref != "" {
		b.WriteString("\nresult_ref: ")
		b.WriteString(ref)
	}
	b.WriteString("\nnote: original output removed from live context after turn window expiry")
	return b.String()
}

func updateToolResultRecord(state *ToolLifecycleState, record ToolResultRecord) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for i, item := range state.RecentResults {
		if item.CallID == record.CallID {
			state.RecentResults[i] = record
			return
		}
	}
	state.RecentResults = append(state.RecentResults, record)
	if state.MaxResultRefs > 0 && len(state.RecentResults) > state.MaxResultRefs {
		state.RecentResults = append([]ToolResultRecord(nil), state.RecentResults[len(state.RecentResults)-state.MaxResultRefs:]...)
	}
}

func sortedLoadedToolNames(state *ToolLifecycleState) []string {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return sortedLoadedToolNamesLocked(state)
}

func sortedLoadedToolNamesLocked(state *ToolLifecycleState) []string {
	if state == nil || len(state.LoadedTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(state.LoadedTools))
	for name := range state.LoadedTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedDeferredToolNames(state *ToolLifecycleState) []string {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return sortedDeferredToolNamesLocked(state)
}

func sortedDeferredToolNamesLocked(state *ToolLifecycleState) []string {
	if state == nil || len(state.DeferredTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(state.DeferredTools))
	for name := range state.DeferredTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
