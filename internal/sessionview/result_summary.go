package sessionview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

func summaryEventRisk(kind string, payload map[string]any) string {
	for _, key := range []string{"error", "summary"} {
		if value := compactContinuationText(summaryString(payload[key]), 180); value != "" {
			return fmt.Sprintf("%s: %s", kind, value)
		}
	}
	return kind
}


// ResultSummary is the projected, deduplicated outcome of a run, ready to be
// rendered into assistant message parts.
type ResultSummary struct {
	Changed     []string
	Verified    []string
	Risks       []string
	Disclosures []DisclosureItem
	Reasoning   string
}

type resultSummaryBuilder struct {
	changed   map[string]struct{}
	verified  map[string]struct{}
	risks     map[string]struct{}
	memory    *DisclosureItem
	skill     *DisclosureItem
	reasoning string
}

// BuildResultSummary projects the loaded runtime artifacts of a run into a
// ResultSummary. It is pure: callers provide the already-loaded events.
func BuildResultSummary(records []events.EventRecord) (ResultSummary, error) {
	builder := newResultSummaryBuilder()
	if err := builder.addEvents(records); err != nil {
		return ResultSummary{}, err
	}
	return builder.summary(), nil
}

func newResultSummaryBuilder() *resultSummaryBuilder {
	return &resultSummaryBuilder{
		changed:  make(map[string]struct{}),
		verified: make(map[string]struct{}),
		risks:    make(map[string]struct{}),
	}
}

func (b *resultSummaryBuilder) addEvents(records []events.EventRecord) error {
	for _, record := range records {
		if !summaryKnownEvent(record.Kind) {
			continue
		}
		payload, err := summaryPayload(record)
		if err != nil {
			return err
		}
		switch record.Kind {
		case "subagent.completed":
			if summary := strings.TrimSpace(summaryString(payload["summary"])); summary != "" {
				b.addVerified(summary)
			}
		case "subagent.failed":
			b.addRisk(summaryEventRisk(record.Kind, payload))
		case "memory.prepared":
			if err := b.addMemoryDisclosure(record.Sequence, payload); err != nil {
				return err
			}
		case "skill.selected", "skill.loaded":
			if err := b.addSkillDisclosure(record.Sequence, record.Kind, payload); err != nil {
				return err
			}
		case "agent.message", "run.completed":
			b.addReasoning(payload)
		}
	}
	return nil
}



func (b *resultSummaryBuilder) summary() ResultSummary {
	disclosures := make([]DisclosureItem, 0, 2)
	if b.memory != nil {
		disclosures = append(disclosures, *b.memory)
	}
	if b.skill != nil {
		disclosures = append(disclosures, *b.skill)
	}
	return ResultSummary{
		Changed:     summarySortedKeys(b.changed),
		Verified:    summarySortedKeys(b.verified),
		Risks:       summarySortedKeys(b.risks),
		Disclosures: disclosures,
		Reasoning:   b.reasoning,
	}
}

func (b *resultSummaryBuilder) addChanged(items ...string) {
	summaryAddTrimmed(b.changed, items...)
}

func (b *resultSummaryBuilder) addVerified(items ...string) {
	summaryAddTrimmed(b.verified, items...)
}

func (b *resultSummaryBuilder) addRisk(items ...string) {
	summaryAddTrimmed(b.risks, items...)
}

func (b *resultSummaryBuilder) addReasoning(payload map[string]any) {
	if strings.TrimSpace(b.reasoning) != "" {
		return
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		return
	}
	reasoning := strings.TrimSpace(summaryString(message["reasoning"]))
	if reasoning == "" {
		return
	}
	b.reasoning = reasoning
}

