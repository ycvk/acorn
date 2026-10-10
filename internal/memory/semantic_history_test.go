package memory

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

func semanticFixtureFact(t *testing.T, db *store.Store, id, content string, now time.Time) core.MemoryRecord {
	t.Helper()
	source, err := db.RegisterMemorySource(context.Background(), core.MemorySource{ID: id, Kind: "standalone", ObjectID: id, Version: "1", Speaker: "owner", Body: content, RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	records, err := db.CommitMemory(context.Background(), core.MemoryMutation{Now: now, Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: content, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: content}}}, Reason: "owner evidence"}}})
	if err != nil {
		t.Fatal(err)
	}
	return records[0]
}

func semanticFixtureVector(t *testing.T, db *store.Store, record core.MemoryRecord, values []float32, now time.Time) {
	t.Helper()
	job, err := db.ClaimMemoryJob(context.Background(), now, time.Minute, "embed")
	if err != nil || job == nil || job.ObjectID != record.ID || job.Version != fmt.Sprint(record.Revision) {
		t.Fatalf("embedding job=%+v err=%v", job, err)
	}
	if err := db.SaveMemoryVectors(context.Background(), *job, []core.MemoryVector{{ID: record.ID, Revision: record.Revision, Generation: 1, Values: values}}, now); err != nil {
		t.Fatal(err)
	}
}

func TestConsolidationFindsRelatedEvidenceWithoutSharedWordsOrEntities(t *testing.T) {
	model := &protocolModel{}
	e, db := testEngine(t, model, 0)
	now := time.Now()
	a := semanticFixtureFact(t, db, "first", "先实际体验小工具", now)
	b := semanticFixtureFact(t, db, "second", "Use a prototype before planning", now)
	semanticFixtureVector(t, db, a, []float32{1, 0}, now)
	semanticFixtureVector(t, db, b, []float32{1, 0}, now)
	job, err := db.ClaimMemoryJob(context.Background(), now, time.Minute, "consolidate")
	if err != nil || job == nil {
		t.Fatalf("consolidation job=%+v err=%v", job, err)
	}
	if err := e.consolidate(context.Background(), *job); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || !strings.Contains(model.seen[0], a.ID) || !strings.Contains(model.seen[0], b.ID) {
		t.Fatalf("semantic evidence did not reach consolidation: %v", model.seen)
	}
}

func TestSemanticHistoryUsesTheVectorOfTheKnownRevision(t *testing.T) {
	for _, tc := range []struct {
		name         string
		old, current []float32
		want         int
	}{
		{"past meaning remains searchable", []float32{1, 0}, []float32{0, 1}, 1},
		{"future meaning cannot rank old evidence", []float32{0, 1}, []float32{1, 0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, db := testEngine(t, &protocolModel{}, 0)
			now := time.Now().Add(-time.Hour)
			r := semanticFixtureFact(t, db, "first", "清晨读书", now)
			semanticFixtureVector(t, db, r, tc.old, now)
			newSource, err := db.RegisterMemorySource(context.Background(), core.MemorySource{ID: "second", Kind: "standalone", ObjectID: "second", Version: "1", Speaker: "owner", Body: "傍晚游泳", RecordedAt: now.Add(time.Minute)})
			if err != nil {
				t.Fatal(err)
			}
			revised, err := db.CommitMemory(context.Background(), core.MemoryMutation{Now: now.Add(time.Minute), Changes: []core.MemoryChange{{ExpectedRevision: 1, Draft: core.MemoryDraft{ID: r.ID, Kind: "fact", Content: newSource.Content, Evidence: []core.MemoryEvidence{{SourceID: newSource.ID, Quote: newSource.Content}}}, Reason: "updated understanding"}}})
			if err != nil {
				t.Fatal(err)
			}
			semanticFixtureVector(t, db, revised[0], tc.current, now.Add(time.Minute))
			got, err := e.semanticCandidates(context.Background(), core.MemoryQuery{Query: "semantic only", Mode: "history", KnownAt: now.Add(30 * time.Second)}, e.cfg.Index)
			if err != nil || len(got) != tc.want {
				t.Fatalf("historical semantic candidates=%+v err=%v want=%d", got, err, tc.want)
			}
			if len(got) > 0 && got[0].Revision != 1 {
				t.Fatalf("future revision returned: %+v", got)
			}
		})
	}
}
