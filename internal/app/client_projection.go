package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	"github.com/ycvk/acorn/internal/runtime"
)

func (s *ClientService) projectThread(record events.SessionRecord, latestRun *events.RunRecord) (Thread, error) {
	thread := Thread{
		ID:            record.SessionID,
		Title:         record.Title,
		WorkspaceRoot: s.workspaceRoot,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		State:         string(runtime.SessionStateNew),
	}
	if latestRun == nil {
		return thread, nil
	}
	state, err := projectThreadState(latestRun.Status)
	if err != nil {
		return Thread{}, err
	}
	thread.LatestRunID = latestRun.RunID
	thread.State = state
	return thread, nil
}

func projectMessage(record events.SessionMessageRecord) (Message, error) {
	switch record.Role {
	case "user", "assistant", "system", "tool":
	default:
		return Message{}, projectionError("message %d has unsupported role %q", record.ID, record.Role)
	}
	parts, err := projectMessageParts(record)
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID:       fmt.Sprintf("%d", record.ID),
		ThreadID: record.SessionID,
		Role:     record.Role,
		Content: MessageContent{
			Type:  "text",
			Text:  record.Content,
			Parts: parts,
		},
		CreatedAt: record.CreatedAt,
		RunID:     record.RunID,
	}, nil
}

func projectMessageParts(record events.SessionMessageRecord) ([]MessagePart, error) {
	if len(record.ContentParts) == 0 {
		return nil, nil
	}
	var parts []MessagePart
	if err := json.Unmarshal(record.ContentParts, &parts); err != nil {
		return nil, projectionError("message %d has invalid content_parts: %v", record.ID, err)
	}
	for index, part := range parts {
		if err := validateMessagePart(part); err != nil {
			return nil, projectionError("message %d content_parts[%d]: %v", record.ID, index, err)
		}
	}
	return parts, nil
}

func validateMessagePart(part MessagePart) error {
	switch part.Kind {
	case "text":
		if strings.TrimSpace(part.Text) == "" {
			return errors.New("text part requires text")
		}
	case "reasoning":
		if strings.TrimSpace(part.Reasoning) == "" {
			return errors.New("reasoning part requires reasoning")
		}
	case "work_status":
		switch part.Status {
		case "working", "interrupted", "failed":
		default:
			return fmt.Errorf("work_status part has unsupported status %q", part.Status)
		}
		if strings.TrimSpace(part.Title) == "" || strings.TrimSpace(part.Summary) == "" {
			return errors.New("work_status part requires title and summary")
		}
		if err := validateMessageAction(part.Action); err != nil {
			return fmt.Errorf("work_status part action: %w", err)
		}
	case "decision":
		if strings.TrimSpace(part.DecisionID) == "" || strings.TrimSpace(part.Question) == "" {
			return errors.New("decision part requires decision_id and question")
		}
		switch part.Status {
		case "", string(events.PendingActionStatusPending), string(events.PendingActionStatusApproved), string(events.PendingActionStatusRejected), string(events.PendingActionStatusResolved):
		default:
			return fmt.Errorf("decision part has unsupported status %q", part.Status)
		}
	case "result":
		if strings.TrimSpace(part.Title) == "" {
			return errors.New("result part requires title")
		}
	case "disclosure":
		if len(part.Items) == 0 {
			return errors.New("disclosure part requires items")
		}
		for index, item := range part.Items {
			if err := validateDisclosureItem(item); err != nil {
				return fmt.Errorf("disclosure part items[%d]: %w", index, err)
			}
		}
	case "technical_detail_link":
		if strings.TrimSpace(part.RunID) == "" && strings.TrimSpace(part.DetailRunID) == "" {
			return errors.New("technical_detail_link part requires run_id")
		}
	default:
		return fmt.Errorf("unsupported kind %q", part.Kind)
	}
	return nil
}

func validateDisclosureItem(item DisclosureItem) error {
	switch item.Kind {
	case "memory", "skill":
	default:
		return fmt.Errorf("unsupported kind %q", item.Kind)
	}
	if strings.TrimSpace(item.Label) == "" {
		return errors.New("label is required")
	}
	switch item.Tone {
	case "memory", "skill", "procedure", "neutral", "warning":
	default:
		return fmt.Errorf("unsupported tone %q", item.Tone)
	}
	if strings.TrimSpace(item.SkillID) != "" && item.Kind != "skill" {
		return errors.New("skill_id is only supported for skill disclosure items")
	}
	return nil
}

func validateMessageAction(action *MessageAction) error {
	if action == nil {
		return nil
	}
	switch action.Kind {
	case "resume_run":
	default:
		return fmt.Errorf("unsupported kind %q", action.Kind)
	}
	if strings.TrimSpace(action.RunID) == "" {
		return errors.New("run_id is required")
	}
	if strings.TrimSpace(action.Label) == "" {
		return errors.New("label is required")
	}
	return nil
}

