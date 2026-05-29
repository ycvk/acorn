package stream

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

func TestBuildTraceProjectsAndSummarizesItems(t *testing.T) {
	run := &events.RunRecord{RunID: "run_1", Status: events.RunStatusSucceeded}
	raw := []events.EventRecord{
		{Sequence: 1, RunID: "run_1", Kind: "run.started", CreatedAt: time.Now(), Payload: map[string]any{"input": "hello"}},
		{Sequence: 2, RunID: "run_1", Kind: "agent.message", CreatedAt: time.Now(), Payload: map[string]any{"message": map[string]any{"role": "assistant", "content": "hi"}}},
		{Sequence: 3, RunID: "run_1", Kind: "run.completed", CreatedAt: time.Now(), Payload: map[string]any{"message": map[string]any{"role": "assistant", "content": "hi"}}},
	}

	trace := mustBuildTrace(t, run, raw)
	if trace == nil || len(trace.Items) != 3 {
		t.Fatalf("BuildTrace returned %#v", trace)
	}
	if trace.Summary == nil || trace.Summary.ItemCount != 3 {
		t.Fatalf("unexpected summary: %#v", trace.Summary)
	}
	if trace.Summary.LastKind != StreamKindRunCompleted {
		t.Fatalf("LastKind = %q, want %q", trace.Summary.LastKind, StreamKindRunCompleted)
	}
}

func TestBuildTraceRoundTripsResumeAndInterruptContract(t *testing.T) {
	trace := mustBuildTrace(t, &events.RunRecord{RunID: "run_3"}, []events.EventRecord{
		{
			Sequence: 1, RunID: "run_3", Kind: "run.interrupted", CreatedAt: time.Now(),
			Payload: map[string]any{
				"interrupt": map[string]any{
					"context_count": 1,
					"contexts": []any{
						map[string]any{
							"id":            "interrupt_1",
							"address":       "tool.run_command",
							"info":          map[string]any{"kind": "approval"},
							"is_root_cause": true,
						},
					},
				},
			},
		},
		{
			Sequence: 2, RunID: "run_3", Kind: "run.resume_requested", CreatedAt: time.Now(),
			Payload: map[string]any{
				"targets": map[string]any{"interrupt_ids": []any{"interrupt_1"}},
			},
		},
	})

	if trace == nil || len(trace.Items) != 2 {
		t.Fatalf("BuildTrace returned %#v", trace)
	}
	if !trace.Summary.Interrupted {
		t.Fatalf("expected interrupted summary, got %#v", trace.Summary)
	}
	interrupt := trace.Items[0].GetInterrupt()
	if interrupt == nil || interrupt.ContextCount != 1 {
		t.Fatalf("unexpected interrupt projection: %#v", interrupt)
	}
	if interrupt.Contexts[0].ID != "interrupt_1" || !interrupt.Contexts[0].IsRootCause {
		t.Fatalf("unexpected interrupt context: %#v", interrupt.Contexts[0])
	}
	targets := trace.Items[1].GetTargets()
	if targets == nil {
		t.Fatalf("unexpected resume targets: nil")
	}

	body, err := json.Marshal(trace.Items[0])
	if err != nil {
		t.Fatalf("json.Marshal(trace.Items[0]): %v", err)
	}
	for _, want := range []string{`"kind":"run_interrupted"`, `"context_count":1`, `"id":"interrupt_1"`} {
		if !json.Valid(body) || !strings.Contains(string(body), want) {
			t.Fatalf("marshal should contain %q, got %s", want, string(body))
		}
	}
}

