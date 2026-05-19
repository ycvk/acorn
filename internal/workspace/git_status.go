package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type GitStatusEntry struct {
	Path           string
	IndexStatus    string
	WorktreeStatus string
}

type WorkspaceGitStatus struct {
	WorkspaceRoot string
	Branch        string
	Clean         bool
	Entries       []GitStatusEntry
}

func (w *Workspace) InspectGitStatus(ctx context.Context, scopedPath string) (*WorkspaceGitStatus, error) {
	if w == nil {
		return nil, errors.New("workspace is required")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, errors.New("git is required for workspace git inspection")
	}

	args := []string{"status", "--short", "--branch"}
	if strings.TrimSpace(scopedPath) != "" {
		relPath, err := w.NormalizeRelativePath(scopedPath)
		if err != nil {
			return nil, err
		}
		args = append(args, "--", filepath.ToSlash(relPath))
	}

	output, err := runGitStatusCommand(ctx, w.rootDir, args...)
	if err != nil {
		return nil, err
	}

	result := &WorkspaceGitStatus{
		WorkspaceRoot: w.rootDir,
		Clean:         true,
	}
	for _, line := range splitGitStatusLines(output) {
		if strings.HasPrefix(line, "## ") {
			result.Branch = strings.TrimPrefix(line, "## ")
			continue
		}
		if len(line) < 3 {
			continue
		}
		result.Clean = false
		result.Entries = append(result.Entries, GitStatusEntry{
			IndexStatus:    strings.TrimSpace(line[:1]),
			WorktreeStatus: strings.TrimSpace(line[1:2]),
			Path:           strings.TrimSpace(line[3:]),
		})
	}
	return result, nil
}

func runGitStatusCommand(ctx context.Context, root string, args ...string) (string, error) {
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

func splitGitStatusLines(body string) []string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
