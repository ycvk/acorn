package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"github.com/ycvk/acorn/internal/workspace"
)

func buildInspectGitStatusTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := toolutils.InferTool("inspect_git_status", "Inspect git status for the workspace or an optional scoped path.", func(ctx context.Context, input InspectGitStatusInput) (InspectGitStatusOutput, error) {
		status, err := ws.InspectGitStatus(ctx, input.Path)
		if err != nil {
			return InspectGitStatusOutput{}, err
		}
		return inspectGitStatusOutput(status), nil
	})
	if err != nil {
		return nil, fmt.Errorf("build inspect_git_status tool: %w", err)
	}
	return tool, nil
}

func inspectGitStatusOutput(status *workspace.WorkspaceGitStatus) InspectGitStatusOutput {
	result := InspectGitStatusOutput{
		RootPath: status.WorkspaceRoot,
		Branch:   status.Branch,
		Clean:    status.Clean,
	}
	for _, entry := range status.Entries {
		result.Entries = append(result.Entries, GitStatusEntry{
			Path:           entry.Path,
			IndexStatus:    entry.IndexStatus,
			WorktreeStatus: entry.WorktreeStatus,
		})
	}
	return result
}

func buildInspectGitDiffTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := toolutils.InferTool("inspect_git_diff", "Inspect git diff output for the workspace or an optional scoped path.", func(ctx context.Context, input InspectGitDiffInput) (InspectGitDiffOutput, error) {
		return runInspectGitDiff(ctx, ws, input)
	})
	if err != nil {
		return nil, fmt.Errorf("build inspect_git_diff tool: %w", err)
	}
	return tool, nil
}

func runInspectGitDiff(ctx context.Context, ws WorkspaceView, input InspectGitDiffInput) (InspectGitDiffOutput, error) {
	if input.ContextLines < 0 {
		return InspectGitDiffOutput{}, errors.New("context_lines must be >= 0")
	}
	args, err := gitDiffArgs(ws, input)
	if err != nil {
		return InspectGitDiffOutput{}, err
	}
	output, err := runGitCommand(ctx, ws.Root(), args...)
	if err != nil {
		return InspectGitDiffOutput{}, err
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultGitDiffMaxBytes
	}
	preview, truncated := previewBytes([]byte(output), maxBytes)
	return InspectGitDiffOutput{
		RootPath:  ws.Root(),
		Path:      filepath.ToSlash(strings.TrimSpace(input.Path)),
		Cached:    input.Cached,
		Truncated: truncated,
		Diff:      preview,
	}, nil
}

func gitDiffArgs(ws WorkspaceView, input InspectGitDiffInput) ([]string, error) {
	args := []string{"diff", "--no-ext-diff", fmt.Sprintf("--unified=%d", input.ContextLines)}
	if input.Cached {
		args = append(args, "--cached")
	}
	if strings.TrimSpace(input.Path) == "" {
		return args, nil
	}
	relPath, err := normalizeScopedRelativePath(ws, input.Path)
	if err != nil {
		return nil, err
	}
	args = append(args, "--", relPath)
	return args, nil
}

func runGitCommand(ctx context.Context, root string, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", errors.New("git is required for native git inspection tools")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if stderrText == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), stderrText)
	}
	return stdout.String(), nil
}

func normalizeScopedRelativePath(ws WorkspaceView, value string) (string, error) {
	resolved, err := ws.ResolveReadPath(value)
	if err != nil {
		return "", err
	}
	rel, err := ws.RelativePath(resolved)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
