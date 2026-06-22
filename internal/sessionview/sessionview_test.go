package sessionview_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/sessionview"
)

func TestBuildResultSummaryMemoryDisclosure(t *testing.T) {
	records := []events.EventRecord{{
		RunID: "run_memory",
		Kind:  "memory.prepared",
		Payload: map[string]any{
			"memory_prepared": map[string]any{
				"entry_count":     float64(2),
				"workspace_scope": "workspace:acorn",
				"entries": []any{
					map[string]any{"ref": "facts/workspaces/acorn/repo.md"},
					map[string]any{"ref": "history/run_1.md"},
				},
			},
		},
	}}

	summary, err := sessionview.BuildResultSummary(records)
	if err != nil {
		t.Fatalf("BuildResultSummary: %v", err)
	}
	if len(summary.Disclosures) != 1 {
		t.Fatalf("disclosures = %#v", summary.Disclosures)
	}
	item := summary.Disclosures[0]
	if item.Kind != "memory" || item.Label != "Prepared 2 memory entries" || item.Detail != "workspace:acorn" || item.Tone != "memory" {
		t.Fatalf("memory disclosure item = %#v", item)
	}
}

func TestBuildResultSummarySkillDisclosure(t *testing.T) {
	records := []events.EventRecord{{
		RunID: "run_skill",
		Kind:  "skill.selected",
		Payload: map[string]any{
			"skill": map[string]any{
				"selected_id":  "skill.ship.patch",
				"name":         "Repository patch shipping",
				"path":         "/Users/ycvk/GolandProjects/acorn/skills/skill.ship.patch/SKILL.md",
				"requirements": []any{"approved design"},
				"score":        float64(0.98),
			},
		},
	}}

	summary, err := sessionview.BuildResultSummary(records)
	if err != nil {
		t.Fatalf("BuildResultSummary: %v", err)
	}
	if len(summary.Disclosures) != 1 {
		t.Fatalf("disclosures = %#v", summary.Disclosures)
	}
	item := summary.Disclosures[0]
	if item.Kind != "skill" || item.Label != "Used skill" || item.Detail != "Repository patch shipping" || item.Tone != "skill" || item.SkillID != "skill.ship.patch" {
		t.Fatalf("skill disclosure item = %#v", item)
	}
	encoded := mustJSON(t, summary.Disclosures)
	for _, forbidden := range []string{"path", "requirements", "score", "/Users/ycvk"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("skill disclosure should not expose %q: %s", forbidden, encoded)
		}
	}
}

func TestBuildResultSummaryProcedureDisclosure(t *testing.T) {
	records := []events.EventRecord{{
		RunID: "run_procedure",
		Kind:  "skill.selected",
		Payload: map[string]any{
			"skill": map[string]any{
				"selected_id":  "sop.fix-sqlite-query-loop-error-handling",
				"name":         "SQLite Rows Error Handling SOP",
				"origin":       "distilled",
				"task_pattern": "fix sqlite query loop error handling",
			},
		},
	}}

	summary, err := sessionview.BuildResultSummary(records)
	if err != nil {
		t.Fatalf("BuildResultSummary: %v", err)
	}
	item := summary.Disclosures[0]
	if item.Kind != "skill" || item.Label != "Used procedure" || item.Detail != "SQLite Rows Error Handling SOP" || item.Tone != "procedure" || item.SkillID != "sop.fix-sqlite-query-loop-error-handling" {
		t.Fatalf("procedure disclosure item = %#v", item)
	}
	encoded := mustJSON(t, summary.Disclosures)
	if strings.Contains(encoded, "selected_id") || strings.Contains(encoded, "task_pattern") {
		t.Fatalf("procedure disclosure should not expose raw internals: %s", encoded)
	}
}

func TestBuildResultSummaryOrdersMemoryBeforeSkill(t *testing.T) {
	records := []events.EventRecord{
		{
			RunID:   "run_combined",
			Kind:    "skill.loaded",
			Payload: map[string]any{"skill": map[string]any{"selected_id": "skill.ship.patch"}},
		},
		{
			RunID:   "run_combined",
			Kind:    "memory.prepared",
			Payload: map[string]any{"memory_prepared": map[string]any{"entry_count": float64(1)}},
		},
	}

	summary, err := sessionview.BuildResultSummary(records)
	if err != nil {
		t.Fatalf("BuildResultSummary: %v", err)
	}
	if len(summary.Disclosures) != 2 || summary.Disclosures[0].Kind != "memory" || summary.Disclosures[1].Kind != "skill" {
		t.Fatalf("disclosure order = %#v", summary.Disclosures)
	}
}

