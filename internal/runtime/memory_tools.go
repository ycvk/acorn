package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
)

type memoryNamespacedTool struct {
	inner        einotool.BaseTool
	invokable    einotool.InvokableTool
	memory       memorymodule.Service
	name         string
	description  string
	originalName string
}

func buildMemoryFileTools(ctx context.Context, memory memorymodule.Service) ([]einotool.BaseTool, error) {
	if memory == nil {
		return nil, fmt.Errorf("memory service is required")
	}
	root := memory.Root()
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return nil, fmt.Errorf("memory root is required")
	}
	ws, err := workspace.New(workspace.Config{
		RootDir:    trimmedRoot,
		StorageDir: filepath.Join(trimmedRoot, ".index"),
	})
	if err != nil {
		return nil, fmt.Errorf("build memory workspace: %w", err)
	}
	catalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, delegateTaskBridge{})
	if err != nil {
		return nil, fmt.Errorf("build memory tools: %w", err)
	}
	tools := catalog.Tools
	result := make([]einotool.BaseTool, 0, len(tools))
	for _, item := range tools {
		wrapped, ok, err := wrapMemoryFileTool(ctx, memory, item)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		result = append(result, wrapped)
	}
	return result, nil
}

func wrapMemoryFileTool(ctx context.Context, memory memorymodule.Service, inner einotool.BaseTool) (*memoryNamespacedTool, bool, error) {
	info, err := inner.Info(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("read memory tool info: %w", err)
	}
	if info == nil {
		return nil, false, fmt.Errorf("read memory tool info: nil ToolInfo")
	}
	name := strings.TrimSpace(info.Name)
	switch name {
	case "read_file", "list_files", "search_text", "create_file", "replace_span":
	default:
		return nil, false, nil
	}
	invokable, ok := inner.(einotool.InvokableTool)
	if !ok {
		return nil, false, fmt.Errorf("memory tool %q is not invokable", name)
	}
	return &memoryNamespacedTool{
		inner:        inner,
		invokable:    invokable,
		memory:       memory,
		name:         "memory_" + name,
		description:  strings.TrimSpace(info.Desc + "\n\nThis tool operates on Acorn's memory root, not the project workspace."),
		originalName: name,
	}, true, nil
}

func (t *memoryNamespacedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.inner.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.description,
		ParamsOneOf: info.ParamsOneOf,
	}, nil
}

func (t *memoryNamespacedTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	switch t.originalName {
	case "create_file":
		return t.runMemoryCreateFile(ctx, argumentsInJSON, opts...)
	case "replace_span":
		return t.runMemoryReplaceSpan(ctx, argumentsInJSON, opts...)
	default:
		return t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
	}
}

func (t *memoryNamespacedTool) runMemoryCreateFile(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var input tools.CreateFileInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_create_file arguments: %w", err)
	}
	plan, err := t.memory.PlanMemoryMutation(ctx, memorymodule.PlanMemoryMutationRequest{
		Path:    input.Path,
		Content: input.Content,
	})
	if err != nil {
		return "", err
	}
	if plan.Action == memorymodule.MemoryMutationRejectInvalid {
		return "", fmt.Errorf("memory mutation rejected: %s", plan.Reason)
	}
	if plan.Action == memorymodule.MemoryMutationNoopDuplicate {
		return memoryNoopMutationOutput(plan)
	}
	if plan.Action != memorymodule.MemoryMutationCreate {
		return "", fmt.Errorf("memory_create_file cannot execute mutation plan action %q: %s", plan.Action, plan.Reason)
	}
	output, err := t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		return "", err
	}
	return attachMemoryMutationPlan(output, plan)
}

func (t *memoryNamespacedTool) runMemoryReplaceSpan(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var input tools.ReplaceSpanInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_replace_span arguments: %w", err)
	}
	content, err := finalMemoryReplaceSpanContent(t.memory.Root(), input)
	if err != nil {
		return "", err
	}
	plan, err := t.memory.PlanMemoryMutation(ctx, memorymodule.PlanMemoryMutationRequest{
		Path:    input.Path,
		Content: content,
	})
	if err != nil {
		return "", err
	}
	if plan.Action == memorymodule.MemoryMutationRejectInvalid {
		return "", fmt.Errorf("memory mutation rejected: %s", plan.Reason)
	}
	if plan.Action == memorymodule.MemoryMutationNoopDuplicate {
		return memoryNoopMutationOutput(plan)
	}
	switch plan.Action {
	case memorymodule.MemoryMutationReplaceExisting, memorymodule.MemoryMutationRetireExisting:
	default:
		return "", fmt.Errorf("memory_replace_span cannot execute mutation plan action %q: %s", plan.Action, plan.Reason)
	}
	output, err := t.invokable.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		return "", err
	}
	return attachMemoryMutationPlan(output, plan)
}

func attachMemoryMutationPlan(output string, plan *memorymodule.MemoryMutationPlan) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return "", fmt.Errorf("parse memory tool output: %w", err)
	}
	payload["mutation_plan"] = plan
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal memory tool output: %w", err)
	}
	return string(body), nil
}

func memoryNoopMutationOutput(plan *memorymodule.MemoryMutationPlan) (string, error) {
	body, err := json.Marshal(map[string]any{
		"message":       string(memorymodule.MemoryMutationNoopDuplicate),
		"mutation_plan": plan,
	})
	if err != nil {
		return "", fmt.Errorf("marshal memory noop output: %w", err)
	}
	return string(body), nil
}

func finalMemoryReplaceSpanContent(root string, input tools.ReplaceSpanInput) (string, error) {
	if strings.TrimSpace(input.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if input.StartLine <= 0 || input.EndLine <= 0 {
		return "", fmt.Errorf("start_line and end_line must be > 0")
	}
	resolved, err := resolveMemoryToolPath(root, input.Path)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read memory file %s: %w", resolved, err)
	}
	offsets := memoryLineStartOffsets(body)
	totalLines := len(offsets)
	startLine, endLine, err := normalizeMemoryLineRange(totalLines, input.StartLine, input.EndLine)
	if err != nil {
		return "", err
	}
	startByte := offsets[startLine-1]
	endByte := len(body)
	if endLine < totalLines {
		endByte = offsets[endLine]
	}
	replaced := append([]byte(nil), body[:startByte]...)
	replaced = append(replaced, []byte(input.Replacement)...)
	replaced = append(replaced, body[endByte:]...)
	return string(replaced), nil
}

func resolveMemoryToolPath(root string, path string) (string, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return "", fmt.Errorf("memory root is required")
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("memory path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(trimmed))
	if clean == "." || strings.HasPrefix(filepath.ToSlash(clean), "../") || clean == ".." {
		return "", fmt.Errorf("memory path must stay inside the memory root")
	}
	return filepath.Join(trimmedRoot, clean), nil
}

func memoryLineStartOffsets(body []byte) []int {
	offsets := []int{0}
	for i, ch := range body {
		if ch == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func normalizeMemoryLineRange(totalLines int, startLine int, endLine int) (int, int, error) {
	if totalLines <= 0 {
		totalLines = 1
	}
	if startLine > endLine {
		return 0, 0, fmt.Errorf("start_line %d must be <= end_line %d", startLine, endLine)
	}
	if startLine > totalLines {
		return 0, 0, fmt.Errorf("start_line %d exceeds file line count %d", startLine, totalLines)
	}
	if endLine > totalLines {
		return 0, 0, fmt.Errorf("end_line %d exceeds file line count %d", endLine, totalLines)
	}
	return startLine, endLine, nil
}
