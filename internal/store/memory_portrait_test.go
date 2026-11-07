package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func claimPortrait(t *testing.T, s *Store, version string, now time.Time) core.MemoryJob {
	t.Helper()
	ctx := context.Background()
	if err := s.QueuePortrait(ctx, version, now); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimMemoryJob(ctx, now, time.Minute, core.PortraitOperation)
	if err != nil || job == nil || job.Version != version {
		t.Fatalf("claim portrait %s = %+v, %v", version, job, err)
	}
	return *job
}

func portraitContents(lines []core.PortraitLine, state string) []string {
	var out []string
	for _, line := range lines {
		if line.State == state {
			out = append(out, line.Content)
		}
	}
	return out
}

func TestPortraitRefreshReplacesShownLinesAndKeepsRemovedOnes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := seedMemorySource(t, s, "owner:a", "owner", "我在做一个叫 Acorn 的个人 agent")
	b := seedMemorySource(t, s, "owner:b", "owner", "展开讲讲：Anthropic 提交上市文件")
	first := memoryTestNow.Add(time.Hour)
	job := claimPortrait(t, s, "day:2026-10-09", first)
	if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: job, Now: first, Lines: []core.PortraitDraft{
		{Content: "在做个人 agent Acorn", SourceIDs: []string{a.ID}},
		{Content: "关注 AI 公司动态", SourceIDs: []string{b.ID, a.ID}},
	}}); err != nil {
		t.Fatal(err)
	}
	lines, err := s.Portrait(ctx)
	if err != nil || len(lines) != 2 || !slices.Equal(lines[1].SourceIDs, []string{b.ID, a.ID}) || !lines[0].CreatedAt.Equal(first) {
		t.Fatalf("portrait = %+v, %v", lines, err)
	}
	if refreshed, err := s.PortraitRefreshedAt(ctx); err != nil || !refreshed.Equal(first) {
		t.Fatalf("refreshed at %v, %v; want %v", refreshed, err, first)
	}
	if err := s.RemovePortraitLine(ctx, lines[1].ID, first); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePortraitLine(ctx, lines[1].ID, first); !errors.Is(err, core.ErrPortraitLineNotFound) {
		t.Fatalf("second removal = %v", err)
	}

	second := first.Add(24 * time.Hour)
	job = claimPortrait(t, s, "day:2026-10-10", second)
	if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: job, Now: second, Lines: []core.PortraitDraft{{Content: "在用 Go 写 Acorn", SourceIDs: []string{a.ID}}}}); err != nil {
		t.Fatal(err)
	}
	lines, err = s.Portrait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if shown, removed := portraitContents(lines, core.PortraitLineShown), portraitContents(lines, core.PortraitLineRemoved); !slices.Equal(shown, []string{"在用 Go 写 Acorn"}) || !slices.Equal(removed, []string{"关注 AI 公司动态"}) {
		t.Fatalf("shown %v removed %v", shown, removed)
	}
	if lines[1].State != core.PortraitLineRemoved || !lines[1].RemovedAt.Equal(first) {
		t.Fatalf("removed line = %+v", lines[1])
	}
}

func TestPortraitCommitWritesNothingItCannotStandBehind(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := seedMemorySource(t, s, "owner:a", "owner", "我住在新加坡")
	now := memoryTestNow.Add(time.Hour)
	job := claimPortrait(t, s, "day:2026-10-09", now)
	for name, tc := range map[string]struct {
		lines []core.PortraitDraft
		want  error
	}{
		"unknown source": {[]core.PortraitDraft{{Content: "住在新加坡", SourceIDs: []string{a.ID}}, {Content: "养猫", SourceIDs: []string{"owner:missing"}}}, core.ErrMemoryNotFound},
		"credential":     {[]core.PortraitDraft{{Content: "常用的 key 是 sk-ant-api03-" + fmt.Sprintf("%040d", 7), SourceIDs: []string{a.ID}}}, core.ErrMemoryCredential},
	} {
		if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: job, Now: now, Lines: tc.lines}); !errors.Is(err, tc.want) {
			t.Fatalf("%s: commit = %v, want %v", name, err, tc.want)
		}
	}
	if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: job, Now: now, Lines: []core.PortraitDraft{{Content: "没有依据", SourceIDs: nil}}}); err == nil {
		t.Fatal("a line without sources was written")
	}
	stale := job
	stale.Token--
	if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: stale, Now: now, Lines: []core.PortraitDraft{{Content: "住在新加坡", SourceIDs: []string{a.ID}}}}); !errors.Is(err, core.ErrMemoryLeaseLost) {
		t.Fatalf("stale lease commit = %v", err)
	}
	if lines, err := s.Portrait(ctx); err != nil || len(lines) != 0 {
		t.Fatalf("portrait after refused commits = %+v, %v", lines, err)
	}
}

