package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/model"
	storecore "github.com/ycvk/acorn/internal/store"
)

func TestSavePlan_CreateNew(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	plan := &model.Plan{
		PlanID:    "plan_1",
		SessionID: "sess_1",
		RunID:     "run_1",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "read the codebase", Status: "pending", DependsOn: []string{}},
			{ID: "s2", Action: "write tests", Status: "pending", DependsOn: []string{"s1"}},
			{ID: "s3", Action: "run tests", Status: "pending", DependsOn: []string{"s2"}},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	loaded, err := store.LoadPlanBySession(ctx, "sess_1")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}

	if loaded.PlanID != "plan_1" {
		t.Fatalf("PlanID = %q, want %q", loaded.PlanID, "plan_1")
	}
	if loaded.SessionID != "sess_1" {
		t.Fatalf("SessionID = %q, want %q", loaded.SessionID, "sess_1")
	}
	if loaded.RunID != "run_1" {
		t.Fatalf("RunID = %q, want %q", loaded.RunID, "run_1")
	}
	if len(loaded.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want 3", len(loaded.Steps))
	}
	if loaded.Steps[0].ID != "s1" || loaded.Steps[0].Action != "read the codebase" || loaded.Steps[0].Status != "pending" {
		t.Fatalf("Step[0] = %+v, want s1/read the codebase/pending", loaded.Steps[0])
	}
	if len(loaded.Steps[1].DependsOn) != 1 || loaded.Steps[1].DependsOn[0] != "s1" {
		t.Fatalf("Step[1].DependsOn = %v, want [s1]", loaded.Steps[1].DependsOn)
	}
	if loaded.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should not be zero")
	}
	if loaded.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should not be zero")
	}
}

