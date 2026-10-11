package memory

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestConsolidationReconcilesFactsCommittedTogetherInOneCall(t *testing.T) {
	model := &protocolModel{}
	e, db := testEngine(t, model, 0)
	now := time.Now()
	e.cfg.Clock = func() time.Time { return now }
	ctx := context.Background()
	source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "walks", Kind: "standalone", ObjectID: "walks", Version: "1", Speaker: "owner", Body: "工作日我独自散步。周日和熟人结伴。周六去游泳。", RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	var changes []core.MemoryChange
	for _, quote := range []string{"工作日我独自散步。", "周日和熟人结伴。", "周六去游泳。"} {
		changes = append(changes, core.MemoryChange{Reason: "owner statement", Draft: core.MemoryDraft{Kind: "fact", Content: quote, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: quote, Relation: "supports"}}}})
	}
	facts, err := db.CommitMemory(ctx, core.MemoryMutation{Now: now, Changes: changes})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		semanticFixtureVector(t, db, f, []float32{1, 0}, now)
	}
	job, err := db.ClaimMemoryJob(ctx, now, time.Minute, "consolidate")
	if err != nil || job == nil {
		t.Fatalf("consolidation job=%+v err=%v", job, err)
	}
	if err := e.consolidate(ctx, *job); err != nil {
		t.Fatal(err)
	}
	var in consolidationInput
	if model.calls != 1 || json.Unmarshal([]byte(model.seen[0]), &in) != nil || len(in.Changed) != len(facts) {
		t.Fatalf("calls=%d input=%v", model.calls, model.seen)
	}
	status, err := db.MemoryProcessingStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := db.ClaimMemoryJob(ctx, now, time.Minute, "consolidate"); err != nil || next != nil {
		t.Fatalf("consolidation jobs left after the batch: %+v err=%v status=%+v", next, err, status)
	}
}

func TestConsolidationBatchFailsTogether(t *testing.T) {
	model := &protocolModel{}
	e, db := testEngine(t, model, 0)
	now := time.Now()
	e.cfg.Clock = func() time.Time { return now }
	ctx := context.Background()
	a := semanticFixtureFact(t, db, "a", "清晨跑步", now)
	b := semanticFixtureFact(t, db, "b", "傍晚游泳", now)
	semanticFixtureVector(t, db, a, []float32{1, 0}, now)
	semanticFixtureVector(t, db, b, []float32{1, 0}, now)
	e.cfg.Model = insightRevisionModel{target: a.ID, invalid: true}
	job, err := db.ClaimMemoryJob(ctx, now, time.Minute, "consolidate")
	if err != nil || job == nil {
		t.Fatalf("consolidation job=%+v err=%v", job, err)
	}
	if err := e.consolidate(ctx, *job); err == nil {
		t.Fatal("invalid consolidation reply committed")
	}
	if err := db.FailMemoryJob(ctx, *job, "test failure", now); err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Hour)
	ready, err := db.ReadyMemoryJobs(ctx, later, "consolidate", 10)
	if err != nil || len(ready) != 2 {
		t.Fatalf("both jobs must return for retry: %+v err=%v", ready, err)
	}
	for _, r := range ready {
		if r.Attempts != 1 {
			t.Fatalf("job %d attempts=%d, want 1", r.ID, r.Attempts)
		}
	}
}
