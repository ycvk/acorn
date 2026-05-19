package crystallization

import (
	"fmt"
	"strings"
)

type Summarizer interface {
	Summarize(title, body, taskPattern string, toolNames []string) string
}

type DefaultSummarizer struct{}

func NewSummarizer() *DefaultSummarizer {
	return &DefaultSummarizer{}
}

func (s *DefaultSummarizer) Summarize(title, body, taskPattern string, toolNames []string) string {
	if s == nil {
		return ""
	}

	var parts []string
	if taskPattern != "" {
		parts = append(parts, fmt.Sprintf("Pattern: %s", taskPattern))
	}
	if len(toolNames) > 0 {
		parts = append(parts, fmt.Sprintf("Tools: %s", strings.Join(toolNames, ", ")))
	}
	if body != "" {
		summary := body
		if len(summary) > 150 {
			summary = summary[:150] + "..."
		}
		parts = append(parts, summary)
	}

	result := strings.Join(parts, "; ")
	if len(result) > 200 {
		result = result[:200]
	}
	return result
}
