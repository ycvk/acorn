package contextplane

import (
	"reflect"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func appendToMessage(msg adk.Message, text string) adk.Message {
	if text == "" {
		return msg
	}
	enhanced := *msg
	if len(enhanced.UserInputMultiContent) > 0 {
		parts := make([]schema.MessageInputPart, len(enhanced.UserInputMultiContent))
		copy(parts, enhanced.UserInputMultiContent)
		if len(parts) > 0 {
			parts[0].Text = parts[0].Text + "\n\n" + text
		}
		enhanced.UserInputMultiContent = parts
	} else {
		enhanced.Content = enhanced.Content + "\n\n" + text
	}
	return &enhanced
}

var (
	// pendingItemRe matches TODO, FIXME, HACK, PENDING, and unchecked-box markers
	// in assistant messages.
	pendingItemRe = regexp.MustCompile(`(?i)(?:TODO|FIXME|HACK|PENDING|□|☐|unchecked|unfinished)[:\s]`)

	// filePathRe matches absolute or relative file paths containing at least one
	// directory separator (e.g. /foo/bar, ./baz/qux.go, src/main.go).
	filePathRe = regexp.MustCompile(`(?:^|[\s"'(])(\.{0,2}/[\w./-]+[\w.-]+|/(?:usr|etc|home|tmp|var|opt|Users|home)/[\w./-]+)`)

	// funcCallRe matches function-call patterns like funcName() or pkg.Func().
	funcCallRe = regexp.MustCompile(`\b([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)\(`)

	// errorMsgRe matches error-prefixed messages like "error: ..." or "Error: ...".
	errorMsgRe = regexp.MustCompile(`(?i)error[:\s]+(.{5,100})`)

	// lineItemRe extracts the full line containing a pending marker so we can
	// report the actionable item rather than just the keyword.
	lineItemRe = regexp.MustCompile(`(?m)^(.*(?:TODO|FIXME|HACK|PENDING|□|☐|unchecked|unfinished).*)$`)
)

const handoffFrameTailSize = 10

// buildHandoffFrame extracts current intent, pending items, and key variables
// from the last handoffFrameTailSize messages and returns a structured XML
// handoff frame string. Returns empty string if all sections resolve to their
// default "N/A" or "none" values.
func buildHandoffFrame(messages []adk.Message) string {
	if len(messages) == 0 {
		return ""
	}

	tailStart := len(messages) - handoffFrameTailSize
	if tailStart < 0 {
		tailStart = 0
	}
	tail := messages[tailStart:]

	currentIntent := extractCurrentIntent(tail)
	pendingItems := extractPendingItems(tail)
	keyVariables := extractKeyVariables(tail)

	if currentIntent == "N/A" && pendingItems == "none" && keyVariables == "none" {
		return ""
	}

	var b strings.Builder
	b.WriteString("<handoff-frame>\n")
	b.WriteString("<current-intent>")
	b.WriteString(currentIntent)
	b.WriteString("</current-intent>\n")
	b.WriteString("<pending-items>")
	b.WriteString(pendingItems)
	b.WriteString("</pending-items>\n")
	b.WriteString("<key-variables>")
	b.WriteString(keyVariables)
	b.WriteString("</key-variables>\n")
	b.WriteString("</handoff-frame>")
	return b.String()
}

func extractCurrentIntent(tail []adk.Message) string {
	for i := len(tail) - 1; i >= 0; i-- {
		msg := tail[i]
		if msg == nil || msg.Role != schema.User {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content != "" {
			if len(content) > 200 {
				return content[:200] + "..."
			}
			return content
		}
		var parts []string
		for _, part := range msg.UserInputMultiContent {
			if strings.TrimSpace(part.Text) != "" {
				parts = append(parts, strings.TrimSpace(part.Text))
			}
		}
		if len(parts) > 0 {
			joined := strings.Join(parts, " ")
			if len(joined) > 200 {
				return joined[:200] + "..."
			}
			return joined
		}
	}
	return "N/A"
}

func extractPendingItems(tail []adk.Message) string {
	var items []string
	seen := make(map[string]bool)
	for _, msg := range tail {
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		content := msg.Content
		if content == "" {
			continue
		}
		if !pendingItemRe.MatchString(content) {
			continue
		}
		matches := lineItemRe.FindAllString(content, -1)
		for _, m := range matches {
			trimmed := strings.TrimSpace(m)
			if trimmed != "" && !seen[trimmed] {
				seen[trimmed] = true
				items = append(items, trimmed)
			}
		}
	}
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, "; ")
}

func extractKeyVariables(tail []adk.Message) string {
	var paths, funcs, errs []string
	seenPath := make(map[string]bool)
	seenFunc := make(map[string]bool)
	seenErr := make(map[string]bool)

	for _, msg := range tail {
		if msg == nil {
			continue
		}
		content := msg.Content
		if content == "" {
			continue
		}
		for _, m := range filePathRe.FindAllStringSubmatch(content, -1) {
			if len(m) > 1 && !seenPath[m[1]] {
				seenPath[m[1]] = true
				paths = append(paths, m[1])
			}
		}
		for _, m := range funcCallRe.FindAllStringSubmatch(content, -1) {
			if len(m) > 1 && !seenFunc[m[1]] {
				seenFunc[m[1]] = true
				funcs = append(funcs, m[1]+"()")
			}
		}
		for _, m := range errorMsgRe.FindAllStringSubmatch(content, -1) {
			if len(m) > 1 && !seenErr[m[1]] {
				seenErr[m[1]] = true
				errs = append(errs, "error: "+m[1])
			}
		}
	}

	var parts []string
	if len(paths) > 0 {
		parts = append(parts, "paths: "+strings.Join(paths, ", "))
	}
	if len(funcs) > 0 {
		parts = append(parts, "functions: "+strings.Join(funcs, ", "))
	}
	if len(errs) > 0 {
		parts = append(parts, strings.Join(errs, "; "))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "; ")
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