func TestBuildTraceProjectsSkillEventsAndSummary(t *testing.T) {
	trace := mustBuildTrace(t, &events.RunRecord{RunID: "run_4"}, []events.EventRecord{
		{
			Sequence: 1, RunID: "run_4", Kind: "skill.discovered", CreatedAt: time.Now(),
			Payload: map[string]any{
				"skill": map[string]any{
					"candidates": []any{
						map[string]any{
							"id":            "skill.inspect.repo",
							"name":          "Inspect Repo",
							"score":         100,
							"matched_terms": []any{"inspect repo"},
						},
					},
				},
			},
		},
		{
			Sequence: 2, RunID: "run_4", Kind: "skill.selected", CreatedAt: time.Now(),
			Payload: map[string]any{
				"skill": map[string]any{
					"selected_id":   "skill.inspect.repo",
					"name":          "Inspect Repo",
					"source":        "workspace",
					"score":         145,
					"instruction":   "Read README.md first.",
					"matched_terms": []any{"inspect repo"},
				},
			},
		},
		{
			Sequence: 3, RunID: "run_4", Kind: "skill.loaded", CreatedAt: time.Now(),
			Payload: map[string]any{
				"skill": map[string]any{
					"selected_id": "skill.inspect.repo",
					"name":        "Inspect Repo",
					"source":      "workspace",
					"instruction": "Read README.md first.",
				},
			},
		},
		{
			Sequence: 4, RunID: "run_4", Kind: "skill.failed", CreatedAt: time.Now(),
			Payload: map[string]any{
				"skill": map[string]any{
					"selected_id":    "skill.inspect.repo",
					"failure_reason": "missing_tool_use:read_file",
				},
			},
		},
		{
			Sequence: 5, RunID: "run_4", Kind: "run.completed", CreatedAt: time.Now(),
			Payload: map[string]any{"message": map[string]any{"role": "assistant", "content": "done"}},
		},
	})
	if trace == nil || len(trace.Items) != 5 {
		t.Fatalf("BuildTrace returned %#v", trace)
	}
	skill0 := trace.Items[0].GetSkill()
	if skill0 == nil || len(skill0.Candidates) != 1 {
		t.Fatalf("unexpected skill discovered projection: %#v", skill0)
	}
	skill1 := trace.Items[1].GetSkill()
	if skill1 == nil || skill1.Name != "Inspect Repo" || skill1.Source != "workspace" || skill1.Instruction != "Read README.md first." {
		t.Fatalf("unexpected skill selected projection: %#v", skill1)
	}
	skill3 := trace.Items[3].GetSkill()
	if skill3 == nil || skill3.FailureReason != "missing_tool_use:read_file" {
		t.Fatalf("unexpected skill failed projection: %#v", skill3)
	}
	if trace.Summary == nil || trace.Summary.SkillEventCount != 4 || !trace.Summary.SkillSelected {
		t.Fatalf("unexpected skill summary: %#v", trace.Summary)
	}
}

func TestBuildTraceProjectsNoSelectionReason(t *testing.T) {
	trace := mustBuildTrace(t, &events.RunRecord{RunID: "run_4b"}, []events.EventRecord{
		{
			Sequence: 1, RunID: "run_4b", Kind: "skill.discovered", CreatedAt: time.Now(),
			Payload: map[string]any{
				"skill": map[string]any{
					"no_selection_reason": "ambiguous_top_score",
					"candidates": []any{
						map[string]any{
							"id":    "skill.inspect.repo",
							"name":  "Inspect Repo",
							"score": 100,
						},
						map[string]any{
							"id":    "skill.inspect.docs",
							"name":  "Inspect Docs",
							"score": 100,
						},
					},
				},
			},
		},
	})
	if trace == nil || len(trace.Items) != 1 {
		t.Fatalf("BuildTrace returned %#v", trace)
	}
	skill := trace.Items[0].GetSkill()
	if skill == nil {
		t.Fatalf("expected skill payload, got %#v", trace.Items[0])
	}
	if got, want := skill.NoSelectionReason, "ambiguous_top_score"; got != want {
		t.Fatalf("NoSelectionReason = %q, want %q", got, want)
	}
	if trace.Summary == nil || trace.Summary.SkillSelected {
		t.Fatalf("unexpected skill summary: %#v", trace.Summary)
	}
}

func TestBuildTracePreservesFileWriteVerificationOutput(t *testing.T) {
	trace := mustBuildTrace(t, &events.RunRecord{RunID: "run_write_1"}, []events.EventRecord{
		{
			Sequence: 1, RunID: "run_write_1", Kind: "tool.call.succeeded", CreatedAt: time.Now(),
			Payload: map[string]any{
				"tool_name": "create_file",
				"output":    `{"path":"/tmp/repo/main.go","bytes":12,"mode":"overwrite","message":"ok","verified_bytes":12,"verified_content":"package main","verification_truncated":false}`,
			},
		},
	})
	if trace == nil || len(trace.Items) != 1 {
		t.Fatalf("BuildTrace returned %#v", trace)
	}
	tc := trace.Items[0].GetToolCall()
	if tc == nil {
		t.Fatalf("expected tool call payload, got %#v", trace.Items[0])
	}
	for _, want := range []string{`"verified_content":"package main"`, `"verified_bytes":12`} {
		if !strings.Contains(tc.Output, want) {
			t.Fatalf("tool output should contain %q, got %s", want, tc.Output)
		}
	}
}

func TestProjectEventToStreamItemRejectsNonObjectPayload(t *testing.T) {
	_, err := ProjectEventToStreamItem(events.EventRecord{
		RunID:   "run_bad",
		Kind:    "agent.message",
		Payload: []string{"not", "an", "object"},
	})
	if err == nil {
		t.Fatal("ProjectEventToStreamItem returned nil error for non-object payload")
	}
	if !strings.Contains(err.Error(), "payload object") {
		t.Fatalf("error = %v, want payload object context", err)
	}
}

func mustBuildTrace(t *testing.T, run *events.RunRecord, raw []events.EventRecord) *Trace {
	t.Helper()
	trace, err := BuildTrace(run, raw)
	if err != nil {
		t.Fatalf("BuildTrace: %v", err)
	}
	return trace
}
