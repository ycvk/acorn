package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func aliasFact(t *testing.T, db *Store, id, content, canonical, alias, scope string) core.MemoryRecord {
	t.Helper()
	s := seedMemorySource(t, db, id, "owner", content)
	c := memoryFact(s, content)
	c.Draft.Entities = []string{canonical}
	if alias != "" {
		c.Draft.Aliases = []core.MemoryEntityAlias{{Canonical: canonical, Alias: alias, Scope: scope, SourceID: s.ID, Quote: content}}
	}
	r, err := db.CommitMemory(context.Background(), core.MemoryMutation{Now: memoryTestNow, Changes: []core.MemoryChange{c}})
	if err != nil {
		t.Fatal(err)
	}
	return r[0]
}

func TestMemoryAliasesResolveOnlySupportedUnambiguousIdentity(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	aliasFact(t, db, "name-photo", "摄影组的小岑就是岑舒。", "岑舒", "小岑", "摄影")
	aliasFact(t, db, "name-run", "跑步组的小岑就是岑然。", "岑然", "小岑", "跑步")
	photo := aliasFact(t, db, "photo", "岑舒校准镜头", "岑舒", "", "")
	run := aliasFact(t, db, "run", "岑然安排晨跑", "岑然", "", "")
	for _, tc := range []struct {
		query        string
		want, forbid string
	}{
		{"摄影的小岑负责什么", photo.ID, run.ID},
		{"跑步的小岑负责什么", run.ID, photo.ID},
		{"小岑负责什么", "", photo.ID},
	} {
		r, err := db.SearchMemoryText(ctx, core.MemoryQuery{Query: tc.query, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if tc.want != "" && !slices.ContainsFunc(r, func(r core.MemoryRecord) bool { return r.ID == tc.want }) {
			t.Errorf("missing resolved identity %s: %+v", tc.query, r)
		}
		if slices.ContainsFunc(r, func(r core.MemoryRecord) bool { return r.ID == tc.forbid }) {
			t.Errorf("ambiguous identity expanded %s: %+v", tc.query, r)
		}
	}
}

func TestMemoryAliasRequiresQuotedEvidenceAndRespectsKnownAtAndForget(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	s := seedMemorySource(t, db, "name", "owner", "澄光项目的英文名是 Clearbeam，缩写为 CB。")
	c := memoryFact(s, s.Content)
	c.Draft.Entities = []string{"澄光"}
	c.Draft.Aliases = []core.MemoryEntityAlias{{Canonical: "澄光", Alias: "CB", SourceID: s.ID, Quote: "不存在的别名证据"}}
	if _, err := db.CommitMemory(ctx, core.MemoryMutation{Now: memoryTestNow, Changes: []core.MemoryChange{c}}); err == nil {
		t.Fatal("unquoted identity accepted")
	}
	c.Draft.Aliases[0].Quote = s.Content
	if _, err := db.CommitMemory(ctx, core.MemoryMutation{Now: memoryTestNow, Changes: []core.MemoryChange{c}}); err != nil {
		t.Fatal(err)
	}
	other := aliasFact(t, db, "work", "澄光处理底片", "澄光", "", "")
	for _, q := range []core.MemoryQuery{{Query: "CB 功能"}, {Query: "cb 功能"}, {Query: "CB 功能", KnownAt: memoryTestNow.Add(-time.Minute), Mode: "history"}, {Query: "SCBA 功能"}} {
		r, err := db.SearchMemoryText(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		found := slices.ContainsFunc(r, func(r core.MemoryRecord) bool { return r.ID == other.ID })
		want := q.KnownAt.IsZero() && q.Query != "SCBA 功能"
		if found != want {
			t.Fatalf("alias expansion %s known_at=%s found=%v want=%v", q.Query, q.KnownAt, found, want)
		}
	}
	request := seedMemorySource(t, db, "forget", "owner", "忘记项目别名")
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{s.ID}, RequestSourceID: request.ID, Reason: request.Content, Now: memoryTestNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	r, err := db.SearchMemoryText(ctx, core.MemoryQuery{Query: "CB 功能", Mode: "history", KnownAt: memoryTestNow})
	if err != nil || slices.ContainsFunc(r, func(r core.MemoryRecord) bool { return r.ID == other.ID }) {
		t.Fatalf("forgotten alias influenced retrieval: %+v %v", r, err)
	}
}

func TestMemoryAliasValidityUsesInstantsAcrossTimezones(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	s := seedMemorySource(t, db, "alias", "owner", "澄光改用简称 CB。")
	c := memoryFact(s, s.Content)
	c.Draft.Entities = []string{"澄光"}
	c.Draft.ValidFrom = "2026-10-10T08:30:00+08:00"
	c.Draft.Aliases = []core.MemoryEntityAlias{{Canonical: "澄光", Alias: "CB", SourceID: s.ID, Quote: s.Content}}
	if _, err := db.CommitMemory(ctx, core.MemoryMutation{Now: memoryTestNow, Changes: []core.MemoryChange{c}}); err != nil {
		t.Fatal(err)
	}
	target := aliasFact(t, db, "task", "澄光处理底片", "澄光", "", "")
	for _, tc := range []struct {
		at   string
		want bool
	}{{"2026-10-10T00:15:00Z", false}, {"2026-10-10T00:45:00Z", true}} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		r, err := db.SearchMemoryText(ctx, core.MemoryQuery{Query: "CB 功能", AsOf: at})
		if err != nil || slices.ContainsFunc(r, func(r core.MemoryRecord) bool { return r.ID == target.ID }) != tc.want {
			t.Fatalf("alias validity at %s: %+v %v", tc.at, r, err)
		}
	}
}
