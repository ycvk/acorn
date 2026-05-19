package runtime

import (
	"encoding/json"
	"fmt"
	"time"
)

// --- StreamItem ---

type StreamItem struct {
	RunID     string         `json:"run_id"`
	Sequence  int64          `json:"sequence,omitempty"`
	Kind      StreamItemKind `json:"kind"`
	CreatedAt time.Time      `json:"created_at"`
	Payload   StreamPayload  `json:"-"`
}

// MarshalJSON serializes StreamItem with payload fields flattened into the
// top-level object. The "kind" field acts as the discriminator so the
// nested "payload" wrapper is unnecessary on the wire.
func (item StreamItem) MarshalJSON() ([]byte, error) {
	obj := map[string]any{
		"run_id":     item.RunID,
		"kind":       string(item.Kind),
		"created_at": item.CreatedAt,
	}
	if item.Sequence != 0 {
		obj["sequence"] = item.Sequence
	}
	if item.Payload != nil {
		payloadBytes, err := json.Marshal(item.Payload)
		if err != nil {
			return nil, fmt.Errorf("marshal stream item payload: %w", err)
		}
		var payloadMap map[string]any
		if err := json.Unmarshal(payloadBytes, &payloadMap); err != nil {
			return nil, fmt.Errorf("unmarshal stream item payload to map: %w", err)
		}
		for k, v := range payloadMap {
			obj[k] = v
		}
	}
	return json.Marshal(obj)
}

// UnmarshalJSON deserializes flat StreamItem JSON, extracting common fields
// and passing the remaining keys as the typed payload based on Kind.
func (item *StreamItem) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	runID, ok := raw["run_id"].(string)
	if !ok {
		return fmt.Errorf("stream item run_id must be a string")
	}
	kindStr, ok := raw["kind"].(string)
	if !ok {
		return fmt.Errorf("stream item kind must be a string")
	}

	var sequence int64
	if seq, ok := raw["sequence"]; ok {
		switch v := seq.(type) {
		case float64:
			sequence = int64(v)
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return fmt.Errorf("parse stream item sequence: %w", err)
			}
			sequence = n
		}
	}

	var createdAt time.Time
	if ca, ok := raw["created_at"]; ok {
		if caStr, ok := ca.(string); ok {
			t, err := time.Parse(time.RFC3339Nano, caStr)
			if err != nil {
				t, err = time.Parse(time.RFC3339, caStr)
				if err != nil {
					return fmt.Errorf("parse created_at: %w", err)
				}
			}
			createdAt = t
		}
	}

	item.RunID = runID
	item.Kind = StreamItemKind(kindStr)
	item.Sequence = sequence
	item.CreatedAt = createdAt

	delete(raw, "run_id")
	delete(raw, "kind")
	delete(raw, "sequence")
	delete(raw, "created_at")

	if len(raw) > 0 {
		payloadBytes, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("re-marshal payload: %w", err)
		}
		p, err := unmarshalPayload(item.Kind, payloadBytes)
		if err != nil {
			return err
		}
		item.Payload = p
	}

	return nil
}

func unmarshalPayload(kind StreamItemKind, data json.RawMessage) (StreamPayload, error) {
	var p StreamPayload
	switch kind {
	case StreamKindRunStarted:
		p = &RunStartedPayload{}
	case StreamKindRunCompleted:
		p = &RunCompletedPayload{}
	case StreamKindRunFailed:
		p = &RunFailedPayload{}
	case StreamKindRunInterrupted:
		p = &RunInterruptedPayload{}
	case StreamKindRunResumeRequested:
		p = &RunResumeRequestedPayload{}
	case StreamKindDecisionSelected:
		p = &DecisionSelectedPayload{}
	case StreamKindDecisionBlocked:
		p = &DecisionBlockedPayload{}
	case StreamKindSkillDiscovered:
		p = &SkillDiscoveredPayload{}
	case StreamKindSkillSelected:
		p = &SkillSelectedPayload{}
	case StreamKindSkillLoaded:
		p = &SkillLoadedPayload{}
	case StreamKindSkillFailed:
		p = &SkillFailedPayload{}
	case StreamKindSkillLifecycle:
		p = &SkillLifecyclePayload{}
	case StreamKindProcedureActivation:
		p = &ProcedureActivationPayload{}
	case StreamKindMemoryPrepared:
		p = &MemoryPreparedPayload{}
	case StreamKindContextPressure:
		p = &ContextPressurePayload{}
	case StreamKindContextCompressed:
		p = &ContextCompressedPayload{}
	case StreamKindAssistantDelta:
		p = &AssistantDeltaPayload{}
	case StreamKindAssistantMessage:
		p = &AssistantMessagePayload{}
	case StreamKindToolCallStarted:
		p = &ToolCallStartedPayload{}
	case StreamKindToolCallProgress:
		p = &ToolCallProgressPayload{}
	case StreamKindToolCallSucceeded:
		p = &ToolCallSucceededPayload{}
	case StreamKindToolCallFailed:
		p = &ToolCallFailedPayload{}
	case StreamKindToolCallInterrupted:
		p = &ToolCallInterruptedPayload{}
	case StreamKindProviderDegraded:
		p = &ProviderDegradedPayload{}
	case StreamKindMCPToolCatalogRefreshed,
		StreamKindMCPToolCatalogRefreshFailed,
		StreamKindMCPProviderAdded,
		StreamKindMCPProviderRemoved,
		StreamKindMCPProviderRestarted,
		StreamKindMCPResourceCatalogRefreshed,
		StreamKindMCPResourceCatalogRefreshFailed,
		StreamKindMCPPromptCatalogRefreshed,
		StreamKindMCPPromptCatalogRefreshFailed,
		StreamKindMCPAuthStatusChanged:
		p = &MCPProviderLifecyclePayload{}
	case StreamKindElicitationPending:
		p = &ElicitationPayload{}
	case StreamKindElicitationDecided:
		p = &ElicitationPayload{}
	case StreamKindSamplingStarted:
		p = &SamplingPayload{}
	case StreamKindSamplingCompleted:
		p = &SamplingPayload{}
	case StreamKindSamplingFailed:
		p = &SamplingPayload{}
	case StreamKindSubagentStarted:
		p = &SubagentStartedPayload{}
	case StreamKindSubagentCompleted:
		p = &SubagentCompletedPayload{}
	case StreamKindSubagentFailed:
		p = &SubagentFailedPayload{}
	case StreamKindHeartbeat:
		p = &HeartbeatPayload{}
	case StreamKindToolParallelBatchStarted:
		p = &ToolParallelBatchStartedPayload{}
	case StreamKindToolParallelBatchCompleted:
		p = &ToolParallelBatchCompletedPayload{}
	case StreamKindRunArchived:
		p = &RunArchivedPayload{}
	case StreamKindPlanCreated:
		p = &PlanCreatedPayload{}
	case StreamKindPlanUpdated:
		p = &PlanUpdatedPayload{}
	case StreamKindPlanCleared:
		p = &PlanClearedPayload{}
	case StreamKindStepStarted:
		p = &PlanStepStartedPayload{}
	case StreamKindStepCompleted:
		p = &PlanStepCompletedPayload{}
	case StreamKindStepFailed:
		p = &PlanStepFailedPayload{}
	case StreamKindCrystallizationFailed:
		p = &CrystallizationFailedPayload{}
	case StreamKindCrystallizationVerdict:
		p = &CrystallizationVerdictPayload{}
	default:
		return nil, fmt.Errorf("unknown stream kind: %s", kind)
	}
	if err := json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("unmarshal %s payload: %w", kind, err)
	}
	return p, nil
}