func TestForgettingASourceDeletesThePortraitLinesCitingIt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := seedMemorySource(t, s, "owner:a", "owner", "我在准备搬家去东京")
	b := seedMemorySource(t, s, "owner:b", "owner", "我每天早上跑步")
	c := seedMemorySource(t, s, "owner:c", "owner", "随便聊聊")
	now := memoryTestNow.Add(time.Hour)
	job := claimPortrait(t, s, "day:2026-10-09", now)
	if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: job, Now: now, Lines: []core.PortraitDraft{
		{Content: "准备搬去东京", SourceIDs: []string{a.ID}},
		{Content: "每天早上跑步", SourceIDs: []string{b.ID}},
		{Content: "跑步也在东京的计划里", SourceIDs: []string{a.ID, b.ID}},
	}}); err != nil {
		t.Fatal(err)
	}
	lines, err := s.Portrait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePortraitLine(ctx, lines[2].ID, now); err != nil {
		t.Fatal(err)
	}
	pending := claimPortrait(t, s, "day:2026-10-10", now.Add(time.Minute))

	request := seedMemorySource(t, s, "request:c", "owner", "忘掉随便聊聊那句")
	at := now.Add(time.Hour)
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{c.ID}, RequestSourceID: request.ID, Reason: "owner asked", Now: at}); err != nil {
		t.Fatal(err)
	}
	forgetRefreshes := func(at time.Time) []string {
		t.Helper()
		jobs, err := s.ReadyMemoryJobs(ctx, at, core.PortraitOperation, 10)
		if err != nil {
			t.Fatal(err)
		}
		var versions []string
		for _, job := range jobs {
			if strings.HasPrefix(job.Version, "forget:") {
				versions = append(versions, job.Version)
			}
		}
		return versions
	}
	if queued := forgetRefreshes(at); len(queued) != 0 {
		t.Fatalf("forgetting an uncited source queued %v", queued)
	}

	request = seedMemorySource(t, s, "request:a", "owner", "忘掉搬家的事")
	at = at.Add(time.Hour)
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{a.ID}, RequestSourceID: request.ID, Reason: "owner asked", Now: at}); err != nil {
		t.Fatal(err)
	}
	lines, err = s.Portrait(ctx)
	if err != nil || len(lines) != 1 || lines[0].Content != "每天早上跑步" {
		t.Fatalf("portrait after forgetting a = %+v, %v", lines, err)
	}
	var left int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM portrait_line_sources WHERE source_id=?`, a.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d line sources still cite a, %v", left, err)
	}
	epoch, err := s.MemoryEpoch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if queued := forgetRefreshes(at); !slices.Equal(queued, []string{fmt.Sprintf("forget:%d", epoch)}) {
		t.Fatalf("refresh after forgetting a shown line's source = %v", queued)
	}
	if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: pending, Now: at, Lines: []core.PortraitDraft{{Content: "准备搬去东京", SourceIDs: []string{b.ID}}}}); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("refresh read before the forget committed: %v", err)
	}
}

func TestOwnerMemorySourcesListsTheOwnersOwnWordsNewestFirst(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	register := func(id, kind, speaker string, at time.Time) {
		t.Helper()
		if _, err := s.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: kind, ObjectID: id, Version: "1", Speaker: speaker, Body: id, RecordedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	register("owner:old", "import", "owner", memoryTestNow.Add(-48*time.Hour))
	register("owner:early", "import", "owner", memoryTestNow)
	register("owner:late", "import", "owner", memoryTestNow.Add(2*time.Hour))
	register("owner:gone", "import", "owner", memoryTestNow.Add(3*time.Hour))
	register("assistant:a", "import", "assistant", memoryTestNow.Add(time.Hour))
	register("request:r", core.MemoryRequestSource, "owner", memoryTestNow.Add(time.Hour))
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{"owner:gone"}, RequestSourceID: "request:r", Reason: "owner asked", Now: memoryTestNow.Add(4 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	sources, err := s.OwnerMemorySources(ctx, memoryTestNow.Add(-time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	if !slices.Equal(ids, []string{"owner:late", "owner:early"}) {
		t.Fatalf("owner sources = %v", ids)
	}
	if sources, err := s.OwnerMemorySources(ctx, memoryTestNow.Add(-time.Hour), 1); err != nil || len(sources) != 1 || sources[0].ID != "owner:late" {
		t.Fatalf("limited owner sources = %+v, %v", sources, err)
	}
}

func TestV35UpgradeQueuesTheFirstPortraitWhenTheOwnerHasSpoken(t *testing.T) {
	for name, speaker := range map[string]string{"owner spoke": "owner", "nothing from the owner": "assistant"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			raw := openRaw(t, dir)
			if _, err := raw.Exec(readLayout(t, 34) + `PRAGMA user_version = 34;
				INSERT INTO memory_sources(id, kind, object_id, version, speaker, body, recorded_at)
				VALUES('s1', 'import', 's1', '1', '` + speaker + `', 'hello', '2026-11-01T02:00:00.000000000Z');`); err != nil {
				t.Fatalf("build v34: %v", err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			s, err := Open(dir)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			jobs, err := s.ReadyMemoryJobs(context.Background(), time.Now().Add(time.Minute), core.PortraitOperation, 10)
			if err != nil {
				t.Fatal(err)
			}
			if queued := len(jobs) == 1 && jobs[0].Version == "bootstrap"; queued != (speaker == "owner") {
				t.Fatalf("portrait jobs after upgrade = %+v", jobs)
			}
		})
	}
}

func TestV37UpgradeDropsTheReferencesARunDidNotRead(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	// run_a in thread s1 read its history and one recalled source; the
	// portrait's source reached its snapshot references without being read.
	if _, err := raw.Exec(readLayout(t, 36) + `PRAGMA user_version = 36;
		INSERT INTO runs(run_id, session_id, status, input_text, created_at) VALUES('run_a', 's1', 'succeeded', 'hi', '2026-11-05T02:00:00.000000000Z');
		INSERT INTO memory_sources(id, kind, object_id, version, speaker, session_id, run_id, recorded_at) VALUES
		 ('history', 'fixture', 'history', '1', 'owner', 's1', '', '2026-11-05T01:00:00.000000000Z'),
		 ('recalled', 'fixture', 'recalled', '1', 'owner', 's2', '', '2026-11-05T01:00:00.000000000Z'),
		 ('portrayed', 'fixture', 'portrayed', '1', 'owner', 's3', '', '2026-11-05T01:00:00.000000000Z'),
		 ('input', 'fixture', 'input', '1', 'system', 's1', 'run_a', '2026-11-05T02:00:00.000000000Z'),
		 ('reply', 'fixture', 'reply', '1', 'assistant', 's1', 'run_a', '2026-11-05T02:01:00.000000000Z'),
		 ('result', 'fixture', 'result', '1', 'tool', 's1', 'run_a', '2026-11-05T02:01:00.000000000Z');
		INSERT INTO context_snapshots(hash, content, created_at) VALUES('h1', '<memory_context>{"owner_portrait":["在做 Acorn"],"recall":{"unprocessed_sources":[{"id":"recalled"}]}}</memory_context>', '2026-11-05T02:00:00.000000000Z');
		INSERT INTO context_snapshot_refs(hash, run_id, source_id, epoch) VALUES('h1', 'run_a', 'history', 0), ('h1', 'run_a', 'recalled', 0), ('h1', 'run_a', 'portrayed', 0);
		INSERT INTO memory_source_links(source_id, parent_id) VALUES
		 ('reply', 'history'), ('reply', 'recalled'), ('reply', 'portrayed'), ('result', 'portrayed'), ('input', 'portrayed');`); err != nil {
		t.Fatalf("build v36: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	strs := func(query string) []string {
		t.Helper()
		rows, err := s.db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		got, err := memoryStringRows(rows)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if refs := strs(`SELECT source_id FROM context_snapshot_refs ORDER BY source_id`); !slices.Equal(refs, []string{"history", "recalled"}) {
		t.Fatalf("snapshot references after upgrade = %v", refs)
	}
	// The run's input keeps the provenance its scheduler linked.
	if links := strs(`SELECT source_id||'<'||parent_id FROM memory_source_links ORDER BY 1`); !slices.Equal(links, []string{"input<portrayed", "reply<history", "reply<recalled"}) {
		t.Fatalf("source links after upgrade = %v", links)
	}
}