func (b *resultSummaryBuilder) addMemoryDisclosure(sequence int64, payload map[string]any) error {
	if b.memory != nil {
		return nil
	}
	prepared, ok := payload["memory_prepared"].(map[string]any)
	if !ok {
		return fmt.Errorf("build session result summary: event sequence=%d memory.prepared missing memory_prepared object", sequence)
	}
	count := summaryInt(prepared["entry_count"])
	if count <= 0 {
		if entries, ok := prepared["entries"].([]any); ok {
			count = len(entries)
		}
	}
	if count <= 0 {
		return nil
	}
	label := fmt.Sprintf("Prepared %d memory entries", count)
	if count == 1 {
		label = "Prepared 1 memory entry"
	}
	detail := strings.TrimSpace(summaryString(prepared["workspace_scope"]))
	item := DisclosureItem{
		Kind:   "memory",
		Label:  label,
		Detail: detail,
		Tone:   "memory",
	}
	b.memory = &item
	return nil
}

func (b *resultSummaryBuilder) addSkillDisclosure(sequence int64, kind string, payload map[string]any) error {
	if b.skill != nil {
		return nil
	}
	skill, ok := payload["skill"].(map[string]any)
	if !ok {
		return fmt.Errorf("build session result summary: event sequence=%d %s missing skill object", sequence, kind)
	}
	name := summaryFirstString(skill, "name", "selected_id")
	if name == "" {
		return fmt.Errorf("build session result summary: event sequence=%d %s missing skill name", sequence, kind)
	}
	skillID := strings.TrimSpace(summaryString(skill["selected_id"]))
	label := "Used skill"
	tone := "skill"
	if summaryString(skill["origin"]) == "distilled" {
		label = "Used procedure"
		tone = "procedure"
	}
	item := DisclosureItem{
		Kind:    "skill",
		Label:   label,
		Detail:  name,
		Tone:    tone,
		SkillID: skillID,
	}
	b.skill = &item
	return nil
}

func summaryKnownEvent(kind string) bool {
	switch kind {
	case "subagent.completed",
		"subagent.failed",
		"memory.prepared",
		"skill.selected",
		"skill.loaded",
		"agent.message",
		"run.completed":
		return true
	default:
		return false
	}
}

func summaryPayload(record events.EventRecord) (map[string]any, error) {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("build session result summary: event payload must be object run_id=%s sequence=%d kind=%s", record.RunID, record.Sequence, record.Kind)
	}
	return payload, nil
}


func summaryCommand(toolName string, argumentsJSON string) (string, error) {
	if strings.TrimSpace(toolName) != "run_command" {
		return "", nil
	}
	if strings.TrimSpace(argumentsJSON) == "" {
		return "", nil
	}
	var payload struct {
		Command any `json:"command"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return "", err
	}
	return summaryCommandValue(payload.Command), nil
}

func summaryCommandValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		return summaryCommandText(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, summaryString(item))
		}
		return summaryCommandText(parts)
	default:
		return ""
	}
}

func summaryCommandText(command []string) string {
	parts := make([]string, 0, len(command))
	for _, item := range command {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

func summaryMutationTool(toolName string) bool {
	name := strings.TrimSpace(toolName)
	switch name {
	case "create_file", "write_file", "edit_file", "delete_file", "move_file", "rename_file", "apply_patch":
		return true
	}
	for _, token := range []string{"create", "write", "edit", "delete", "move", "rename", "patch"} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func summaryPathsFromArguments(argumentsJSON string) ([]string, error) {
	if strings.TrimSpace(argumentsJSON) == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return nil, err
	}
	paths := make([]string, 0, 4)
	for _, key := range []string{"path", "file_path", "target", "root_dir", "work_dir"} {
		if value := strings.TrimSpace(summaryString(payload[key])); value != "" {
			paths = append(paths, value)
		}
	}
	paths = append(paths, summaryStringSlice(payload["paths"])...)
	return paths, nil
}

func summaryString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func summaryFirstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(summaryString(payload[key])); value != "" {
			return value
		}
	}
	return ""
}

func summaryInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func summaryStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, summaryString(item))
		}
		return out
	default:
		return nil
	}
}

func summaryAddTrimmed(target map[string]struct{}, items ...string) {
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			target[trimmed] = struct{}{}
		}
	}
}

func summarySortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactContinuationText(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return trimmed
	}
	return string(runes[:limit]) + "..."
}