func projectRun(record events.RunRecord) (Run, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return Run{}, err
	}
	mode, err := projectRunMode(record.OrchestrationMode)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID:        record.RunID,
		ThreadID:  record.SessionID,
		Status:    status,
		Mode:      mode,
		CreatedAt: record.CreatedAt,
	}
	if record.Status != events.RunStatusRunning {
		run.CompletedAt = record.UpdatedAt
	}
	return run, nil
}

func projectRunEvent(record events.EventRecord) (RunEvent, error) {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return RunEvent{}, projectionError("run event payload must be object: run_id=%s sequence=%d kind=%s", record.RunID, record.Sequence, record.Kind)
	}
	data, err := projectRunEventData(record.Kind, payload)
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

func projectUnsupportedRunEvent(record events.EventRecord) UnsupportedRunEvent {
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
	if _, err := projectRunEventData(record.Kind, payload); err != nil {
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

func projectRunEventData(kind string, payload map[string]any) (any, error) {
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
		return ToolCallStartedData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.progress":
		return ToolCallProgressData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.succeeded":
		return ToolCallSucceededData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.failed":
		return ToolCallFailedData{ToolCall: projectToolCallPayload(kind, payload)}, nil
	case "tool.call.interrupted":
		return ToolCallInterruptedData{ToolCall: projectToolCallPayload(kind, payload)}, nil
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
	case "operator_question.pending":
		return projectOperatorQuestionData(payload), nil
	case "operator_question.decided":
		return projectOperatorQuestionData(payload), nil
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
	case "crystallization.failed":
		return CrystallizationFailedData{
			RunID: topLevelString(payload, "run_id"),
			Error: topLevelString(payload, "error"),
		}, nil
	case "crystallization.verdict":
		return CrystallizationVerdictData{
			RunID:     topLevelString(payload, "run_id"),
			Verdict:   topLevelString(payload, "verdict"),
			SkillID:   topLevelString(payload, "skill_id"),
			Reason:    topLevelString(payload, "reason"),
			SimilarTo: topLevelString(payload, "similar_to"),
		}, nil
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
			Summary:           topLevelString(payload, "summary"),
			FinalStatus:       topLevelString(payload, "final_status"),
			AcceptanceStatus:  topLevelString(payload, "acceptance_status"),
			AcceptanceReasons: stringArrayField(payload, "acceptance_reasons"),
			OrchestrationMode: topLevelString(payload, "orchestration_mode"),
			ParentStepID:      topLevelString(payload, "parent_step_id"),
			Error:             topLevelString(payload, "error"),
		}, nil
	default:
		return nil, projectionError("unsupported live run event kind %q", kind)
	}
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

func pendingActionOptionsFromAny(raw any) []events.PendingActionOption {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]events.PendingActionOption, 0, len(items))
	for _, item := range items {
		option, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, events.PendingActionOption{
			ID:          topLevelString(option, "id"),
			Label:       topLevelString(option, "label"),
			Description: topLevelString(option, "description"),
		})
	}
	return out
}

func topLevelBool(payload map[string]any, key string) bool {
	value, ok := payload[key].(bool)
	return ok && value
}

func projectToolCallPayload(kind string, payload map[string]any) map[string]any {
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

func projectThreadState(status events.RunStatus) (string, error) {
	switch status {
	case events.RunStatusRunning:
		return string(runtime.SessionStateRunning), nil
	case events.RunStatusSucceeded:
		return string(runtime.SessionStateCompleted), nil
	case events.RunStatusInterrupted:
		return string(runtime.SessionStateInterrupted), nil
	case events.RunStatusFailed:
		return string(runtime.SessionStateFailed), nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

func projectRunStatus(status events.RunStatus) (string, error) {
	switch status {
	case events.RunStatusRunning:
		return "running", nil
	case events.RunStatusSucceeded:
		return "completed", nil
	case events.RunStatusInterrupted:
		return "interrupted", nil
	case events.RunStatusFailed:
		return "failed", nil
	default:
		return "", projectionError("unknown run status %q", status)
	}
}

func projectRunMode(mode orchestrationmode.Mode) (string, error) {
	switch mode {
	case orchestrationmode.DirectResponse:
		return "direct", nil
	case orchestrationmode.SingleAgent:
		return "agent", nil
	case orchestrationmode.PlanExecute:
		return "plan_execute", nil
	default:
		return "", projectionError("unknown run mode %q", mode)
	}
}

func topLevelString(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func topLevelInt(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func objectField(payload map[string]any, key string) (map[string]any, bool) {
	value, ok := payload[key].(map[string]any)
	if !ok {
		return nil, false
	}
	return cloneMap(value), true
}

func stringArrayField(payload map[string]any, key string) []string {
	values, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		item, ok := value.(string)
		if ok {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return out
}

func cloneMap(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func projectionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrClientProjectionFailed, fmt.Sprintf(format, args...))
}