func TestBuildResultSummaryFailsOnCorruptMemoryEvent(t *testing.T) {
	records := []events.EventRecord{{
		RunID:    "run_corrupt_memory",
		Sequence: 1,
		Kind:     "memory.prepared",
		Payload:  map[string]any{"entry_count": float64(1)},
	}}

	_, err := sessionview.BuildResultSummary(records)
	if err == nil {
		t.Fatal("expected corrupt memory prepared error")
	}
	if !strings.Contains(err.Error(), "memory.prepared missing memory_prepared object") {
		t.Fatalf("error = %v, want corrupt memory prepared context", err)
	}
}

func TestBuildResultSummaryFailsOnCorruptSkillEvent(t *testing.T) {
	records := []events.EventRecord{{
		RunID:    "run_corrupt_skill",
		Sequence: 1,
		Kind:     "skill.selected",
		Payload:  map[string]any{"skill": map[string]any{"path": "/tmp/skill"}},
	}}

	_, err := sessionview.BuildResultSummary(records)
	if err == nil {
		t.Fatal("expected corrupt skill event error")
	}
	if !strings.Contains(err.Error(), "skill.selected missing skill name") {
		t.Fatalf("error = %v, want corrupt skill context", err)
	}
}

func TestAssistantMessageForRunSucceededShape(t *testing.T) {
	run := &events.RunRecord{RunID: "run_1", Status: events.RunStatusSucceeded, Output: "done"}
	summary := sessionview.ResultSummary{
		Reasoning:   "checked the repository state before answering",
		Changed:     []string{},
		Verified:    []string{},
		Risks:       []string{},
		Disclosures: nil,
	}

	content, parts, err := sessionview.AssistantMessageForRun(run, summary)
	if err != nil {
		t.Fatalf("AssistantMessageForRun: %v", err)
	}
	if content != "done" {
		t.Fatalf("content = %q, want done", content)
	}
	if len(parts) != 4 {
		t.Fatalf("parts length = %d, want 4: %#v", len(parts), parts)
	}
	if parts[0].Kind != "text" || parts[0].Text != "done" {
		t.Fatalf("text part = %#v", parts[0])
	}
	if parts[1].Kind != "reasoning" || parts[1].Reasoning != "checked the repository state before answering" {
		t.Fatalf("reasoning part = %#v", parts[1])
	}
	if parts[2].Kind != "result" || parts[2].Title != "Task completed" {
		t.Fatalf("result part = %#v", parts[2])
	}
	if parts[3].Kind != "technical_detail_link" || parts[3].RunID != "run_1" || parts[3].DetailRunID != "run_1" {
		t.Fatalf("technical detail part = %#v", parts[3])
	}
}

func TestAssistantMessageForRunFailedShape(t *testing.T) {
	run := &events.RunRecord{RunID: "run_failed", Status: events.RunStatusFailed, Error: "shell exited with status 1"}

	content, parts, err := sessionview.AssistantMessageForRun(run, sessionview.ResultSummary{})
	if err != nil {
		t.Fatalf("AssistantMessageForRun: %v", err)
	}
	if content != "Acorn could not finish this turn." {
		t.Fatalf("content = %q", content)
	}
	if len(parts) != 2 || parts[0].Kind != "work_status" || parts[0].Status != "failed" || parts[0].Title != "Acorn could not finish" {
		t.Fatalf("failure status part = %#v", parts)
	}
	if parts[0].Summary != "shell exited with status 1" || parts[0].Action != nil {
		t.Fatalf("failure status summary/action = %#v", parts[0])
	}
	if parts[1].Kind != "technical_detail_link" || parts[1].RunID != "run_failed" {
		t.Fatalf("failure technical detail part = %#v", parts[1])
	}
}

func TestAssistantMessageForRunInterruptedShape(t *testing.T) {
	run := &events.RunRecord{RunID: "run_resume", Status: events.RunStatusInterrupted, Output: "partial output"}

	content, parts, err := sessionview.AssistantMessageForRun(run, sessionview.ResultSummary{})
	if err != nil {
		t.Fatalf("AssistantMessageForRun: %v", err)
	}
	if content != "Acorn paused before continuing." {
		t.Fatalf("content = %q", content)
	}
	if len(parts) != 2 || parts[0].Kind != "work_status" || parts[0].Status != "interrupted" {
		t.Fatalf("interrupted parts = %#v", parts)
	}
	if parts[0].Action == nil || parts[0].Action.Kind != "resume_run" || parts[0].Action.RunID != "run_resume" {
		t.Fatalf("interrupted action = %#v", parts[0].Action)
	}
}

