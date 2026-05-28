package clientevents

import (
	"fmt"

	"github.com/ycvk/acorn/internal/events"
)

// ProjectRunEvent converts an EventRecord into the /v1 client run event contract.
func ProjectRunEvent(record events.EventRecord) (RunEvent, error) {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return RunEvent{}, projectionError("run event payload must be object: run_id=%s sequence=%d kind=%s", record.RunID, record.Sequence, record.Kind)
	}
	data, err := ProjectRunEventData(record.Kind, payload)
	if err != nil {
		return RunEvent{}, err
	}
	return RunEvent{
		EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
		RunID:   record.RunID,
		Seq:     record.Sequence,
		TS:      record.CreatedAt,
		Type:    record.Kind,
		Data:    data,
	}, nil
}

// ProjectUnsupportedRunEvent creates an UnsupportedRunEvent for diagnostics.
func ProjectUnsupportedRunEvent(record events.EventRecord) UnsupportedRunEvent {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return UnsupportedRunEvent{
			EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
			RunID:   record.RunID,
			Seq:     record.Sequence,
			TS:      record.CreatedAt,
			Type:    record.Kind,
			Raw:     nil,
			Reason:  fmt.Sprintf("payload for %q is not an object", record.Kind),
		}
	}
	raw := cloneMap(payload)
	if _, err := ProjectRunEventData(record.Kind, payload); err != nil {
		return UnsupportedRunEvent{
			EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
			RunID:   record.RunID,
			Seq:     record.Sequence,
			TS:      record.CreatedAt,
			Type:    record.Kind,
			Raw:     raw,
			Reason:  err.Error(),
		}
	}
	return UnsupportedRunEvent{
		EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
		RunID:   record.RunID,
		Seq:     record.Sequence,
		TS:      record.CreatedAt,
		Type:    record.Kind,
		Raw:     raw,
		Reason:  fmt.Sprintf("event %q is not part of the client live contract", record.Kind),
	}
}

// ProjectRunEventData maps an event kind and payload to a typed client data struct.
func ProjectRunEventData(kind string, payload map[string]any) (any, error) {
	switch kind {
	case "run.started":
		return RunStartedData{Input: topLevelString(payload, "input")}, nil
	case "assistant.delta":
		value, ok := objectField(payload, "assistant_delta")
		if !ok {
			return nil, projectionError("assistant.delta payload missing assistant_delta object")
		}
		return AssistantDeltaData{AssistantDelta: value}, nil
	case "agent.message":
		value, ok := objectField(payload, "message")
		if !ok {
			return nil, projectionError("agent.message payload missing message object")
		}
		return AgentMessageData{Message: value}, nil
	case "tool.call.started":
		return ToolCallStartedData{ToolCall: projectToolCallPayload(payload)}, nil
	case "tool.call.progress":
		return ToolCallProgressData{ToolCall: projectToolCallPayload(payload)}, nil
	case "tool.call.succeeded":
		return ToolCallSucceededData{ToolCall: projectToolCallPayload(payload)}, nil
	case "tool.call.failed":
		return ToolCallFailedData{ToolCall: projectToolCallPayload(payload)}, nil
	case "tool.call.interrupted":
		return ToolCallInterruptedData{ToolCall: projectToolCallPayload(payload)}, nil
	case "run.completed":
		value, _ := objectField(payload, "message")
		return RunCompletedData{Message: value}, nil
	case "run.failed":
		return RunFailedData{Error: topLevelString(payload, "error")}, nil
	case "run.interrupted":
		value, _ := objectField(payload, "interrupt")
		return RunInterruptedData{Interrupt: value}, nil
	case "run.resume_requested":
		value, _ := objectField(payload, "targets")
		return RunResumeRequestedData{Targets: value}, nil
	case "elicitation.pending":
		return ElicitationPendingData{
			ActionID:        topLevelString(payload, "action_id"),
			Message:         topLevelString(payload, "message"),
			RequestedSchema: payload["requested_schema"],
		}, nil
	case "elicitation.decided":
		return ElicitationDecidedData{
			ActionID:        topLevelString(payload, "action_id"),
			Message:         topLevelString(payload, "message"),
			RequestedSchema: payload["requested_schema"],
		}, nil
	case "operator_question.pending", "operator_question.decided":
		return projectOperatorQuestionData(payload), nil
	case "provider.degraded":
		return projectProviderDegradedData(payload), nil
	case "mcp.tool_catalog_refreshed",
		"mcp.tool_catalog_refresh_failed",
		"mcp.provider_added",
		"mcp.provider_removed",
		"mcp.provider_restarted",
		"mcp.resource_catalog_refreshed",
		"mcp.resource_catalog_refresh_failed",
		"mcp.prompt_catalog_refreshed",
		"mcp.prompt_catalog_refresh_failed",
		"mcp.auth_status_changed":
		return MCPProviderLifecycleData{
			ProviderName: topLevelString(payload, "provider_name"),
			Transport:    topLevelString(payload, "transport"),
			Error:        topLevelString(payload, "error"),
			AuthStatus:   topLevelString(payload, "auth_status"),
		}, nil
	case "sampling.started", "sampling.completed", "sampling.failed":
		return SamplingData{
			RunID: topLevelString(payload, "run_id"),
			Depth: topLevelInt(payload, "depth"),
			Model: topLevelString(payload, "model"),
		}, nil
	case "decision_selected":
		return projectDecisionSelectedData(payload), nil
	case "decision_blocked":
		data := projectDecisionSelectedData(payload)
		return DecisionBlockedData(data), nil
	case "skill.discovered", "skill.selected", "skill.loaded", "skill.failed":
		value, _ := objectField(payload, "skill")
		return SkillData{Skill: value}, nil
	case "skill.lifecycle":
		value, ok := objectField(payload, "skill_lifecycle")
		if !ok {
			return nil, projectionError("skill.lifecycle payload missing skill_lifecycle object")
		}
		return SkillLifecycleData{SkillLifecycle: value}, nil
	case "procedure.activation":
		value, ok := objectField(payload, "procedure_activation")
		if !ok {
			return nil, projectionError("procedure.activation payload missing procedure_activation object")
		}
		return ProcedureActivationData{ProcedureActivation: value}, nil
	case "memory.prepared":
		value, ok := objectField(payload, "memory_prepared")
		if !ok {
			return nil, projectionError("memory.prepared payload missing memory_prepared object")
		}
		return MemoryPreparedData{MemoryPrepared: value}, nil
	case "context.pressure":
		value, ok := objectField(payload, "context_pressure")
		if !ok {
			return nil, projectionError("context.pressure payload missing context_pressure object")
		}
		return ContextPressureData{ContextPressure: value}, nil
	case "context.compressed":
		value, ok := objectField(payload, "context_compressed")
		if !ok {
			return nil, projectionError("context.compressed payload missing context_compressed object")
		}
		return ContextCompressedData{ContextCompressed: value}, nil
	case "plan.created", "plan.updated":
		value, _ := objectField(payload, "plan")
		return PlanData{Plan: value}, nil
	case "plan.cleared":
		return PlanClearedData{PlanID: topLevelString(payload, "plan_id")}, nil
	case "step.started", "step.completed", "step.failed":
		plan, _ := objectField(payload, "plan")
		step, _ := objectField(payload, "step")
		return PlanStepData{
			PlanID:    topLevelString(payload, "plan_id"),
			SessionID: topLevelString(payload, "session_id"),
			Plan:      plan,
			Step:      step,
			UpdatedAt: topLevelString(payload, "updated_at"),
			Error:     topLevelString(payload, "error"),
		}, nil
	case "subagent.started", "subagent.completed", "subagent.failed":
		return SubagentData{
			SubRunID:          topLevelString(payload, "sub_run_id"),
			ParentID:          topLevelString(payload, "parent_id"),
			SessionID:         topLevelString(payload, "session_id"),
			Depth:             topLevelInt(payload, "depth"),
			Task:              topLevelString(payload, "task"),
			ChildRunMode:      topLevelString(payload, "child_run_mode"),
			WorkspaceMode:     topLevelString(payload, "workspace_mode"),
			WorktreePath:      topLevelString(payload, "worktree_path"),
			ContextMessages:   topLevelInt(payload, "context_messages"),
			Summary:           topLevelString(payload, "summary"),
			FinalStatus:       topLevelString(payload, "final_status"),
			AcceptanceStatus:  topLevelString(payload, "acceptance_status"),
			AcceptanceReasons: stringArrayField(payload, "acceptance_reasons"),
			EvidenceRefs:      stringArrayField(payload, "evidence_refs"),
			OrchestrationMode: topLevelString(payload, "orchestration_mode"),
			ParentStepID:      topLevelString(payload, "parent_step_id"),
			Error:             topLevelString(payload, "error"),
		}, nil
	default:
		return nil, projectionError("unsupported live run event kind %q", kind)
	}
}

