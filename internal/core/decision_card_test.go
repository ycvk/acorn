package core

import (
	"encoding/json"
	"testing"
)

func TestOperatorQuestionPayloadSupportsDecisionCard(t *testing.T) {
	payload := OperatorQuestionPayload{
		Question: "Should Acorn send this deployment notification email?",
		Options: []PendingActionOption{
			{ID: "approve", Label: "Approve and send"},
			{ID: "reject", Label: "Reject — do not send"},
		},
		ConsideredOptions: []ConsideredOption{
			{
				ID:             "approve",
				Label:          "Approve and send",
				Evidence:       "Deploy succeeded; email template matches owner preference.",
				RejectedReason: "",
			},
			{
				ID:             "delay",
				Label:          "Delay until morning",
				Evidence:       "Owner timezone is 2am.",
				RejectedReason: "Deployment is time-sensitive; owner set critical alerts bypass quiet hours.",
			},
		},
		Rationale:      "Deployment succeeded and notification matches owner's configured preferences. Delay rejected due to time-sensitivity.",
		Risk:           "Sends an external email to 3 recipients. No cost. Reversible (recall possible within 1h).",
		Recommendation: "approve",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded OperatorQuestionPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.ConsideredOptions) != 2 {
		t.Fatalf("considered options = %d, want 2", len(decoded.ConsideredOptions))
	}
	if decoded.ConsideredOptions[1].RejectedReason == "" {
		t.Fatal("second considered option should have a rejected reason")
	}
	if decoded.Rationale == "" {
		t.Fatal("rationale should survive roundtrip")
	}
	if decoded.Risk == "" {
		t.Fatal("risk should survive roundtrip")
	}
	if decoded.Recommendation != "approve" {
		t.Fatalf("recommendation = %q, want 'approve'", decoded.Recommendation)
	}
}

func TestOperatorQuestionPayloadBackwardCompatible(t *testing.T) {
	// Old payloads without Decision Card fields must still decode.
	old := `{"question":"ok?","options":[{"id":"yes","label":"Yes"}],"allow_freeform":false}`
	var payload OperatorQuestionPayload
	if err := json.Unmarshal([]byte(old), &payload); err != nil {
		t.Fatalf("unmarshal old payload: %v", err)
	}
	if payload.Question != "ok?" {
		t.Fatalf("question = %q, want 'ok?'", payload.Question)
	}
	if len(payload.ConsideredOptions) != 0 {
		t.Fatalf("considered options = %d, want 0 for old payload", len(payload.ConsideredOptions))
	}
}
