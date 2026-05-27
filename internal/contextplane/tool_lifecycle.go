package contextplane

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
)

const (
	defaultToolLifecycleMaxTurns = 2
	defaultToolLifecycleMaxRefs  = 32
)

type toolLifecycleContextKey struct{}

type ToolLifecycleContext struct {
	Ledger          store.ToolResultLedger
	State           *ToolLifecycleState
	Catalog         *tooling.Catalog
	ToolInfosByName map[string]*schema.ToolInfo
}

func WithToolLifecycleContext(ctx context.Context, ledger store.ToolResultLedger, state *ToolLifecycleState, catalog *tooling.Catalog, infos []*schema.ToolInfo) context.Context {
	infoMap := make(map[string]*schema.ToolInfo, len(infos))
	for _, info := range infos {
		if info == nil || strings.TrimSpace(info.Name) == "" {
			continue
		}
		infoMap[strings.TrimSpace(info.Name)] = info
	}
	return context.WithValue(ctx, toolLifecycleContextKey{}, &ToolLifecycleContext{
		Ledger:          ledger,
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
	lifecycleCtx.State.Mu().Lock()
	names := make([]string, 0, len(always)+len(lifecycleCtx.State.LoadedTools))
	names = append(names, always...)
	for name := range lifecycleCtx.State.LoadedTools {
		names = append(names, name)
	}
	lifecycleCtx.State.Mu().Unlock()
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
	lifecycleCtx.State.Mu().Lock()
	records := make(map[string]ToolResultRecord, len(lifecycleCtx.State.RecentResults))
	maxAgeTurns := lifecycleCtx.State.MaxAgeTurns
	for _, item := range lifecycleCtx.State.RecentResults {
		if strings.TrimSpace(item.CallID) == "" {
			continue
		}
		records[item.CallID] = item
	}
	lifecycleCtx.State.Mu().Unlock()
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
		UpdateToolResultRecord(lifecycleCtx.State, record)
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

func UpdateToolResultRecord(state *ToolLifecycleState, record ToolResultRecord) {
	if state == nil {
		return
	}
	state.Mu().Lock()
	defer state.Mu().Unlock()
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
	state.Mu().Lock()
	defer state.Mu().Unlock()
	return SortedLoadedToolNamesLocked(state)
}

func SortedLoadedToolNamesLocked(state *ToolLifecycleState) []string {
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
	state.Mu().Lock()
	defer state.Mu().Unlock()
	return SortedDeferredToolNamesLocked(state)
}

func SortedDeferredToolNamesLocked(state *ToolLifecycleState) []string {
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
