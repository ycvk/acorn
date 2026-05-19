package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

type sessionMessageResultSummary struct {
	changed     []string
	verified    []string
	risks       []string
	disclosures []SessionDisclosureItem
	reasoning   string
}

type sessionMessageResultSummaryBuilder struct {
	changed   map[string]struct{}
	verified  map[string]struct{}
	risks     map[string]struct{}
	memory    *SessionDisclosureItem
	skill     *SessionDisclosureItem
	reasoning string
}

func (s *Store) buildSessionMessageResultSummary(ctx context.Context, runID string) (sessionMessageResultSummary, error) {
	records, err := s.LoadEvents(ctx, runID)
	if err != nil {
		return sessionMessageResultSummary{}, fmt.Errorf("build session result summary: load events: %w", err)
	}
	builder := newSessionMessageResultSummaryBuilder()
	if err := builder.addEvents(records); err != nil {
		return sessionMessageResultSummary{}, err
	}

	plan, err := s.LoadPlanByRun(ctx, runID)
	if err != nil && !errors.Is(err, ErrPlanNotFound) {
		return sessionMessageResultSummary{}, fmt.Errorf("build session result summary: load plan: %w", err)
	}
	if plan != nil {
		builder.addPlan(plan)
	}
	return builder.summary(), nil
}

func newSessionMessageResultSummaryBuilder() *sessionMessageResultSummaryBuilder {
	return &sessionMessageResultSummaryBuilder{
		changed:  make(map[string]struct{}),
		verified: make(map[string]struct{}),
		risks:    make(map[string]struct{}),
	}
}

