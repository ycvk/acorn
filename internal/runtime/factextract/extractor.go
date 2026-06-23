package factextract

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxFactLen = 200

// ExtractSemanticFact produces a human-readable fact from a tool execution.
// It extracts meaningful information from tool name, arguments, and output instead
// of fabricating generic success when the payload cannot be summarized.
func ExtractSemanticFact(toolName, argumentsJSON, output string) string {
	switch toolName {
	case "read_file":
		return extractReadFileFact(argumentsJSON, output)
	case "list_files":
		return extractListFilesFact(argumentsJSON, output)
	case "search_text":
		return extractSearchTextFact(argumentsJSON, output)
	case "inspect_git_status":
		return extractGitStatusFact(output)
	case "inspect_git_diff":
		return extractGitDiffFact(argumentsJSON)
	case "create_file":
		return extractCreateFileFact(argumentsJSON, output)
	case "replace_span":
		return extractReplaceSpanFact(argumentsJSON)
	case "apply_unified_patch":
		return extractApplyPatchFact(argumentsJSON)
	case "run_command":
		return extractRunCommandFact(argumentsJSON, output)
	default:
		return truncateFact(fmt.Sprintf("tool %s produced an unclassified result", toolName))
	}
}

func extractReadFileFact(argumentsJSON, output string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil || args.Path == "" {
		return invalidToolArgumentsFact("read_file")
	}
	firstLine := firstNonEmptyLine(output)
	if firstLine == "" {
		return truncateFact(fmt.Sprintf("file %s exists (empty)", args.Path))
	}
	return truncateFact(fmt.Sprintf("file %s exists, starts: %s", args.Path, firstLine))
}

func extractListFilesFact(argumentsJSON, output string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return invalidToolArgumentsFact("list_files")
	}
	var result struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(output), &result); err == nil {
		scope := strings.TrimSpace(args.Path)
		if scope == "" {
			scope = "."
		}
		return truncateFact(fmt.Sprintf("listed %d workspace path(s) under %s", result.Total, scope))
	}
	return unsummarizedToolResultFact("list_files")
}

func extractSearchTextFact(argumentsJSON, output string) string {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil || args.Query == "" {
		return invalidToolArgumentsFact("search_text")
	}
	var result struct {
		Matches []any `json:"matches"`
	}
	if err := json.Unmarshal([]byte(output), &result); err == nil {
		return truncateFact(fmt.Sprintf("search %q returned %d match(es)", args.Query, len(result.Matches)))
	}
	return unsummarizedToolResultFact("search_text")
}

func extractGitStatusFact(output string) string {
	var result struct {
		Clean   bool  `json:"clean"`
		Entries []any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(output), &result); err == nil {
		if result.Clean {
			return truncateFact("git status is clean")
		}
		return truncateFact(fmt.Sprintf("git status has %d changed path(s)", len(result.Entries)))
	}
	return unsummarizedToolResultFact("inspect_git_status")
}

func extractGitDiffFact(argumentsJSON string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return invalidToolArgumentsFact("inspect_git_diff")
	}
	if strings.TrimSpace(args.Path) == "" {
		return truncateFact("git diff inspected workspace changes")
	}
	return truncateFact(fmt.Sprintf("git diff inspected %s", args.Path))
}

func extractCreateFileFact(argumentsJSON, output string) string {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil || args.Path == "" {
		return invalidToolArgumentsFact("create_file")
	}
	byteCount := len(args.Content)
	var result struct {
		Bytes int `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(output), &result); err == nil && result.Bytes > 0 {
		byteCount = result.Bytes
	}
	return truncateFact(fmt.Sprintf("created file %s (%d bytes)", args.Path, byteCount))
}

func extractReplaceSpanFact(argumentsJSON string) string {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil || args.Path == "" {
		return invalidToolArgumentsFact("replace_span")
	}
	return truncateFact(fmt.Sprintf("replaced lines %d-%d in %s", args.StartLine, args.EndLine, args.Path))
}

func extractApplyPatchFact(argumentsJSON string) string {
	var args struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil || len(args.Paths) == 0 {
		return invalidToolArgumentsFact("apply_unified_patch")
	}
	return truncateFact(fmt.Sprintf("applied patch to %s", strings.Join(args.Paths, ", ")))
}

func extractRunCommandFact(argumentsJSON, output string) string {
	var args struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil || len(args.Command) == 0 {
		return invalidToolArgumentsFact("run_command")
	}
	cmdStr := strings.Join(args.Command, " ")
	exitCode := 0
	var result struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(output), &result); err == nil {
		exitCode = result.ExitCode
	}
	return truncateFact(fmt.Sprintf("command %q exited %d", cmdStr, exitCode))
}

func invalidToolArgumentsFact(toolName string) string {
	return truncateFact(fmt.Sprintf("%s result has invalid arguments", toolName))
}

func unsummarizedToolResultFact(toolName string) string {
	return truncateFact(fmt.Sprintf("%s result could not be summarized", toolName))
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateFact(s string) string {
	if len(s) <= maxFactLen {
		return s
	}
	return s[:maxFactLen-3] + "..."
}
