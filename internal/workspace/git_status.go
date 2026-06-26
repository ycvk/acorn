package workspace

import (
	"context"
	"errors"
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

	output, err := w.gitOutput(ctx, args...)
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
