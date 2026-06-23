package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
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
	tool, err := inferProgressTool("read_file", "Read a workspace file with explicit line-range and preview controls.", func(ctx context.Context, input ReadFileInput, emit ToolProgressEmitter) (ReadFileOutput, error) {
		return runReadFile(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build read_file tool: %w", err)
	}
	return tool, nil
}

func runReadFile(ctx context.Context, ws WorkspaceView, input ReadFileInput, emit ToolProgressEmitter) (ReadFileOutput, error) {
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

func readResolvedFile(ctx context.Context, resolved string, body []byte, input ReadFileInput, emit ToolProgressEmitter) (ReadFileOutput, error) {
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
	tool, err := inferProgressTool("list_files", "List workspace files and directories under an optional path prefix.", func(ctx context.Context, input ListFilesInput, emit ToolProgressEmitter) (ListFilesOutput, error) {
		return runListFiles(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build list_files tool: %w", err)
	}
	return tool, nil
}

func runListFiles(ctx context.Context, ws WorkspaceView, input ListFilesInput, emit ToolProgressEmitter) (ListFilesOutput, error) {
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

func visitListFile(ctx context.Context, ws WorkspaceView, basePath, current string, entry fs.DirEntry, pattern string, state *listFilesState, limit int, emit ToolProgressEmitter) error {
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

func appendListEntry(ctx context.Context, state *listFilesState, rel string, entry fs.DirEntry, info fs.FileInfo, emit ToolProgressEmitter) error {
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

func buildSearchTextTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("search_text", "Search workspace text files without dropping to raw command execution.", func(ctx context.Context, input SearchTextInput, emit ToolProgressEmitter) (SearchTextOutput, error) {
		return runSearchText(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build search_text tool: %w", err)
	}
	return tool, nil
}

func runSearchText(ctx context.Context, ws WorkspaceView, input SearchTextInput, emit ToolProgressEmitter) (SearchTextOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return SearchTextOutput{}, errors.New("query is required")
	}
	basePath, baseRel, err := resolveListBase(ws, input.Path)
	if err != nil {
		return SearchTextOutput{}, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultSearchTextLimit
	}
	matcher, err := buildTextMatcher(query, input.Regex, input.CaseSensitive)
	if err != nil {
		return SearchTextOutput{}, err
	}
	return searchWorkspaceFiles(ctx, ws, basePath, baseRel, query, input, matcher, limit, emit)
}

func searchWorkspaceFiles(ctx context.Context, ws WorkspaceView, basePath, baseRel, query string, input SearchTextInput, matcher func(string) (int, bool), limit int, emit ToolProgressEmitter) (SearchTextOutput, error) {
	state := &searchState{matches: make([]SearchTextMatch, 0, minInt(limit, 16))}
	err := walkWorkspaceFiles(basePath, func(current string, entry fs.DirEntry) error {
		return scanSearchFile(ctx, ws, current, matcher, state, limit, emit)
	})
	if err != nil {
		return SearchTextOutput{}, fmt.Errorf("search workspace text: %w", err)
	}
	return searchOutput(query, baseRel, input, state, limit), nil
}

type searchState struct {
	matches                []SearchTextMatch
	scannedFileCount       int
	skippedBinaryFileCount int
}

func scanSearchFile(ctx context.Context, ws WorkspaceView, current string, matcher func(string) (int, bool), state *searchState, limit int, emit ToolProgressEmitter) error {
	if len(state.matches) >= limit {
		return nil
	}
	body, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	if isBinaryFile(body) {
		state.skippedBinaryFileCount++
		return nil
	}
	state.scannedFileCount++
	rel, err := ws.RelativePath(current)
	if err != nil {
		return err
	}
	return collectSearchMatches(ctx, rel, body, matcher, state, limit, emit)
}

func collectSearchMatches(ctx context.Context, rel string, body []byte, matcher func(string) (int, bool), state *searchState, limit int, emit ToolProgressEmitter) error {
	lineNo := 1
	for _, line := range splitLinesForSearch(body) {
		column, ok := matcher(line)
		if ok {
			match := SearchTextMatch{
				Path:     filepath.ToSlash(rel),
				Line:     lineNo,
				Column:   column,
				LineText: line,
			}
			state.matches = append(state.matches, match)
			if err := emitToolProgress(ctx, emit, fmt.Sprintf("%s:%d:%d %s", match.Path, match.Line, match.Column, match.LineText)); err != nil {
				return err
			}
			if len(state.matches) >= limit {
				break
			}
		}
		lineNo++
	}
	return nil
}

func searchOutput(query, baseRel string, input SearchTextInput, state *searchState, limit int) SearchTextOutput {
	return SearchTextOutput{
		Query:                  query,
		Path:                   filepath.ToSlash(baseRel),
		Regex:                  input.Regex,
		ScannedFileCount:       state.scannedFileCount,
		SkippedBinaryFileCount: state.skippedBinaryFileCount,
		Truncated:              len(state.matches) >= limit,
		Matches:                state.matches,
	}
}

func buildTextMatcher(query string, regex bool, caseSensitive bool) (func(line string) (int, bool), error) {
	if regex {
		return buildRegexMatcher(query, caseSensitive)
	}
	return buildLiteralMatcher(query, caseSensitive), nil
}

func buildRegexMatcher(query string, caseSensitive bool) (func(line string) (int, bool), error) {
	pattern := query
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile regex %q: %w", query, err)
	}
	return func(line string) (int, bool) {
		loc := re.FindStringIndex(line)
		if loc == nil {
			return 0, false
		}
		return loc[0] + 1, true
	}, nil
}

func buildLiteralMatcher(query string, caseSensitive bool) func(line string) (int, bool) {
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}
	return func(line string) (int, bool) {
		haystack := line
		if !caseSensitive {
			haystack = strings.ToLower(line)
		}
		index := strings.Index(haystack, needle)
		if index < 0 {
			return 0, false
		}
		return index + 1, true
	}
}

func matchesWorkspacePattern(relPath string, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true
	}
	normalized := filepath.ToSlash(relPath)
	if strings.ContainsAny(pattern, "*?[") {
		if ok, err := path.Match(pattern, normalized); err == nil && ok {
			return true
		}
		base := path.Base(normalized)
		ok, err := path.Match(pattern, base)
		if err != nil {
			return false
		}
		return ok
	}
	return strings.Contains(normalized, pattern)
}

func splitLinesForSearch(body []byte) []string {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	return strings.Split(text, "\n")
}

func isBinaryFile(body []byte) bool {
	sample := body
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	return bytes.IndexByte(sample, 0) >= 0
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
