package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestMemoryReadPagesAllRevisionsAndEscapedSources(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	now := time.Now()
	content := "第一条原话" + strings.Repeat("\"\\\n", 1500)
	source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:pages", Kind: "standalone", ObjectID: "pages", Version: "1", Speaker: "owner", Body: content, RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	draft := core.MemoryDraft{Kind: "fact", Content: "第一条原话", Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: "第一条原话", Relation: "supports"}}}
	records, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: draft, Reason: "owner statement"}}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	for revision := int64(1); revision < 4; revision++ {
		draft.ID = records[0].ID
		draft.Scope = strings.Repeat("情境", int(revision))
		_, err = db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: draft, ExpectedRevision: revision, Reason: "scope"}}, Now: now.Add(time.Duration(revision) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
	}
	cursor := ""
	versions := map[int64]bool{}
	var text strings.Builder
	for page := 0; page < 40; page++ {
		out, err := e.Read(ctx, ReadInput{ID: records[0].ID, Cursor: cursor, MaxTokens: 1600})
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := e.jsonTokens(ctx, out)
		if err != nil || tokens > 1600 {
			t.Fatalf("page overflow %d %v", tokens, err)
		}
		for _, v := range out.Versions {
			if versions[v.Revision] {
				t.Fatal("duplicate revision")
			}
			versions[v.Revision] = true
		}
		if out.Source != nil {
			text.WriteString(out.Source.Content)
		}
		cursor = out.NextCursor
		if cursor == "" {
			break
		}
	}
	if cursor != "" || len(versions) != 4 || text.String() != content {
		t.Fatalf("incomplete pages revisions=%d bytes=%d cursor=%s", len(versions), text.Len(), cursor)
	}
}