func projectProviderDegradedData(payload map[string]any) ProviderDegradedData {
	items, ok := payload["affected_providers"].([]any)
	if !ok {
		return ProviderDegradedData{}
	}
	providers := make([]ProviderDegradedEntryData, 0, len(items))
	for _, item := range items {
		provider, ok := item.(map[string]any)
		if !ok {
			continue
		}
		providers = append(providers, ProviderDegradedEntryData{
			Name:      topLevelString(provider, "name"),
			Transport: topLevelString(provider, "transport"),
			Error:     topLevelString(provider, "error"),
		})
	}
	return ProviderDegradedData{AffectedProviders: providers}
}

func projectOperatorQuestionData(payload map[string]any) OperatorQuestionData {
	return OperatorQuestionData{
		ActionID:         topLevelString(payload, "action_id"),
		Question:         topLevelString(payload, "question"),
		Options:          pendingActionOptionsFromAny(payload["options"]),
		AllowFreeform:    topLevelBool(payload, "allow_freeform"),
		Decision:         topLevelString(payload, "decision"),
		SelectedOptionID: topLevelString(payload, "selected_option_id"),
		Answer:           topLevelString(payload, "answer"),
	}
}

func projectDecisionSelectedData(payload map[string]any) DecisionSelectedData {
	return DecisionSelectedData{
		Action:              topLevelString(payload, "action"),
		Intent:              topLevelString(payload, "intent"),
		SelectedSkillID:     topLevelString(payload, "selected_skill_id"),
		DecisionReason:      topLevelString(payload, "decision_reason"),
		DecisionProfileHash: topLevelString(payload, "decision_profile_hash"),
		ExplicitSkillID:     topLevelString(payload, "explicit_skill_id"),
	}
}

func projectToolCallPayload(payload map[string]any) map[string]any {
	if value, ok := objectField(payload, "tool_call"); ok {
		return value
	}
	out := cloneMap(payload)
	if _, hasName := out["name"]; !hasName {
		if toolName := topLevelString(payload, "tool_name"); toolName != "" {
			out["name"] = toolName
		}
	}
	return out
}
