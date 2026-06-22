package toolset

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

func BuildMemoryFileTools(ctx context.Context, memory memorymodule.Service) ([]einotool.BaseTool, error) {
	if memory == nil {
		return nil, fmt.Errorf("memory service is required")
	}
	catalog, err := buildMemoryToolCatalog(ctx, memory)
	if err != nil {
		return nil, err
	}
	return collectMemoryFileTools(ctx, memory, catalog)
}

func buildMemoryToolCatalog(ctx context.Context, memory memorymodule.Service) (*tools.Catalog, error) {
	trimmedRoot := strings.TrimSpace(memory.Root())
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
	catalog, err := tools.BuildCatalog(tools.CatalogConfig{Workspace: ws, MutationEnabled: true}, nil)
	if err != nil {
		return nil, fmt.Errorf("build memory tools: %w", err)
	}
	return catalog, nil
}

func collectMemoryFileTools(ctx context.Context, memory memorymodule.Service, catalog *tools.Catalog) ([]einotool.BaseTool, error) {
	searchTool, err := newMemorySearchTool(memory)
	if err != nil {
		return nil, err
	}
	rememberTool, err := newMemoryRememberTool(memory)
	if err != nil {
		return nil, err
	}
	result := []einotool.BaseTool{searchTool, rememberTool}
	for _, item := range catalog.Tools {
		wrapped, ok, err := wrapMemoryFileTool(ctx, memory, item)
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, wrapped)
		}
	}
	return result, nil
}

type memoryNamespacedTool struct {
	inner        einotool.BaseTool
	invokable    einotool.InvokableTool
	memory       memorymodule.Service
	name         string
	description  string
	originalName string
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
	case "read_file", "list_files", "create_file", "replace_span":
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
		description:  strings.TrimSpace(info.Desc + "\n\nThis tool operates on Acorn's memory root, not the workspace."),
		originalName: name,
	}, true, nil
}

func (t *memoryNamespacedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.inner.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &schema.ToolInfo{Name: t.name, Desc: t.description, ParamsOneOf: info.ParamsOneOf}, nil
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

func (t *memoryNamespacedTool) runMemoryCreateFile(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	var input tools.CreateFileInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_create_file arguments: %w", err)
	}
	result, err := t.memory.ApplyMemoryMutation(ctx, memorymodule.PlanMemoryMutationRequest{Path: input.Path, Content: input.Content})
	if err != nil {
		return "", err
	}
	if rejected := memoryMutationRejection(result); rejected != "" {
		return "", fmt.Errorf("memory mutation rejected: %s", rejected)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal memory_create_file result: %w", err)
	}
	return string(body), nil
}

func (t *memoryNamespacedTool) runMemoryReplaceSpan(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	var input tools.ReplaceSpanInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse memory_replace_span arguments: %w", err)
	}
	existingBytes, err := os.ReadFile(filepath.Join(t.memory.Root(), input.Path))
	if err != nil {
		return "", fmt.Errorf("read existing file for replace_span: %w", err)
	}
	replaced, err := applyLineRangeReplacement(string(existingBytes), input.StartLine, input.EndLine, input.Replacement)
	if err != nil {
		return "", fmt.Errorf("apply line range replacement: %w", err)
	}
	result, err := t.memory.ApplyMemoryMutation(ctx, memorymodule.PlanMemoryMutationRequest{Path: input.Path, Content: replaced})
	if err != nil {
		return "", err
	}
	if rejected := memoryMutationRejection(result); rejected != "" {
		return "", fmt.Errorf("memory mutation rejected: %s", rejected)
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal memory_replace_span result: %w", err)
	}
	return string(body), nil
}

func memoryMutationRejection(result *memorymodule.MemoryMutationResult) string {
	if result == nil || result.MutationPlan == nil {
		return ""
	}
	if result.MutationPlan.Action == memorymodule.MemoryMutationRejectInvalid {
		return result.MutationPlan.Reason
	}
	return ""
}

func applyLineRangeReplacement(content string, startLine, endLine int, replacement string) (string, error) {
	lines := strings.Split(content, "\n")
	if startLine < 1 || startLine > len(lines) {
		return "", fmt.Errorf("start_line %d out of range (1-%d)", startLine, len(lines))
	}
	if endLine < 1 || endLine > len(lines) {
		return "", fmt.Errorf("end_line %d out of range (1-%d)", endLine, len(lines))
	}
	if endLine < startLine {
		return "", fmt.Errorf("end_line %d < start_line %d", endLine, startLine)
	}
	var result []string
	result = append(result, lines[:startLine-1]...)
	result = append(result, strings.Split(replacement, "\n")...)
	result = append(result, lines[endLine:]...)
	return strings.Join(result, "\n"), nil
}