func TestDecisionMessageForElicitation(t *testing.T) {
	action := &events.PendingActionRecord{
		ActionID:    "action_decision",
		RunID:       "run_decision",
		Kind:        events.PendingActionKindElicitation,
		PayloadJSON: `{"message":"Allow Acorn to continue?"}`,
		Status:      events.PendingActionStatusPending,
	}

	content, parts, err := sessionview.DecisionMessageForPendingAction(action)
	if err != nil {
		t.Fatalf("DecisionMessageForPendingAction: %v", err)
	}
	if content != "Allow Acorn to continue?" {
		t.Fatalf("content = %q", content)
	}
	if len(parts) != 2 || parts[0].Kind != "decision" {
		t.Fatalf("decision parts = %#v", parts)
	}
	if parts[0].DecisionID != "action_decision" || parts[0].Status != "pending" || parts[0].SelectedOptionID != "" {
		t.Fatalf("decision part = %#v", parts[0])
	}
	if parts[0].Options[0].ID != "accept" || parts[0].Options[1].ID != "decline" {
		t.Fatalf("decision options = %#v", parts[0].Options)
	}

	action.Status = events.PendingActionStatusApproved
	action.DecisionJSON = `{"action":"accept"}`
	_, decided, err := sessionview.DecisionMessageForPendingAction(action)
	if err != nil {
		t.Fatalf("DecisionMessageForPendingAction approved: %v", err)
	}
	if decided[0].Status != "approved" || decided[0].SelectedOptionID != "accept" {
		t.Fatalf("decided decision part = %#v", decided[0])
	}
}

func TestDecisionMessageForOperatorQuestion(t *testing.T) {
	action := &events.PendingActionRecord{
		ActionID: "action_operator",
		RunID:    "run_operator",
		Kind:     events.PendingActionKindOperatorQuestion,
		PayloadJSON: `{
			"question":"Which path should Acorn take?",
			"options":[{"id":"fast","label":"Fast path","description":"Ship the narrow fix"}],
			"allow_freeform":true
		}`,
		DecisionJSON: `{"action":"answer","selected_option_id":"fast","answer":"Use the fast path."}`,
		Status:       events.PendingActionStatusApproved,
	}

	content, parts, err := sessionview.DecisionMessageForPendingAction(action)
	if err != nil {
		t.Fatalf("DecisionMessageForPendingAction: %v", err)
	}
	if content != "Which path should Acorn take?" {
		t.Fatalf("content = %q", content)
	}
	if parts[0].SelectedOptionID != "fast" || parts[0].Answer != "Use the fast path." {
		t.Fatalf("operator decision part = %#v", parts[0])
	}
	if len(parts[0].Options) != 1 || parts[0].Options[0].ID != "fast" {
		t.Fatalf("operator decision options = %#v", parts[0].Options)
	}
}

func TestDecisionMessageForPendingActionRejectsNilAndUnsupported(t *testing.T) {
	if _, _, err := sessionview.DecisionMessageForPendingAction(nil); err == nil {
		t.Fatal("expected nil action error")
	}
	action := &events.PendingActionRecord{ActionID: "action_x", Kind: "unsupported"}
	if _, _, err := sessionview.DecisionMessageForPendingAction(action); err == nil {
		t.Fatal("expected unsupported kind error")
	}
}

func TestDecisionMessageHasActionID(t *testing.T) {
	parts := []sessionview.MessagePart{
		{Kind: "decision", DecisionID: "action_decision"},
		{Kind: "technical_detail_link", RunID: "run_decision"},
	}
	encoded := mustJSON(t, parts)

	matches, err := sessionview.DecisionMessageHasActionID(encoded, "action_decision")
	if err != nil {
		t.Fatalf("DecisionMessageHasActionID: %v", err)
	}
	if !matches {
		t.Fatal("expected decision part to match action id")
	}

	missing, err := sessionview.DecisionMessageHasActionID(encoded, "other")
	if err != nil {
		t.Fatalf("DecisionMessageHasActionID other: %v", err)
	}
	if missing {
		t.Fatal("expected no match for unrelated action id")
	}

	empty, err := sessionview.DecisionMessageHasActionID("", "action_decision")
	if err != nil {
		t.Fatalf("DecisionMessageHasActionID empty: %v", err)
	}
	if empty {
		t.Fatal("expected empty content parts to not match")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}
