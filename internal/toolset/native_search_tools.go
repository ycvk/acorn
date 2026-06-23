package toolset

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

	"github.com/ycvk/acorn/internal/toolkit"
)

func buildSearchTextTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("search_text", "Search workspace text files without dropping to raw command execution.", func(ctx context.Context, input SearchTextInput, emit toolkit.ToolProgressEmitter) (SearchTextOutput, error) {
		return runSearchText(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build search_text tool: %w", err)
	}
	return tool, nil
}

func runSearchText(ctx context.Context, ws WorkspaceView, input SearchTextInput, emit toolkit.ToolProgressEmitter) (SearchTextOutput, error) {
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

func searchWorkspaceFiles(ctx context.Context, ws WorkspaceView, basePath, baseRel, query string, input SearchTextInput, matcher func(string) (int, bool), limit int, emit toolkit.ToolProgressEmitter) (SearchTextOutput, error) {
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

func scanSearchFile(ctx context.Context, ws WorkspaceView, current string, matcher func(string) (int, bool), state *searchState, limit int, emit toolkit.ToolProgressEmitter) error {
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

func collectSearchMatches(ctx context.Context, rel string, body []byte, matcher func(string) (int, bool), state *searchState, limit int, emit toolkit.ToolProgressEmitter) error {
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
