package memory

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestCorrectionUsesEntitiesFromItsOwnEvidence(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	for _, s := range []core.MemorySource{
		{ID: "old-city", Kind: "standalone", ObjectID: "old-city", Version: "1", Speaker: "owner", RunID: "old-run", Body: "我住在杭州", RecordedAt: time.Now()},
		{ID: "new-city", Kind: "standalone", ObjectID: "new-city", Version: "1", Speaker: "owner", RunID: "new-run", Body: "我已搬到苏州", RecordedAt: time.Now()},
	} {
		if _, err := db.RegisterMemorySource(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	old, err := e.Keep(ctx, "old-run", WriteInput{Content: "我住在杭州", SourceID: "old-city", Quote: "我住在杭州", Entities: []string{"杭州"}})
	if err != nil {
		t.Fatal(err)
	}
	corrected, err := e.Correct(ctx, "new-run", CorrectInput{ID: old.ID, Revision: old.Revision, Content: "我住在苏州", SourceID: "new-city", Quote: "我已搬到苏州", Entities: []string{"苏州"}})
	if err != nil || !slices.Equal(corrected.Entities, []string{"苏州"}) {
		t.Fatalf("corrected entity links=%+v err=%v", corrected, err)
	}
	found, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", Entities: []string{"杭州"}})
	if err != nil || len(found) != 0 {
		t.Fatalf("current record linked to former city: %+v %v", found, err)
	}
}

// A new thought takes no revision or state, as a kept fact takes none: they
// belong to an update that lost its id.
func TestANewThoughtRefusesARevisionOrState(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	if _, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "night", Kind: "standalone", ObjectID: "night", Version: "1", Speaker: "owner", RunID: "run-night", Body: "night reflection", RecordedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []WriteInput{
		{Revision: 2, Content: "a stray update", SourceID: "night", Quote: "night reflection"},
		{State: "released", Content: "a stray release", SourceID: "night", Quote: "night reflection"},
	} {
		if _, err := e.Think(ctx, "run-night", "", in); err == nil || !strings.Contains(err.Error(), "give its id") {
			t.Fatalf("Think(%+v) err = %v", in, err)
		}
	}
	thought, err := e.Think(ctx, "run-night", "", WriteInput{Content: "worth checking the logs", SourceID: "night", Quote: "night reflection", Reason: "the job failed twice"})
	if err != nil || thought.Kind != "thought" || thought.Revision != 1 {
		t.Fatalf("new thought = %+v, %v", thought, err)
	}
}
