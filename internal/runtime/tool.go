package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
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

type toolExecutionScheduler struct {
	resolver    tooling.ExecutionPolicyResolver
	maxParallel int
	knownTools  map[string]struct{}
}

func newToolExecutionScheduler(resolver tooling.ExecutionPolicyResolver, maxParallel int, knownTools map[string]struct{}) *toolExecutionScheduler {
	if maxParallel < 1 {
		maxParallel = 1
	}
	copied := make(map[string]struct{}, len(knownTools))
	for name := range knownTools {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			copied[trimmed] = struct{}{}
		}
	}
	return &toolExecutionScheduler{
		resolver:    resolver,
		maxParallel: maxParallel,
		knownTools:  copied,
	}
}

type classifiedCall struct {
	index    int
	toolCall schema.ToolCall
	safety   tooling.ParallelPolicy
	argsErr  string
	paths    []string
}

func pathsOverlap(left []string, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, path := range left {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	for _, path := range right {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			return true
		}
	}
	return false
}

func executionPathsFromArgs(args map[string]any, pathArg string, required bool) ([]string, error) {
	key := strings.TrimSpace(pathArg)
	if key == "" {
		return nil, nil
	}
	if args == nil {
		if required {
			return nil, fmt.Errorf("missing required %s argument", key)
		}
		return nil, nil
	}
	raw, ok := args[key]
	if !ok {
		if required {
			return nil, fmt.Errorf("missing required %s argument", key)
		}
		return nil, nil
	}
	paths, ok := normalizeExecutionPaths(raw)
	if !ok {
		if required {
			return nil, fmt.Errorf("%s argument must be a string or array of strings", key)
		}
		return nil, nil
	}
	if len(paths) == 0 {
		if required {
			return nil, fmt.Errorf("missing required %s argument", key)
		}
		return nil, nil
	}
	return paths, nil
}

func normalizeExecutionPaths(raw any) ([]string, bool) {
	switch value := raw.(type) {
	case string:
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return []string{trimmed}, true
		}
		return nil, true
	case []string:
		return executionTrimmedPaths(value), true
	case []any:
		paths := make([]string, 0, len(value))
		for _, item := range value {
			path, ok := item.(string)
			if !ok {
				return nil, false
			}
			paths = append(paths, path)
		}
		return executionTrimmedPaths(paths), true
	default:
		return nil, false
	}
}

func executionTrimmedPaths(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

const (
	// mcpNamespacePrefix is the prefix added to all MCP-sourced tool names
	// exposed to the LLM. Follows the Claude Code / OpenAI Codex convention.
	mcpNamespacePrefix = "mcp__"

	// mcpNamespaceSep separates the provider name and the original tool name
	// in the namespaced tool name.
	mcpNamespaceSep = "__"
)

// mcpToolName constructs the namespaced tool name for an MCP-sourced tool.
// Format: mcp__{provider}__{tool}
// The provider name is sanitized to be alphanumeric+dashes only.
func mcpToolName(provider, toolName string) string {
	return mcpNamespacePrefix + sanitizeMCPProviderName(provider) + mcpNamespaceSep + toolName
}

// sanitizeMCPProviderName replaces non-alphanumeric characters with dashes
// and trims leading/trailing dashes, underscores, and dots.
// Falls back to "provider" if the result is empty.
func sanitizeMCPProviderName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	result := strings.Trim(b.String(), "-_ .")
	if result == "" {
		return "provider"
	}
	return result
}

// augmentDescription appends MCP provider attribution to a tool description.
func augmentDescription(desc, provider string) string {
	if strings.TrimSpace(desc) == "" {
		return fmt.Sprintf("Provided by MCP server: %s", provider)
	}
	return desc + fmt.Sprintf("\n\nProvided by MCP server: %s", provider)
}

// mcpNamespacedTool wraps a BaseTool so that Info() returns the namespaced
// (prefixed) tool name and augmented description, while InvokableRun()
// delegates to the original tool which uses the original name for MCP RPC.
type mcpNamespacedTool struct {
	inner         einotool.BaseTool
	invokable     einotool.InvokableTool
	prefixedName  string
	augmentedDesc string
}

// newMCPNamespacedTool creates a namespace-prefixed wrapper around an MCP tool.
// The LLM sees prefixedName in the tool schema, but tool calls are routed
// through the original inner tool which preserves the original MCP name for
// the tools/call RPC.
func newMCPNamespacedTool(ctx context.Context, inner einotool.BaseTool, provider, originalToolName string) (*mcpNamespacedTool, error) {
	invokable, ok := inner.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("MCP tool %q is not invokable", originalToolName)
	}
	info, err := inner.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("read tool info for MCP namespacing: %w", err)
	}
	prefixed := mcpToolName(provider, originalToolName)
	augDesc := augmentDescription(info.Desc, provider)
	return &mcpNamespacedTool{
		inner:         inner,
		invokable:     invokable,
		prefixedName:  prefixed,
		augmentedDesc: augDesc,
	}, nil
}

func (t *mcpNamespacedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.inner.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &schema.ToolInfo{
		Name:        t.prefixedName,
		Desc:        t.augmentedDesc,
		ParamsOneOf: info.ParamsOneOf,
	}, nil
}