func TestSavePlan_RoundtripsRepoAwareMetadata(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC()
	plan := &model.Plan{
		PlanID:    "plan_repo_aware",
		SessionID: "sess_repo_aware",
		RunID:     "run_repo_aware",
		Steps: []model.PlanStep{{
			ID:        "s1",
			Action:    "update runtime plan metadata",
			Status:    "pending",
			DependsOn: []string{},
			RepoTargets: []model.PlanRepoTarget{{
				Path:       "internal/model/plan.go",
				Symbol:     "model.PlanStep",
				StartLine:  30,
				EndLine:    44,
				Reason:     "plan metadata belongs on model.PlanStep",
				Confidence: "high",
			}},
			VerificationIntent: []model.VerificationIntent{{
				Kind:    "test",
				Command: []string{"go", "test", "./internal/runtime"},
				Paths:   []string{"internal/runtime"},
				Reason:  "runtime plan tests cover metadata",
			}},
			Risk:      "write",
			ToolHints: []string{"read_file", "apply_unified_patch"},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	loaded, err := store.LoadPlanBySession(ctx, "sess_repo_aware")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}
	if len(loaded.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(loaded.Steps))
	}
	step := loaded.Steps[0]
	if step.Risk != "write" {
		t.Fatalf("Risk = %q, want write", step.Risk)
	}
	if len(step.RepoTargets) != 1 || step.RepoTargets[0].Path != "internal/model/plan.go" || step.RepoTargets[0].Confidence != "high" {
		t.Fatalf("RepoTargets = %+v", step.RepoTargets)
	}
	if len(step.VerificationIntent) != 1 || step.VerificationIntent[0].Kind != "test" || len(step.VerificationIntent[0].Command) != 3 {
		t.Fatalf("model.VerificationIntent = %+v", step.VerificationIntent)
	}
	if len(step.ToolHints) != 2 || step.ToolHints[1] != "apply_unified_patch" {
		t.Fatalf("ToolHints = %+v", step.ToolHints)
	}
}

func TestSavePlan_RoundtripsEvidence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC()
	plan := &model.Plan{
		PlanID:    "plan_evidence",
		SessionID: "sess_evidence",
		RunID:     "run_evidence",
		Steps: []model.PlanStep{{
			ID:     "s1",
			Action: "verify evidence roundtrip",
			Status: "completed",
			Evidence: []model.PlanEvidence{{
				ID:          "ev_1",
				StepID:      "s1",
				Kind:        "test",
				Status:      "passed",
				Summary:     "go test ./internal/runtime passed",
				ToolName:    "run_command",
				Command:     []string{"go", "test", "./internal/runtime"},
				Paths:       []string{"internal/runtime"},
				DiffRef:     "diff_1",
				ChildRunID:  "child_1",
				SourceRunID: "run_evidence",
				RecordedAt:  now,
			}},
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	loaded, err := store.LoadPlanBySession(ctx, "sess_evidence")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}
	if len(loaded.Steps) != 1 || len(loaded.Steps[0].Evidence) != 1 {
		t.Fatalf("loaded evidence = %+v", loaded.Steps)
	}
	ev := loaded.Steps[0].Evidence[0]
	if ev.StepID != "s1" || ev.Kind != "test" || len(ev.Command) != 3 || ev.DiffRef != "diff_1" {
		t.Fatalf("evidence = %+v", ev)
	}
}

func TestSavePlan_UpdateExisting(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	plan := &model.Plan{
		PlanID:    "plan_up",
		SessionID: "sess_up",
		RunID:     "run_1",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "step one", Status: "pending"},
			{ID: "s2", Action: "step two", Status: "pending"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan (create): %v", err)
	}

	updated := &model.Plan{
		PlanID:    "plan_up",
		SessionID: "sess_up",
		RunID:     "run_2",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "step one", Status: "completed"},
			{ID: "s2", Action: "step two", Status: "in_progress"},
			{ID: "s3", Action: "step three", Status: "pending"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SavePlan(ctx, updated); err != nil {
		t.Fatalf("SavePlan (update): %v", err)
	}

	loaded, err := store.LoadPlanBySession(ctx, "sess_up")
	if err != nil {
		t.Fatalf("LoadPlanBySession: %v", err)
	}

	if len(loaded.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want 3 after update", len(loaded.Steps))
	}
	if loaded.Steps[0].Status != "completed" {
		t.Fatalf("Step[0].Status = %q, want completed", loaded.Steps[0].Status)
	}
	if loaded.Steps[1].Status != "in_progress" {
		t.Fatalf("Step[1].Status = %q, want in_progress", loaded.Steps[1].Status)
	}
	if loaded.RunID != "run_2" {
		t.Fatalf("RunID = %q, want run_2", loaded.RunID)
	}
}

func TestSavePlan_EmptyStepsDeletesRow(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	plan := &model.Plan{
		PlanID:    "plan_del",
		SessionID: "sess_del",
		RunID:     "run_del",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "do something", Status: "pending"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan (create): %v", err)
	}

	clearPlan := &model.Plan{
		PlanID: "plan_del",
		Steps:  nil,
	}
	if err := store.SavePlan(ctx, clearPlan); err != nil {
		t.Fatalf("SavePlan (clear): %v", err)
	}

	_, err = store.LoadPlanBySession(ctx, "sess_del")
	if err == nil {
		t.Fatal("expected storecore.ErrPlanNotFound after clearing steps, got nil error")
	}
	if !errors.Is(err, storecore.ErrPlanNotFound) {
		t.Fatalf("expected storecore.ErrPlanNotFound, got %v", err)
	}
}

func TestLoadPlanBySession_NotFound(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	_, err = store.LoadPlanBySession(ctx, "nonexistent_session")
	if err == nil {
		t.Fatal("expected storecore.ErrPlanNotFound, got nil error")
	}
	if !errors.Is(err, storecore.ErrPlanNotFound) {
		t.Fatalf("expected storecore.ErrPlanNotFound, got %v", err)
	}
}

func TestLoadPlanByRun(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	plan := &model.Plan{
		PlanID:    "plan_run",
		SessionID: "sess_run",
		RunID:     "run_42",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "inspect", Status: "in_progress"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	loaded, err := store.LoadPlanByRun(ctx, "run_42")
	if err != nil {
		t.Fatalf("LoadPlanByRun: %v", err)
	}
	if loaded.PlanID != "plan_run" {
		t.Fatalf("PlanID = %q, want plan_run", loaded.PlanID)
	}
	if loaded.SessionID != "sess_run" {
		t.Fatalf("SessionID = %q, want sess_run", loaded.SessionID)
	}
	if len(loaded.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(loaded.Steps))
	}
	if loaded.Steps[0].Status != "in_progress" {
		t.Fatalf("Step[0].Status = %q, want in_progress", loaded.Steps[0].Status)
	}
}

func TestLoadPlanByRun_NotFound(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	_, err = store.LoadPlanByRun(ctx, "nonexistent_run")
	if err == nil {
		t.Fatal("expected storecore.ErrPlanNotFound, got nil error")
	}
	if !errors.Is(err, storecore.ErrPlanNotFound) {
		t.Fatalf("expected storecore.ErrPlanNotFound, got %v", err)
	}
}

func TestDeletePlanBySession(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Now().UTC()

	plan := &model.Plan{
		PlanID:    "plan_del_sess",
		SessionID: "sess_del_by_session",
		RunID:     "run_del_sess",
		Steps: []model.PlanStep{
			{ID: "s1", Action: "do work", Status: "pending"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	if err := store.DeletePlanBySession(ctx, "sess_del_by_session"); err != nil {
		t.Fatalf("DeletePlanBySession: %v", err)
	}

	_, err = store.LoadPlanBySession(ctx, "sess_del_by_session")
	if err == nil {
		t.Fatal("expected storecore.ErrPlanNotFound after delete, got nil error")
	}
	if !errors.Is(err, storecore.ErrPlanNotFound) {
		t.Fatalf("expected storecore.ErrPlanNotFound, got %v", err)
	}
}
