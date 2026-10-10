package memory

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type insightRevisionModel struct {
	target  string
	invalid bool
}

func (m insightRevisionModel) GenerateMemory(_ context.Context, _, input string, _ int) (Generation, error) {
	var records []core.MemoryRecord
	if err := json.Unmarshal([]byte(input), &records); err != nil {
		return Generation{}, err
	}
	var insight, changed core.MemoryRecord
	for _, r := range records {
		if r.Kind == "insight" {
			insight = r
		}
		if r.ID == m.target {
			changed = r
		}
	}
	parents := append(slices.Clone(insight.Parents), changed.ID)
	if m.invalid {
		parents = append(parents, "unseen-invented-parent")
	}
	evidence := append(slices.Clone(insight.Evidence), changed.Evidence...)
	out, err := json.Marshal(map[string]any{"changes": []core.MemoryChange{{ExpectedRevision: insight.Revision, Reason: "new experience narrows the scope", Draft: core.MemoryDraft{ID: insight.ID, Kind: "insight", Content: insight.Content, Scope: "私人项目", State: "current", Parents: parents, Evidence: evidence}}}, "links": []core.MemoryLink{}})
	return Generation{Text: string(out)}, err
}

func TestConsolidationRevisionRetainsOriginalParentsOutsideCandidateWindow(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve known parent ids", true: "reject invented parent id"}[invalid], func(t *testing.T) {
			e, db := testEngine(t, &protocolModel{}, 0)
			now := time.Now()
			e.cfg.Clock = func() time.Time { return now }
			a := semanticFixtureFact(t, db, "a", "尝试烹饪", now)
			b := semanticFixtureFact(t, db, "b", "体验陶艺", now)
			evidence := append(slices.Clone(a.Evidence), b.Evidence...)
			insights, err := db.CommitMemory(context.Background(), core.MemoryMutation{Now: now, Changes: []core.MemoryChange{{Reason: "independent experiences", Draft: core.MemoryDraft{Kind: "insight", Content: "通过动手获得理解", Parents: []string{a.ID, b.ID}, Evidence: evidence}}}})
			if err != nil {
				t.Fatal(err)
			}
			c := semanticFixtureFact(t, db, "c", "在私人项目中快速迭代", now)
			for _, r := range []core.MemoryRecord{a, b, insights[0], c} {
				v := []float32{0, 1}
				if r.ID == insights[0].ID || r.ID == c.ID {
					v = []float32{1, 0}
				}
				semanticFixtureVector(t, db, r, v, now)
			}
			e.cfg.Model = insightRevisionModel{target: c.ID, invalid: invalid}
			for {
				job, err := db.ClaimMemoryJob(context.Background(), now, time.Minute, "consolidate")
				if err != nil || job == nil {
					t.Fatalf("missing target job %v", err)
				}
				if job.ObjectID != c.ID {
					if err := db.FinishMemoryJob(context.Background(), *job, "fixture setup", now); err != nil {
						t.Fatal(err)
					}
					continue
				}
				err = e.consolidate(context.Background(), *job)
				if (err != nil) != invalid {
					t.Fatalf("revision invalid=%v err=%v", invalid, err)
				}
				break
			}
			if !invalid {
				r, err := db.ReadMemory(context.Background(), insights[0].ID)
				if err != nil || r.Record.Revision != 2 || len(r.Record.Parents) != 3 {
					t.Fatalf("insight revision=%+v %v", r, err)
				}
			}
		})
	}
}
