package store

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestOwnerRequestSourcesAreNeverExtracted(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now()
	for _, source := range []core.MemorySource{
		{ID: "request:1", Kind: core.MemoryRequestSource, ObjectID: "request:1", Version: "1", Speaker: "owner", Body: "我现在只喝茶", RecordedAt: now},
		{ID: "owner:1", Kind: "standalone", ObjectID: "owner:1", Version: "1", Speaker: "owner", Body: "我养了一只猫", RecordedAt: now},
	} {
		if _, err := s.RegisterMemorySource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT object_id FROM memory_jobs WHERE operation='extract' ORDER BY object_id`)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := memoryStringRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(extracted) != 1 || extracted[0] != "owner:1" {
		t.Fatalf("extract jobs for %v, want only the owner's statement", extracted)
	}
}
