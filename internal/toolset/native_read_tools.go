package toolset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/toolkit"
)

const (
	defaultReadFileMaxBytes = 16000
	defaultListFilesLimit   = 200
	defaultSearchTextLimit  = 100
	defaultGitDiffMaxBytes  = 32000
)

var skippedReadOnlyDirectoryNames = map[string]struct{}{
	".git":         {},
	".acorn":       {},
	".planning":    {},
	"bin":          {},
	"build":        {},
	"coverage":     {},
	"dist":         {},
	"node_modules": {},
	"vendor":       {},
}

func buildReadFileTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("read_file", "Read a workspace file with explicit line-range and preview controls.", func(ctx context.Context, input ReadFileInput, emit toolkit.ToolProgressEmitter) (ReadFileOutput, error) {
		return runReadFile(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build read_file tool: %w", err)
	}
	return tool, nil
}

func runReadFile(ctx context.Context, ws WorkspaceView, input ReadFileInput, emit toolkit.ToolProgressEmitter) (ReadFileOutput, error) {
	if strings.TrimSpace(input.Path) == "" {
		return ReadFileOutput{}, errors.New("path is required")
	}
	resolved, err := ws.ResolveReadPath(input.Path)
	if err != nil {
		return ReadFileOutput{}, err
	}
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("resolved %s", filepath.ToSlash(resolved))); err != nil {
		return ReadFileOutput{}, err
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return ReadFileOutput{}, fmt.Errorf("read file %s: %w", resolved, err)
	}
	return readResolvedFile(ctx, resolved, body, input, emit)
}

func readResolvedFile(ctx context.Context, resolved string, body []byte, input ReadFileInput, emit toolkit.ToolProgressEmitter) (ReadFileOutput, error) {
	totalLines := countLines(body)
	startLine, endLine, err := normalizeLineRange(totalLines, input.StartLine, input.EndLine)
	if err != nil {
		return ReadFileOutput{}, err
	}
	content, lineBytes, err := extractLineRange(body, startLine, endLine)
	if err != nil {
		return ReadFileOutput{}, err
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultReadFileMaxBytes
	}
	preview, truncated := previewBytes([]byte(content), maxBytes)
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("read %s lines %d-%d of %d (%d bytes)", filepath.ToSlash(resolved), startLine, endLine, totalLines, lineBytes)); err != nil {
		return ReadFileOutput{}, err
	}
	return ReadFileOutput{
		Path:       resolved,
		StartLine:  startLine,
		EndLine:    endLine,
		TotalLines: totalLines,
		Bytes:      lineBytes,
		Truncated:  truncated,
		Content:    preview,
	}, nil
}

func buildListFilesTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("list_files", "List workspace files and directories under an optional path prefix.", func(ctx context.Context, input ListFilesInput, emit toolkit.ToolProgressEmitter) (ListFilesOutput, error) {
		return runListFiles(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build list_files tool: %w", err)
	}
	return tool, nil
}

func runListFiles(ctx context.Context, ws WorkspaceView, input ListFilesInput, emit toolkit.ToolProgressEmitter) (ListFilesOutput, error) {
	basePath, baseRel, err := resolveListBase(ws, input.Path)
	if err != nil {
		return ListFilesOutput{}, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultListFilesLimit
	}
	state := &listFilesState{entries: make([]ListFileEntry, 0, minInt(limit, 32))}
	err = filepath.WalkDir(basePath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current != basePath && entry.IsDir() {
			if shouldSkipReadOnlyDir(entry.Name()) {
				return filepath.SkipDir
			}
		}
		if current == basePath {
			return nil
		}
		return visitListFile(ctx, ws, basePath, current, entry, input.Pattern, state, limit, emit)
	})
	if err != nil {
		return ListFilesOutput{}, fmt.Errorf("list workspace files: %w", err)
	}
	return listFilesOutput(ws, baseRel, input, state), nil
}

type listFilesState struct {
	entries []ListFileEntry
	total   int
}

func visitListFile(ctx context.Context, ws WorkspaceView, basePath, current string, entry fs.DirEntry, pattern string, state *listFilesState, limit int, emit toolkit.ToolProgressEmitter) error {
	if current == basePath {
		return nil
	}
	rel, err := ws.RelativePath(current)
	if err != nil {
		return err
	}
	if !matchesWorkspacePattern(rel, pattern) {
		return nil
	}
	state.total++
	if len(state.entries) >= limit {
		return nil
	}
	info, err := entry.Info()
	if err != nil {
		return err
	}
	return appendListEntry(ctx, state, rel, entry, info, emit)
}

func appendListEntry(ctx context.Context, state *listFilesState, rel string, entry fs.DirEntry, info fs.FileInfo, emit toolkit.ToolProgressEmitter) error {
	entryPath := filepath.ToSlash(rel)
	state.entries = append(state.entries, ListFileEntry{
		Path:  entryPath,
		IsDir: entry.IsDir(),
		Size:  info.Size(),
	})
	kind := "file"
	if entry.IsDir() {
		kind = "dir"
	}
	return emitToolProgress(ctx, emit, fmt.Sprintf("%s %s", kind, entryPath))
}

func listFilesOutput(ws WorkspaceView, baseRel string, input ListFilesInput, state *listFilesState) ListFilesOutput {
	return ListFilesOutput{
		RootPath:  ws.Root(),
		Path:      filepath.ToSlash(baseRel),
		Pattern:   strings.TrimSpace(input.Pattern),
		Total:     state.total,
		Truncated: state.total > len(state.entries),
		Entries:   state.entries,
	}
}

func resolveListBase(ws WorkspaceView, value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return ws.Root(), "", nil
	}
	resolved, err := ws.ResolveReadPath(value)
	if err != nil {
		return "", "", err
	}
	rel, err := ws.RelativePath(resolved)
	if err != nil {
		return "", "", err
	}
	return resolved, rel, nil
}

func walkWorkspaceFiles(basePath string, visit func(current string, entry fs.DirEntry) error) error {
	return filepath.WalkDir(basePath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if current != basePath && shouldSkipReadOnlyDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		return visit(current, entry)
	})
}

func shouldSkipReadOnlyDir(name string) bool {
	_, ok := skippedReadOnlyDirectoryNames[name]
	return ok
}

func countLines(body []byte) int {
	if len(body) == 0 {
		return 1
	}
	return bytes.Count(body, []byte{'\n'}) + 1
}

func lineStartOffsets(body []byte) []int {
	offsets := []int{0}
	for i, ch := range body {
		if ch == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func normalizeLineRange(totalLines int, startLine int, endLine int) (int, int, error) {
	if totalLines <= 0 {
		totalLines = 1
	}
	if startLine <= 0 {
		startLine = 1
	}
	if endLine <= 0 {
		endLine = totalLines
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

func extractLineRange(body []byte, startLine int, endLine int) (string, int, error) {
	offsets := lineStartOffsets(body)
	totalLines := len(offsets)
	startLine, endLine, err := normalizeLineRange(totalLines, startLine, endLine)
	if err != nil {
		return "", 0, err
	}
	startByte := offsets[startLine-1]
	endByte := len(body)
	if endLine < totalLines {
		endByte = offsets[endLine]
	}
	segment := body[startByte:endByte]
	return string(segment), len(segment), nil
}

func previewBytes(body []byte, limit int) (string, bool) {
	if limit <= 0 || len(body) <= limit {
		return string(body), false
	}
	return string(body[:limit]), true
}