func (t *mcpNamespacedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}

// ToolSchemaCache detects when tool schemas change between runs.
type ToolSchemaCache struct {
	lastHashes map[string]string
	mu         sync.RWMutex
}

func NewToolSchemaCache() *ToolSchemaCache {
	return &ToolSchemaCache{
		lastHashes: make(map[string]string),
	}
}

// ComputeHash returns a SHA-256 hash of the tool name + schema JSON,
// truncated to 16 hex characters.
func (c *ToolSchemaCache) ComputeHash(toolName, schemaJSON string) string {
	h := sha256.Sum256([]byte(toolName + ":" + schemaJSON))
	return hex.EncodeToString(h[:])[:16]
}

// HasChanged returns true if the current schema hash differs from the
// last recorded hash for this tool.
func (c *ToolSchemaCache) HasChanged(toolName, currentSchemaJSON string) bool {
	current := c.ComputeHash(toolName, currentSchemaJSON)
	c.mu.RLock()
	last, ok := c.lastHashes[toolName]
	c.mu.RUnlock()
	return !ok || last != current
}

// UpdateHash records the current schema hash for the given tool.
func (c *ToolSchemaCache) UpdateHash(toolName, schemaJSON string) {
	h := c.ComputeHash(toolName, schemaJSON)
	c.mu.Lock()
	c.lastHashes[toolName] = h
	c.mu.Unlock()
}

// AnyChanged checks whether any tool in the list has a schema that
// differs from the last recorded hash. It also updates hashes for
// all tools so subsequent calls reflect the current state.
func (c *ToolSchemaCache) AnyChanged(ctx context.Context, tools []einotool.BaseTool) bool {
	anyChanged := false
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			anyChanged = true
			continue
		}
		schemaJSON := toolSchemaJSON(info)
		if c.HasChanged(info.Name, schemaJSON) {
			anyChanged = true
		}
		c.UpdateHash(info.Name, schemaJSON)
	}
	return anyChanged
}

// toolSchemaJSON serializes a ToolInfo's parameter schema to JSON.
// Returns empty string if the tool has no parameters.
func toolSchemaJSON(info *schema.ToolInfo) string {
	if info == nil {
		return ""
	}
	payload := map[string]any{
		"name": info.Name,
		"desc": info.Desc,
	}
	if info.ParamsOneOf != nil {
		schema, err := info.ToJSONSchema()
		if err != nil {
			return info.Name + ":" + info.Desc
		}
		payload["params"] = schema
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return info.Name + ":" + info.Desc
	}
	return string(data)
}

