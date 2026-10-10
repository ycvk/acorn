package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// The document layout written before records and revisions became columns.
const legacyMemoryDocumentSchema = `
CREATE TABLE memory_sources (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, object_id TEXT NOT NULL, version TEXT NOT NULL,
 speaker TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '', run_id TEXT NOT NULL DEFAULT '',
 body TEXT NOT NULL DEFAULT '', recorded_at TEXT NOT NULL, occurred_at TEXT NOT NULL DEFAULT '',
 UNIQUE(kind, object_id, version)
);
CREATE TABLE memory_records (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, content TEXT NOT NULL, scope TEXT NOT NULL,
 state TEXT NOT NULL, revision INTEGER NOT NULL, recorded_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 valid_from TEXT NOT NULL DEFAULT '', valid_to TEXT NOT NULL DEFAULT '', excluded INTEGER NOT NULL DEFAULT 0,
 data TEXT NOT NULL
);
CREATE TABLE memory_revisions (
 record_id TEXT NOT NULL REFERENCES memory_records(id), revision INTEGER NOT NULL,
 at TEXT NOT NULL, reason TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(record_id,revision)
);
CREATE TABLE memory_evidence (
 record_id TEXT NOT NULL REFERENCES memory_records(id), source_id TEXT NOT NULL REFERENCES memory_sources(id),
 quote TEXT NOT NULL, relation TEXT NOT NULL, PRIMARY KEY(record_id, source_id, quote, relation)
);
CREATE TABLE memory_entity_aliases (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, canonical TEXT NOT NULL,
 alias TEXT NOT NULL, scope TEXT NOT NULL, source_id TEXT NOT NULL, quote TEXT NOT NULL,
 PRIMARY KEY(record_id,revision,canonical,alias,scope)
);
`

func TestMemoryDocumentsMoveIntoColumns(t *testing.T) {
	dir := t.TempDir()
	raw, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(legacyMemoryDocumentSchema); err != nil {
		t.Fatal(err)
	}
	at := memoryTestNow
	stamp := formatTimestamp(at)
	if _, err := raw.Exec(`INSERT INTO memory_sources(id,kind,object_id,version,speaker,body,recorded_at) VALUES('s1','standalone','s1','1','owner','我住在杭州，小岑是岑溪',?)`, stamp); err != nil {
		t.Fatal(err)
	}
	evidence := []core.MemoryEvidence{{SourceID: "s1", Quote: "我住在杭州", Relation: "supports"}}
	first := core.MemoryRecord{ID: "fact", Kind: "fact", Content: "住在上海", State: "current", Basis: "direct", Revision: 1, RecordedAt: at, UpdatedAt: at, Entities: []string{"上海"}, Evidence: evidence, Parents: []string{}, SourceIDs: []string{"s1"}}
	second := first
	second.Revision, second.Content, second.State, second.UpdatedAt = 2, "住在杭州", "disputed", at.Add(time.Hour)
	second.ValidFrom = time.Date(2026, 10, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	second.Entities = []string{"岑溪", "杭州"}
	second.Evidence = []core.MemoryEvidence{evidence[0], {SourceID: "s1", Quote: "小岑是岑溪", Relation: "supports"}}
	second.Aliases = []core.MemoryEntityAlias{{Canonical: "岑溪", Alias: "小岑", SourceID: "s1", Quote: "小岑是岑溪"}}
	insight := core.MemoryRecord{ID: "insight", Kind: "insight", Content: "常住江南", State: "current", Basis: "inferred", Revision: 1, RecordedAt: at, UpdatedAt: at, NeedsReview: true, Pinned: true, Entities: []string{}, Evidence: []core.MemoryEvidence{}, Parents: []string{"fact"}, SourceIDs: []string{"s1"}}
	for _, r := range []core.MemoryRecord{first, second, insight} {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if r.Revision == 1 {
			if _, err := raw.Exec(`INSERT INTO memory_records(id,kind,content,scope,state,revision,recorded_at,updated_at,data) VALUES(?,?,?,?,?,?,?,?,?)`, r.ID, r.Kind, r.Content, r.Scope, r.State, r.Revision, stamp, formatTimestamp(r.UpdatedAt), string(data)); err != nil {
				t.Fatal(err)
			}
		} else if _, err := raw.Exec(`UPDATE memory_records SET content=?,state=?,revision=?,updated_at=?,valid_from=?,data=? WHERE id=?`, r.Content, r.State, r.Revision, formatTimestamp(r.UpdatedAt), formatTimestamp(r.ValidFrom), string(data), r.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`INSERT INTO memory_revisions(record_id,revision,at,reason,data) VALUES(?,?,?,'legacy',?)`, r.ID, r.Revision, formatTimestamp(r.UpdatedAt), string(data)); err != nil {
			t.Fatal(err)
		}
		for _, a := range r.Aliases {
			if _, err := raw.Exec(`INSERT INTO memory_entity_aliases VALUES(?,?,?,?,?,?,?)`, r.ID, r.Revision, a.Canonical, a.Alias, a.Scope, a.SourceID, a.Quote); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := raw.Exec(`INSERT INTO memory_evidence VALUES('fact','s1','我住在杭州','supports')`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open legacy documents: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	read, err := s.ReadMemory(ctx, "fact")
	if err != nil {
		t.Fatal(err)
	}
	second.ValidFrom = second.ValidFrom.UTC()
	for i, want := range []core.MemoryRecord{first, second} {
		if got := read.Versions[i]; !reflect.DeepEqual(got, want) {
			t.Fatalf("revision %d:\n got %+v\nwant %+v", want.Revision, got, want)
		}
	}
	if !reflect.DeepEqual(read.Record, second) {
		t.Fatalf("current record:\n got %+v\nwant %+v", read.Record, second)
	}
	derived, err := s.MemoryRecordsByIDs(ctx, []string{"insight"}, core.MemoryQuery{Mode: "history"})
	if err != nil || len(derived) != 1 || !reflect.DeepEqual(derived[0], insight) {
		t.Fatalf("insight = %+v, %v", derived, err)
	}
	for table, column := range map[string]string{"memory_records": "data", "memory_revisions": "data"} {
		columns, err := s.tableColumns(table)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := columns[column]; ok {
			t.Fatalf("%s.%s remains", table, column)
		}
	}
	var leftover int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='memory_evidence'`).Scan(&leftover); err != nil || leftover != 0 {
		t.Fatalf("memory_evidence remains: %d %v", leftover, err)
	}
}