func (b *sessionMessageResultSummaryBuilder) addEvents(records []events.EventRecord) error {
	for _, record := range records {
		if !sessionSummaryKnownEvent(record.Kind) {
			continue
		}
		payload, err := sessionSummaryPayload(record)
		if err != nil {
			return err
		}
		toolName := sessionSummaryToolName(payload)
		args := strings.TrimSpace(sessionSummaryString(payload["arguments_json"]))

		switch record.Kind {
		case "tool.call.succeeded":
			if command, err := sessionSummaryCommand(toolName, args); err != nil {
				return fmt.Errorf("build session result summary: event sequence=%d command: %w", record.Sequence, err)
			} else if command != "" {
				b.addVerified(command)
			}
			if sessionSummaryMutationTool(toolName) {
				paths, err := sessionSummaryPathsFromArguments(args)
				if err != nil {
					return fmt.Errorf("build session result summary: event sequence=%d paths: %w", record.Sequence, err)
				}
				b.addChanged(paths...)
			}
		case "tool.call.failed", "tool.call.interrupted":
			b.addRisk(sessionSummaryToolRisk(toolName, payload))
		case "subagent.completed":
			if summary := strings.TrimSpace(sessionSummaryString(payload["summary"])); summary != "" {
				b.addVerified(summary)
			}
		case "subagent.failed":
			b.addRisk(sessionSummaryEventRisk(record.Kind, payload))
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

func (b *sessionMessageResultSummaryBuilder) addPlan(plan *PlanRecord) {
	for _, step := range plan.Steps {
		for _, item := range step.Evidence {
			status := strings.TrimSpace(item.Status)
			kind := strings.TrimSpace(item.Kind)
			if status == "failed" {
				b.addRisk(sessionSummaryPlanRisk(item))
				continue
			}
			switch kind {
			case "diff":
				b.addChanged(item.Paths...)
			}
			if status != "passed" && status != "confirmed" {
				continue
			}
			switch kind {
			case "command", "test":
				if command := sessionSummaryCommandText(item.Command); command != "" {
					b.addVerified(command)
				} else {
					b.addVerified(item.Summary)
				}
			case "manual", "subagent":
				b.addVerified(item.Summary)
			}
		}
	}
}

func (b *sessionMessageResultSummaryBuilder) summary() sessionMessageResultSummary {
	disclosures := make([]SessionDisclosureItem, 0, 2)
	if b.memory != nil {
		disclosures = append(disclosures, *b.memory)
	}
	if b.skill != nil {
		disclosures = append(disclosures, *b.skill)
	}
	return sessionMessageResultSummary{
		changed:     sessionSummarySortedKeys(b.changed),
		verified:    sessionSummarySortedKeys(b.verified),
		risks:       sessionSummarySortedKeys(b.risks),
		disclosures: disclosures,
		reasoning:   b.reasoning,
	}
}

func (b *sessionMessageResultSummaryBuilder) addChanged(items ...string) {
	sessionSummaryAddTrimmed(b.changed, items...)
}

func (b *sessionMessageResultSummaryBuilder) addVerified(items ...string) {
	sessionSummaryAddTrimmed(b.verified, items...)
}

func (b *sessionMessageResultSummaryBuilder) addRisk(items ...string) {
	sessionSummaryAddTrimmed(b.risks, items...)
}

func (b *sessionMessageResultSummaryBuilder) addReasoning(payload map[string]any) {
	if strings.TrimSpace(b.reasoning) != "" {
		return
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		return
	}
	reasoning := strings.TrimSpace(sessionSummaryString(message["reasoning"]))
	if reasoning == "" {
		return
	}
	b.reasoning = reasoning
}

func (b *sessionMessageResultSummaryBuilder) addMemoryDisclosure(sequence int64, payload map[string]any) error {
	if b.memory != nil {
		return nil
	}
	prepared, ok := payload["memory_prepared"].(map[string]any)
	if !ok {
		return fmt.Errorf("build session result summary: event sequence=%d memory.prepared missing memory_prepared object", sequence)
	}
	count := sessionSummaryInt(prepared["entry_count"])
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
	detail := strings.TrimSpace(sessionSummaryString(prepared["workspace_scope"]))
	item := SessionDisclosureItem{
		Kind:   "memory",
		Label:  label,
		Detail: detail,
		Tone:   "memory",
	}
	b.memory = &item
	return nil
}

func (b *sessionMessageResultSummaryBuilder) addSkillDisclosure(sequence int64, kind string, payload map[string]any) error {
	if b.skill != nil {
		return nil
	}
	skill, ok := payload["skill"].(map[string]any)
	if !ok {
		return fmt.Errorf("build session result summary: event sequence=%d %s missing skill object", sequence, kind)
	}
	name := sessionSummaryFirstString(skill, "name", "selected_id")
	if name == "" {
		return fmt.Errorf("build session result summary: event sequence=%d %s missing skill name", sequence, kind)
	}
	skillID := strings.TrimSpace(sessionSummaryString(skill["selected_id"]))
	label := "Used skill"
	tone := "skill"
	if sessionSummaryString(skill["origin"]) == "distilled" {
		label = "Used procedure"
		tone = "procedure"
	}
	item := SessionDisclosureItem{
		Kind:    "skill",
		Label:   label,
		Detail:  name,
		Tone:    tone,
		SkillID: skillID,
	}
	b.skill = &item
	return nil
}

func sessionSummaryKnownEvent(kind string) bool {
	switch kind {
	case "tool.call.succeeded",
		"tool.call.failed",
		"tool.call.interrupted",
		"subagent.completed",
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

func sessionSummaryPayload(record events.EventRecord) (map[string]any, error) {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("build session result summary: event payload must be object run_id=%s sequence=%d kind=%s", record.RunID, record.Sequence, record.Kind)
	}
	return payload, nil
}

func sessionSummaryToolName(payload map[string]any) string {
	for _, key := range []string{"tool_name", "name"} {
		if value := strings.TrimSpace(sessionSummaryString(payload[key])); value != "" {
			return value
		}
	}
	toolCall, ok := payload["tool_call"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"tool_name", "name"} {
		if value := strings.TrimSpace(sessionSummaryString(toolCall[key])); value != "" {
			return value
		}
	}
	return ""
}

func sessionSummaryToolRisk(toolName string, payload map[string]any) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "tool"
	}
	for _, key := range []string{"error", "reason"} {
		if value := compactContinuationText(sessionSummaryString(payload[key]), 180); value != "" {
			return fmt.Sprintf("%s failed: %s", name, value)
		}
	}
	return fmt.Sprintf("%s failed", name)
}

func sessionSummaryEventRisk(kind string, payload map[string]any) string {
	for _, key := range []string{"error", "summary"} {
		if value := compactContinuationText(sessionSummaryString(payload[key]), 180); value != "" {
			return fmt.Sprintf("%s: %s", kind, value)
		}
	}
	return kind
}

func sessionSummaryPlanRisk(item PlanEvidence) string {
	if errText := compactContinuationText(item.Error, 180); errText != "" {
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			return fmt.Sprintf("%s: %s", summary, errText)
		}
		return errText
	}
	if summary := strings.TrimSpace(item.Summary); summary != "" {
		return summary
	}
	if command := sessionSummaryCommandText(item.Command); command != "" {
		return command
	}
	if kind := strings.TrimSpace(item.Kind); kind != "" {
		return kind + " failed"
	}
	return "plan evidence failed"
}

func sessionSummaryCommand(toolName string, argumentsJSON string) (string, error) {
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
	return sessionSummaryCommandValue(payload.Command), nil
}

func sessionSummaryCommandValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		return sessionSummaryCommandText(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, sessionSummaryString(item))
		}
		return sessionSummaryCommandText(parts)
	default:
		return ""
	}
}

func sessionSummaryCommandText(command []string) string {
	parts := make([]string, 0, len(command))
	for _, item := range command {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

func sessionSummaryMutationTool(toolName string) bool {
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

func sessionSummaryPathsFromArguments(argumentsJSON string) ([]string, error) {
	if strings.TrimSpace(argumentsJSON) == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return nil, err
	}
	paths := make([]string, 0, 4)
	for _, key := range []string{"path", "file_path", "target", "root_dir", "work_dir"} {
		if value := strings.TrimSpace(sessionSummaryString(payload[key])); value != "" {
			paths = append(paths, value)
		}
	}
	paths = append(paths, sessionSummaryStringSlice(payload["paths"])...)
	return paths, nil
}

func sessionSummaryString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func sessionSummaryFirstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(sessionSummaryString(payload[key])); value != "" {
			return value
		}
	}
	return ""
}

func sessionSummaryInt(value any) int {
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

func sessionSummaryStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, sessionSummaryString(item))
		}
		return out
	default:
		return nil
	}
}

func sessionSummaryAddTrimmed(target map[string]struct{}, items ...string) {
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			target[trimmed] = struct{}{}
		}
	}
}

func sessionSummarySortedKeys(values map[string]struct{}) []string {
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
