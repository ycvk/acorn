package clientevents

import (
	"encoding/json"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/skills"
)

func DeriveSessionState(latestRun *events.RunRecord, hasDegradedProvider bool) SessionState {
	if latestRun == nil {
		return SessionStateNew
	}
	switch latestRun.Status {
	case events.RunStatusSucceeded:
		if hasDegradedProvider {
			return SessionStateDegraded
		}
		return SessionStateCompleted
	case events.RunStatusFailed:
		return SessionStateFailed
	case events.RunStatusRunning:
		return SessionStateRunning
	case events.RunStatusInterrupted:
		if hasDegradedProvider {
			return SessionStateDegraded
		}
		return SessionStateInterrupted
	default:
		return SessionStateDegraded
	}
}

func BuildTraceSummary(raw []events.EventRecord) *TraceSummary {
	summary := &TraceSummary{ItemCount: len(raw)}
	assistantDeltaMessageIDs := make(map[string]struct{})
	for _, event := range raw {
		kind := traceKindFromEventKind(event.Kind)
		summary.LastKind = kind
		switch kind {
		case "assistant.delta":
			summary.AssistantDeltaCount++
			payload := eventPayloadMap(event.Payload)
			if delta, ok := objectField(payload, "assistant_delta"); ok {
				summary.AssistantDeltaCharCount += len([]rune(topLevelString(delta, "delta")))
				messageID := topLevelString(delta, "message_id")
				if messageID != "" {
					assistantDeltaMessageIDs[messageID] = struct{}{}
				}
			}
		case "assistant_message":
			summary.AssistantMessageCount++
		case "tool_call_started", "tool_call_succeeded", "tool_call_failed", "tool_call_interrupted":
			summary.ToolCallCount++
		case "decision_selected":
			summary.DecisionEventCount++
			summary.DecisionSelected = true
		case "decision_blocked":
			summary.DecisionEventCount++
			summary.DecisionBlocked = true
		case "skill_discovered", "skill_selected", "skill_loaded", "skill_failed", "skill.lifecycle":
			summary.SkillEventCount++
			if kind == "skill_selected" {
				summary.SkillSelected = true
			}
		case "run_interrupted":
			summary.Interrupted = true
		case "run_failed":
			summary.Failed = true
		case "run_completed":
			summary.Completed = true
		case "plan.created", "plan.updated", "plan.cleared", "step.started", "step.completed", "step.failed":
			summary.PlanEventCount++
		}
	}
	summary.AssistantDeltaMessageCount = len(assistantDeltaMessageIDs)
	return summary
}

func SelectedSkillFromEvents(raw []events.EventRecord) *SelectedSkill {
	for i := len(raw) - 1; i >= 0; i-- {
		event := raw[i]
		if event.Kind != "skill.loaded" && event.Kind != "skill.selected" {
			continue
		}
		payload := eventPayloadMap(event.Payload)
		skillPayload, ok := objectField(payload, "skill")
		if !ok {
			continue
		}
		selectedID := topLevelString(skillPayload, "selected_id")
		if selectedID == "" {
			continue
		}
		requirements := skills.Requirements{}
		if reqPayload, ok := objectField(skillPayload, "requirements"); ok {
			requirements = skills.Requirements{
				Tools:    stringArrayField(reqPayload, "tools"),
				Toolsets: stringArrayField(reqPayload, "toolsets"),
				Bins:     stringArrayField(reqPayload, "bins"),
				Env:      stringArrayField(reqPayload, "env"),
			}
		}
		return &SelectedSkill{
			Skill: skills.Spec{
				ID:           selectedID,
				Name:         firstNonEmpty(topLevelString(skillPayload, "name"), selectedID),
				Summary:      topLevelString(skillPayload, "summary"),
				Instruction:  topLevelString(skillPayload, "instruction"),
				Source:       topLevelString(skillPayload, "source"),
				Origin:       skills.Origin(topLevelString(skillPayload, "origin")),
				TaskPattern:  topLevelString(skillPayload, "task_pattern"),
				PromotedFrom: topLevelString(skillPayload, "promoted_from"),
				Path:         topLevelString(skillPayload, "path"),
				Scripts:      stringArrayField(skillPayload, "scripts"),
				Requires:     requirements,
			},
			Score:        topLevelInt(skillPayload, "score"),
			MatchedTerms: stringArrayField(skillPayload, "matched_terms"),
		}
	}
	return nil
}

func traceKindFromEventKind(eventKind string) string {
	switch eventKind {
	case "run.started":
		return "run_started"
	case "run.completed":
		return "run_completed"
	case "run.failed":
		return "run_failed"
	case "run.interrupted":
		return "run_interrupted"
	case "run.resume_requested":
		return "run_resume_requested"
	case "decision_selected":
		return "decision_selected"
	case "decision_blocked":
		return "decision_blocked"
	case "skill.discovered":
		return "skill_discovered"
	case "skill.selected":
		return "skill_selected"
	case "skill.loaded":
		return "skill_loaded"
	case "skill.failed":
		return "skill_failed"
	case "skill.lifecycle":
		return "skill.lifecycle"
	case "memory.prepared":
		return "memory_prepared"
	case "context.pressure":
		return "context_pressure"
	case "context.compressed":
		return "context_compressed"
	case "assistant.delta":
		return "assistant.delta"
	case "agent.message":
		return "assistant_message"
	case "tool.call.started":
		return "tool_call_started"
	case "tool.call.progress":
		return "tool_call_progress"
	case "tool.call.succeeded":
		return "tool_call_succeeded"
	case "tool.call.failed":
		return "tool_call_failed"
	case "tool.call.interrupted":
		return "tool_call_interrupted"
	case "mcp.tool_catalog_refreshed":
		return "mcp.tool_catalog_refreshed"
	case "mcp.tool_catalog_refresh_failed":
		return "mcp.tool_catalog_refresh_failed"
	case "mcp.provider_added":
		return "mcp.provider_added"
	case "mcp.provider_removed":
		return "mcp.provider_removed"
	case "mcp.provider_restarted":
		return "mcp.provider_restarted"
	case "mcp.resource_catalog_refreshed":
		return "mcp.resource_catalog_refreshed"
	case "mcp.resource_catalog_refresh_failed":
		return "mcp.resource_catalog_refresh_failed"
	case "mcp.prompt_catalog_refreshed":
		return "mcp.prompt_catalog_refreshed"
	case "mcp.prompt_catalog_refresh_failed":
		return "mcp.prompt_catalog_refresh_failed"
	case "mcp.auth_status_changed":
		return "mcp.auth_status_changed"
	case "subagent.started":
		return "subagent.started"
	case "subagent.completed":
		return "subagent.completed"
	case "subagent.failed":
		return "subagent.failed"
	case "plan.created":
		return "plan.created"
	case "plan.updated":
		return "plan.updated"
	case "plan.cleared":
		return "plan.cleared"
	case "step.started":
		return "step.started"
	case "step.completed":
		return "step.completed"
	case "step.failed":
		return "step.failed"
	default:
		return eventKind
	}
}

func eventPayloadMap(payload any) map[string]any {
	if payload == nil {
		return nil
	}
	if value, ok := payload.(map[string]any); ok {
		return value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