func buildCatalogSpecs(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind tooling.ToolKind,
	profiles []tooling.ToolProfile,
	tools []einotool.BaseTool,
) ([]tooling.ToolSpec, error) {
	specs := make([]tooling.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		spec, err := runtimeToolSpec(ctx, cfg, source, kind, profiles, tool)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func runtimeToolSpec(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind tooling.ToolKind,
	profiles []tooling.ToolProfile,
	tool einotool.BaseTool,
) (tooling.ToolSpec, error) {
	info, err := tool.Info(ctx)
	if err != nil {
		return tooling.ToolSpec{}, fmt.Errorf("read tool info for %s spec: %w", source, err)
	}
	if info == nil {
		return tooling.ToolSpec{}, fmt.Errorf("read tool info for %s spec: nil ToolInfo", source)
	}
	name := strings.TrimSpace(info.Name)
	if name == "" {
		return tooling.ToolSpec{}, fmt.Errorf("%s tool has empty name", source)
	}

	if localSpec, ok := tooling.ConfiguredLocalSpec(cfg, name); ok {
		localSpec.Tool = tool
		return localSpec, nil
	}

	spec := tooling.ToolSpec{
		ToolContract: tooling.ToolContract{
			Name:          name,
			Source:        source,
			Kind:          kind,
			Category:      tooling.ToolCategoryInspect,
			ResourceScope: tooling.ResourceScopeWorkspaceFile,
			Profiles:      append([]tooling.ToolProfile(nil), profiles...),
			PlanPolicy:    tooling.PlanPolicyNone,
			FactPolicy:    tooling.FactPolicySuppress,
			Loading:       tooling.EagerLoadingPolicy(),
			Execution: tooling.ToolExecutionPolicy{
				ParallelPolicy: tooling.ParallelPolicyReadOnly,
				SideEffects:    []tooling.ToolSideEffect{tooling.ToolSideEffectReadWorkspace},
			},
			Result:     tooling.InlineResultPolicy(0),
			Boundary:   tooling.ToolResultBoundaryPolicy(),
			Projection: tooling.ActivityProjectionPolicy(),
		},
		Tool: tool,
	}

	switch name {
	case "delegate_task":
		spec.Kind = tooling.ToolKindSkill
		spec.Category = tooling.ToolCategorySkill
		spec.ResourceScope = tooling.ResourceScopeSkill
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectSkillRead}
		spec.PlanPolicy = tooling.PlanPolicyRequireActivePlan
	case "load_tools":
		spec.Kind = tooling.ToolKindNative
		spec.Category = tooling.ToolCategoryInspect
		spec.ResourceScope = tooling.ResourceScopeWorkspaceFile
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectReadWorkspace}
	case "update_working_checkpoint", "clear_working_checkpoint":
		spec.Kind = tooling.ToolKindMemory
		spec.Category = tooling.ToolCategoryMemory
		spec.ResourceScope = tooling.ResourceScopeMemory
		spec.Loading = tooling.DeferredLoadingPolicy("working_state_tool")
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyNeverParallel
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectMemoryWrite}
	case "memory_search", "memory_read_file", "memory_list_files":
		spec.Kind = tooling.ToolKindMemory
		spec.Category = tooling.ToolCategoryMemory
		spec.ResourceScope = tooling.ResourceScopeMemory
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectMemoryRead}
		spec.FactPolicy = tooling.FactPolicySuppress
	case "memory_create_file", "memory_replace_span":
		spec.Kind = tooling.ToolKindMemory
		spec.Category = tooling.ToolCategoryMemory
		spec.ResourceScope = tooling.ResourceScopeMemory
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyWriteScoped
		spec.Execution.PathArg = "path"
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectMemoryWrite}
		spec.FactPolicy = tooling.FactPolicySuppress
	case "skill_list", "skill_view":
		spec.Kind = tooling.ToolKindSkill
		spec.Category = tooling.ToolCategorySkill
		spec.ResourceScope = tooling.ResourceScopeSkill
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
		spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectSkillRead}
	default:
		switch kind {
		case tooling.ToolKindMCP, tooling.ToolKindMCPResource, tooling.ToolKindMCPPrompt:
			spec.Kind = kind
			spec.Category = tooling.ToolCategoryIntegration
			spec.ResourceScope = tooling.ResourceScopeMCP
			spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
			spec.Execution.PathArg = "path"
			spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectIntegration}
			spec.FactPolicy = tooling.FactPolicyAuto
			if kind == tooling.ToolKindMCPResource || kind == tooling.ToolKindMCPPrompt {
				spec.Loading = tooling.DeferredLoadingPolicy("deferred_mcp_catalog")
			}
		default:
			spec.Category = tooling.ToolCategoryInspect
			spec.ResourceScope = tooling.ResourceScopeWorkspaceFile
			spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
			spec.Execution.PathArg = "path"
			spec.Execution.SideEffects = []tooling.ToolSideEffect{tooling.ToolSideEffectReadWorkspace}
			spec.FactPolicy = tooling.FactPolicyAuto
		}
	}
	return spec, nil
}

func mcpToolParallelPolicy(cfg *config.Config, providerName string) (tooling.ParallelPolicy, error) {
	if cfg == nil {
		return "", fmt.Errorf("resolve MCP tool safety for provider %q: config is required", strings.TrimSpace(providerName))
	}
	for _, provider := range cfg.MCP.Providers {
		if strings.TrimSpace(provider.Name) != strings.TrimSpace(providerName) {
			continue
		}
		if strings.TrimSpace(provider.ToolSafety) == "" {
			return "", fmt.Errorf("mcp provider %q must declare tool_safety", strings.TrimSpace(providerName))
		}
		return tooling.ParseParallelPolicy(provider.ToolSafety)
	}
	return "", fmt.Errorf("mcp provider %q is not configured", strings.TrimSpace(providerName))
}

type loadToolsInput struct {
	Query     string   `json:"query,omitempty"`
	ToolNames []string `json:"tool_names,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type loadToolsOutput struct {
	Messages        []string `json:"messages,omitempty"`
	LoadedToolNames []string `json:"loaded_tool_names,omitempty"`
	AlreadyLoaded   []string `json:"already_loaded,omitempty"`
}

func newLoadToolsTool(plane contextplane.ContextPlane) (einotool.BaseTool, error) {
	if plane == nil {
		return nil, errors.New("context plane is required")
	}
	return toolutils.InferTool("load_tools", "Load deferred tool definitions by query or exact tool names.", func(ctx context.Context, input loadToolsInput) (loadToolsOutput, error) {
		result, err := plane.DeferredLoad(ctx, contextplane.DeferredLoadRequest{
			RunID:     getRunID(ctx),
			SessionID: SessionIDFromContext(ctx),
			Query:     strings.TrimSpace(input.Query),
			ToolNames: append([]string(nil), input.ToolNames...),
			Limit:     input.Limit,
		})
		if err != nil {
			return loadToolsOutput{}, err
		}
		messageTexts := make([]string, 0, len(result.Messages))
		for _, msg := range result.Messages {
			if msg == nil {
				continue
			}
			messageTexts = append(messageTexts, strings.TrimSpace(msg.Content))
		}
		return loadToolsOutput{
			Messages:        messageTexts,
			LoadedToolNames: append([]string(nil), result.LoadedToolNames...),
			AlreadyLoaded:   append([]string(nil), result.AlreadyLoaded...),
		}, nil
	})
}

func isLoadToolsCall(call schema.ToolCall) bool {
	return strings.TrimSpace(call.Function.Name) == "load_tools"
}
