package memory

import (
	"fmt"
	"path/filepath"
	"strings"
)

func resolveLimit(name string, value int, fallback int, max int) (int, error) {
	if value <= 0 {
		return fallback, nil
	}
	if value > max {
		return 0, fmt.Errorf("%s limit %d exceeds max %d", name, value, max)
	}
	return value, nil
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func nonEmpty(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func compactWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func snippet(text string) string {
	compact := compactWhitespace(text)
	if len(compact) <= 180 {
		return compact
	}
	return strings.TrimSpace(compact[:177]) + "..."
}

func WorkspaceSlug(root string) string {
	base := filepath.Base(strings.TrimSpace(root))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return sanitizeName(base)
}

func WorkspaceScope(slug string) string {
	trimmed := sanitizeName(slug)
	if trimmed == "" {
		return ""
	}
	return "workspace:" + trimmed
}
