package memory

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

// mergeModel answers a consolidation of exactly two facts with one merge.
// keep picks the survivor out of the two; dropEvidence leaves the duplicate's
// evidence out of the draft.
type mergeModel struct {
	keep         func(a, b core.MemoryRecord) core.MemoryRecord
	dropEvidence bool
}

func (m mergeModel) GenerateMemory(_ context.Context, _, input string, _ int) (Generation, error) {
	var in consolidationInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return Generation{}, err
	}
	facts := append(in.Changed, in.Related...)
	if len(facts) != 2 {
		return Generation{}, errors.New("merge fixture expects exactly two facts")
	}
	survivor := m.keep(facts[0], facts[1])
	duplicate := facts[0]
	if duplicate.ID == survivor.ID {
		duplicate = facts[1]
	}
	evidence := slices.Clone(survivor.Evidence)
	if !m.dropEvidence {
		evidence = append(evidence, duplicate.Evidence...)
	}
	entities := slices.Clone(survivor.Entities)
	entities = append(entities, duplicate.Entities...)
	change := core.MemoryChange{
		Draft:            core.MemoryDraft{ID: survivor.ID, Kind: "fact", Content: survivor.Content, Scope: survivor.Scope, State: "current", Pinned: survivor.Pinned, Entities: entities, Evidence: evidence},
		ExpectedRevision: survivor.Revision,
		Merges:           duplicate.ID,
		MergesRevision:   duplicate.Revision,
		Reason:           "both statements make the same claim",
	}
	out, err := json.Marshal(map[string]any{"changes": []core.MemoryChange{change}, "links": []core.MemoryLink{}})
	return Generation{Text: string(out)}, err
}

func earlierRecorded(a, b core.MemoryRecord) core.MemoryRecord {
	if a.RecordedAt.Before(b.RecordedAt) {
		return a
	}
	return b
}

func laterRecorded(a, b core.MemoryRecord) core.MemoryRecord {
	if a.RecordedAt.Before(b.RecordedAt) {
		return b
	}
	return a
}

func pinnedOne(a, b core.MemoryRecord) core.MemoryRecord {
	if a.Pinned {
		return a
	}
	return b
}

// mergeFact commits a fact from its own owner source at the given time.
func mergeFact(t *testing.T, db *store.Store, id, content string, pinned bool, at time.Time) core.MemoryRecord {
	t.Helper()
	ctx := context.Background()
	source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: "standalone", ObjectID: id, Version: "1", Speaker: "owner", Body: content, RecordedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	records, err := db.CommitMemory(ctx, core.MemoryMutation{Now: at, Changes: []core.MemoryChange{{Reason: "owner statement", Draft: core.MemoryDraft{Kind: "fact", Content: content, Pinned: pinned, Entities: []string{"饮食"}, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: content}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return records[0]
}

// runMergeConsolidation consolidates the two facts in one call and returns
// the outcome. The first fact is recorded an hour before the second.
func runMergeConsolidation(t *testing.T, model Model, firstPinned, secondPinned bool) (*Engine, core.MemoryRecord, core.MemoryRecord, error) {
	t.Helper()
	e, db := testEngine(t, &protocolModel{}, 0)
	now := time.Now()
	e.cfg.Clock = func() time.Time { return now.Add(2 * time.Hour) }
	first := mergeFact(t, db, "first", "我不吃香菜", firstPinned, now)
	second := mergeFact(t, db, "second", "owner 不吃香菜", secondPinned, now.Add(time.Hour))
	e.cfg.Model = model
	job, err := db.ClaimMemoryJob(context.Background(), e.cfg.Clock(), time.Minute, "consolidate")
	if err != nil || job == nil {
		t.Fatalf("consolidation job=%+v err=%v", job, err)
	}
	return e, first, second, e.consolidate(context.Background(), *job)
}

func TestConsolidationMergesDuplicateFactsIntoTheSurvivorTheCodeChooses(t *testing.T) {
	cases := []struct {
		name                      string
		firstPinned, secondPinned bool
		keep                      func(a, b core.MemoryRecord) core.MemoryRecord
		survivorIsFirst           bool
	}{
		{"earlier fact survives", false, false, earlierRecorded, true},
		{"pinned fact survives even when recorded later", false, true, pinnedOne, false},
		{"both pinned, earlier survives", true, true, earlierRecorded, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, first, second, err := runMergeConsolidation(t, mergeModel{keep: tc.keep}, tc.firstPinned, tc.secondPinned)
			if err != nil {
				t.Fatal(err)
			}
			db := e.cfg.Store
			survivor, duplicate := first, second
			if !tc.survivorIsFirst {
				survivor, duplicate = second, first
			}
			kept, err := db.ReadMemory(context.Background(), survivor.ID)
			if err != nil {
				t.Fatal(err)
			}
			if kept.Record.State != "current" || kept.Record.Revision != 2 || len(kept.Record.Evidence) != 2 || kept.Record.Pinned != survivor.Pinned || kept.Record.Content != survivor.Content {
				t.Fatalf("survivor=%+v", kept.Record)
			}
			retired, err := db.ReadMemory(context.Background(), duplicate.ID)
			if err != nil {
				t.Fatal(err)
			}
			if retired.Record.State != "merged" || !slices.Contains(retired.Links, core.MemoryLink{FromID: duplicate.ID, ToID: survivor.ID, Relation: "merged_into"}) {
				t.Fatalf("duplicate=%+v links=%+v", retired.Record, retired.Links)
			}
		})
	}
}

func TestConsolidationRejectsMergesTheModelGetsWrong(t *testing.T) {
	cases := []struct {
		name  string
		model mergeModel
	}{
		{"keeps the later fact", mergeModel{keep: laterRecorded}},
		{"keeps the unpinned fact", mergeModel{keep: func(a, b core.MemoryRecord) core.MemoryRecord {
			if a.Pinned {
				return b
			}
			return a
		}}},
		{"drops the duplicate's evidence", mergeModel{keep: earlierRecorded, dropEvidence: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The first fact is pinned only for the unpinned-survivor case.
			e, first, second, err := runMergeConsolidation(t, tc.model, tc.name == "keeps the unpinned fact", false)
			if err == nil {
				t.Fatal("invalid merge committed")
			}
			db := e.cfg.Store
			for _, r := range []core.MemoryRecord{first, second} {
				got, err := db.ReadMemory(context.Background(), r.ID)
				if err != nil || got.Record.State != "current" || got.Record.Revision != 1 || len(got.Links) != 0 {
					t.Fatalf("record changed by a rejected merge: %+v %v", got, err)
				}
			}
		})
	}
}

func TestMergedFactLeavesCurrentRecallAndStaysInHistory(t *testing.T) {
	e, first, second, err := runMergeConsolidation(t, mergeModel{keep: earlierRecorded}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(hits []core.MemoryHit) []string {
		out := make([]string, 0, len(hits))
		for _, h := range hits {
			out = append(out, h.Record.ID)
		}
		slices.Sort(out)
		return out
	}
	current, err := e.Recall(context.Background(), core.MemoryQuery{Query: "香菜", Mode: "current"})
	if err != nil || !slices.Equal(ids(current.Hits), []string{first.ID}) {
		t.Fatalf("current recall=%v err=%v", ids(current.Hits), err)
	}
	history, err := e.Recall(context.Background(), core.MemoryQuery{Query: "香菜", Mode: "history"})
	both := []string{first.ID, second.ID}
	slices.Sort(both)
	if err != nil || !slices.Equal(ids(history.Hits), both) {
		t.Fatalf("history recall=%v err=%v", ids(history.Hits), err)
	}
}
