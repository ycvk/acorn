package memory

import (
	"context"
	"slices"
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
