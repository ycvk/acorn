package tool

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/toolkit"
)

type toolExecutionScheduler struct {
	resolver    toolkit.ExecutionPolicyResolver
	maxParallel int
	knownTools  map[string]struct{}
}

func newToolExecutionScheduler(resolver toolkit.ExecutionPolicyResolver, maxParallel int, knownTools map[string]struct{}) *toolExecutionScheduler {
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
	safety   toolkit.ParallelPolicy
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
