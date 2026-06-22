package tool

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
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
	validator *toolArgumentValidator
}

type ToolAuditCallIDKey struct{}

func getRunID(ctx context.Context) string {
	return runtimeapi.GetRunID(ctx)
}

func withToolAuditCallID(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, ToolAuditCallIDKey{}, strings.TrimSpace(callID))
}

func ToolAuditCallID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, ok := ctx.Value(ToolAuditCallIDKey{}).(string)
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

func (t *auditedTool) InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	return t.run(ctx, argumentsInJSON, emit, opts...)
}

func (t *auditedTool) run(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
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

func progressToolFromBase(tool einotool.BaseTool) tooling.ProgressTool {
	progress, ok := tool.(tooling.ProgressTool)
	if !ok {
		return nil
	}
	return progress
}

func (t *auditedTool) invoke(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter, opts ...einotool.Option) (string, error) {
	if t.progress != nil {
		return t.progress.InvokableRunWithProgress(ctx, argumentsInJSON, emit, opts...)
	}
	return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
}

func BuildAuditedTools(
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
func NewMCPNamespacedTool(ctx context.Context, inner einotool.BaseTool, provider, originalToolName string) (*mcpNamespacedTool, error) {
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

func BuildCatalogSpecs(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind tooling.ToolKind,
	tools []einotool.BaseTool,
) ([]tooling.ToolSpec, error) {
	specs := make([]tooling.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		spec, err := RuntimeToolSpec(ctx, cfg, source, kind, tool)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func RuntimeToolSpec(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind tooling.ToolKind,
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

	if contract, ok := tooling.BuiltinToolSpec(name, source); ok {
		return tooling.ToolSpec{ToolContract: contract, Tool: tool}, nil
	}

	spec := tooling.ToolSpec{
		ToolContract: tooling.ToolContract{
			Name:     name,
			Source:   source,
			Kind:     kind,
			Category: tooling.ToolCategoryInspect,
			Loading:  tooling.EagerLoadingPolicy(),
			Execution: tooling.ToolExecutionPolicy{
				ParallelPolicy: tooling.ParallelPolicyReadOnly,
			},
		},
		Tool: tool,
	}

	switch kind {
	case tooling.ToolKindMCP:
		spec.Kind = kind
		spec.Category = tooling.ToolCategoryIntegration
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	default:
		spec.Category = tooling.ToolCategoryInspect
		spec.Execution.ParallelPolicy = tooling.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	}
	return spec, nil
}

func MCPToolParallelPolicy(cfg *config.Config, providerName string) (tooling.ParallelPolicy, error) {
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

func NewLoadToolsTool() (einotool.BaseTool, error) {
	return toolutils.InferTool("load_tools", "Load deferred tool definitions by query or exact tool names.", func(ctx context.Context, input loadToolsInput) (loadToolsOutput, error) {
		result, err := contextplane.DeferredLoad(ctx, contextplane.DeferredLoadRequest{
			RunID:     getRunID(ctx),
			SessionID: runtimeapi.SessionIDFromContext(ctx),
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

func IsLoadToolsCall(call schema.ToolCall) bool {
	return strings.TrimSpace(call.Function.Name) == "load_tools"
}
