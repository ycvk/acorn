package api

import (
	"fmt"
	"slices"

	"github.com/ycvk/acorn/internal/core"
)

var liveRunEventKinds = []string{
	"run.started",
	"assistant.delta",
	"agent.message",
	"run.completed",
	"run.failed",
	"run.interrupted",
	"run.resume_requested",
	"elicitation.pending",
	"elicitation.decided",
	"operator_question.pending",
	"operator_question.decided",
	"decision_blocked",
}

// IsLiveRunEventKind reports whether kind is part of the /v1 mobile live contract.
func IsLiveRunEventKind(kind string) bool {
	return slices.Contains(liveRunEventKinds, kind)
}

// ProjectRunEvent converts an EventRecord into the /v1 client run event contract.
func ProjectRunEvent(record core.EventRecord) (core.RunEvent, error) {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return core.RunEvent{}, projectionError("run event payload must be object: run_id=%s sequence=%d kind=%s", record.RunID, record.Sequence, record.Kind)
	}
	if !IsLiveRunEventKind(record.Kind) {
		return core.RunEvent{}, projectionError("event %q is diagnostic-only and not part of the live client contract", record.Kind)
	}
	data, err := ProjectRunEventData(record.Kind, payload)
	if err != nil {
		return core.RunEvent{}, err
	}
	return core.RunEvent{
		EventID: fmt.Sprintf("%s:%d", record.RunID, record.Sequence),
		RunID:   record.RunID,
		Seq:     record.Sequence,
		TS:      record.CreatedAt,
		Type:    record.Kind,
		Data:    data,
	}, nil
}

// ProjectRunEventData maps an event kind and payload to a typed client data struct.
func ProjectRunEventData(kind string, payload map[string]any) (any, error) {
	switch kind {
	case "run.started":
		return core.RunStartedData{Input: topLevelString(payload, "input")}, nil
	case "assistant.delta":
		value, ok := objectField(payload, "assistant_delta")
		if !ok {
			return nil, projectionError("assistant.delta payload missing assistant_delta object")
		}
		return core.AssistantDeltaData{AssistantDelta: value}, nil
	case "agent.message":
		value, ok := objectField(payload, "message")
		if !ok {
			return nil, projectionError("agent.message payload missing message object")
		}
		return core.AgentMessageData{Message: value}, nil
	case "run.completed":
		value, _ := objectField(payload, "message")
		return core.RunCompletedData{Message: value}, nil
	case "run.failed":
		return core.RunFailedData{Error: topLevelString(payload, "error")}, nil
	case "run.interrupted":
		value, _ := objectField(payload, "interrupt")
		return core.RunInterruptedData{Interrupt: value}, nil
	case "run.resume_requested":
		value, _ := objectField(payload, "targets")
		return core.RunResumeRequestedData{Targets: value}, nil
	case "elicitation.pending":
		return core.ElicitationPendingData{
			ActionID:        topLevelString(payload, "action_id"),
			Message:         topLevelString(payload, "message"),
			RequestedSchema: payload["requested_schema"],
		}, nil
	case "elicitation.decided":
		return core.ElicitationDecidedData{
			ActionID:        topLevelString(payload, "action_id"),
			Message:         topLevelString(payload, "message"),
			RequestedSchema: payload["requested_schema"],
		}, nil
	case "operator_question.pending", "operator_question.decided":
		return projectOperatorQuestionData(payload), nil
	case "decision_blocked":
		return projectDecisionBlockedData(payload), nil
	default:
		return nil, projectionError("unsupported live run event kind %q", kind)
	}
}

func projectOperatorQuestionData(payload map[string]any) core.OperatorQuestionData {
	return core.OperatorQuestionData{
		ActionID:         topLevelString(payload, "action_id"),
		Question:         topLevelString(payload, "question"),
		Options:          pendingActionOptionsFromAny(payload["options"]),
		AllowFreeform:    topLevelBool(payload, "allow_freeform"),
		Decision:         topLevelString(payload, "decision"),
		SelectedOptionID: topLevelString(payload, "selected_option_id"),
		Answer:            topLevelString(payload, "answer"),
	}
}

func projectDecisionBlockedData(payload map[string]any) core.DecisionBlockedData {
	return core.DecisionBlockedData{
		Action:          topLevelString(payload, "action"),
		DecisionReason:  topLevelString(payload, "decision_reason"),
		ExplicitSkillID: topLevelString(payload, "explicit_skill_id"),
	}
}
