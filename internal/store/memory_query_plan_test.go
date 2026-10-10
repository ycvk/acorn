package store

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestMemoryVectorPageUsesPrimaryKeyCursor(t *testing.T) {
	s := openTestStore(t)
	var plans []string
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+memoryVectorPageSQL, 1, "", 512)
	if err != nil {
		t.Fatal(err)
	}
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id, parent, unused int
		var detail string
		if err := scan(&id, &parent, &unused, &detail); err != nil {
			return err
		}
		plans = append(plans, detail)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(plans) == 0 || !strings.Contains(plans[0], "SEARCH e") || strings.Contains(strings.Join(plans, "\n"), "TEMP B-TREE") {
		t.Fatalf("vector page must seek its cursor before record validation: %v", plans)
	}
	if !strings.Contains(strings.Join(plans, "\n"), "SEARCH r USING COVERING INDEX") {
		t.Fatalf("vector validation must not read every record body: %v", plans)
	}
}

func TestMemoryAliasLookupStartsWithAliasDeclarations(t *testing.T) {
	s := openTestStore(t)
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+memoryAliasQuerySQL+" AND a.revision=r.revision", "摄影群的小岑")
	if err != nil {
		t.Fatal(err)
	}
	var plans []string
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id, parent, unused int
		var detail string
		if err := scan(&id, &parent, &unused, &detail); err != nil {
			return err
		}
		plans = append(plans, detail)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(plans) == 0 || !strings.Contains(plans[0], "SCAN a") {
		t.Fatalf("alias resolution must examine declarations before loading record histories: %v", plans)
	}
}

func TestMemoryEntityNeighborsUseEntityIndex(t *testing.T) {
	s := openTestStore(t)
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT b.record_id FROM memory_mentions a JOIN memory_mentions b ON a.entity_id=b.entity_id WHERE a.record_id=?`, "record")
	if err != nil {
		t.Fatal(err)
	}
	var plans []string
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id, parent, unused int
		var detail string
		if err := scan(&id, &parent, &unused, &detail); err != nil {
			return err
		}
		plans = append(plans, detail)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plans, "\n"), "SEARCH b USING COVERING INDEX idx_memory_mentions_entity") {
		t.Fatalf("entity expansion must seek the shared entity: %v", plans)
	}
}

func TestMemoryRecordLinksStartFromLinkIndexes(t *testing.T) {
	s := openTestStore(t)
	rows, err := s.db.Query("EXPLAIN QUERY PLAN "+memoryRecordLinksSQL, "record", "record")
	if err != nil {
		t.Fatal(err)
	}
	var plans []string
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id, parent, unused int
		var detail string
		if err := scan(&id, &parent, &unused, &detail); err != nil {
			return err
		}
		plans = append(plans, detail)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(plans, "\n")
	if strings.Contains(plan, "idx_memory_records_current") || !strings.Contains(plan, "SEARCH memory_links USING COVERING INDEX idx_memory_links_parent") || !strings.Contains(plan, "SEARCH a USING") {
		t.Fatalf("record links must be read from link indexes before validating endpoints: %v", plans)
	}
}

func TestMemoryJobStatusReadsIndexRanges(t *testing.T) {
	s := openTestStore(t)
	for _, query := range []string{memoryJobStatusSQL, "SELECT COUNT(*) FROM memory_jobs j JOIN memory_sources s ON s.id=j.object_id WHERE " + memoryPendingSourcesWhere} {
		rows, err := s.db.Query("EXPLAIN QUERY PLAN " + query)
		if err != nil {
			t.Fatal(err)
		}
		var plans []string
		if err := scanRows(rows, func(scan func(...any) error) error {
			var id, parent, unused int
			var detail string
			if err := scan(&id, &parent, &unused, &detail); err != nil {
				return err
			}
			plans = append(plans, detail)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		for _, plan := range plans {
			if strings.Contains(plan, "memory_jobs") && !strings.Contains(plan, "idx_memory_jobs_ready (state=?)") && !strings.Contains(plan, "idx_memory_jobs_completed") {
				t.Fatalf("job status must not scan completed jobs: %v", plans)
			}
		}
	}
}

func TestMemoryKeywordSearchLeavesFrequentTermsToSemanticRoute(t *testing.T) {
	s := openTestStore(t)
	seedCapacityCorpus(t, s, memoryKeywordTermMatches+100, 8)
	ctx := context.Background()
	frequent, err := s.SearchMemoryText(ctx, core.MemoryQuery{Query: "项目经历", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(frequent) != 0 {
		t.Fatalf("a term in every record must not drive keyword ranking: %d hits", len(frequent))
	}
	mixed, err := s.SearchMemoryText(ctx, core.MemoryQuery{Query: "项目经历 00000003", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(mixed) == 0 || mixed[0].ID != "bench_00000003" {
		t.Fatalf("the selective term must rank its record first: %+v", mixed)
	}
}

func TestMemoryKeywordSearchMatchesEveryShortTermBesideLongTerms(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, text := range []string{"小王 下周去杭州", "我在学 Go", "周末整理阳台花园", "无关的记录"} {
		source := seedMemorySource(t, s, "owner:"+text, "owner", text)
		if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, text)}, Now: memoryTestNow}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SearchMemoryText(ctx, core.MemoryQuery{Query: "小王 Go 阳台花园", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var contents []string
	for _, r := range got {
		contents = append(contents, r.Content)
	}
	if len(got) != 3 || slices.Contains(contents, "无关的记录") {
		t.Fatalf("short and long terms must each find their record: %v", contents)
	}
}
