package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	agentruntime "github.com/ycvk/acorn/internal/runtime"
)

// TestMemoryVectorPerformance measures scaled candidate scans and exact reranking against the real
// SQLite adapter. Run with ACORN_MEMORY_BENCH_REPORT set and TMPDIR on disk.
func TestMemoryVectorPerformance(t *testing.T) {
	reportPath := os.Getenv("ACORN_MEMORY_BENCH_REPORT")
	if reportPath == "" {
		t.Skip("set ACORN_MEMORY_BENCH_REPORT for 10k/100k vector benchmark")
	}
	const dimensions = 1024
	counter, err := agentruntime.NewTokenCounter()
	if err != nil {
		t.Fatal(err)
	}
	type sample struct {
		Records, Dimensions                                                      int
		ColdMS, P50MS, P95MS                                                     float64
		RSSBefore, PeakRSS, ExtraRSS                                             uint64
		VerifiedRows                                                             int
		StandardP95MS, KeywordP95MS, GraphP95MS, VectorReadP95MS, ExactReadP95MS float64
		CancelMS, SQLiteWaitMS                                                   float64
		MixedP95MS, MixedSQLiteWaitMS                                            float64
		IngestedDuringReads                                                      int64
	}
	var samples []sample
	for _, count := range []int{10000, 100000} {
		dir := t.TempDir()
		db, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		corpus := seedCapacityCorpus(t, db, count, dimensions)
		t.Logf("seeded records=%d database=%s", count, dir)
		query := corpus.vectors[0]
		if _, err := db.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			t.Fatal(err)
		}
		scan := func() (int, float64) {
			start := time.Now()
			cursor := ""
			n := 0
			best := float64(-2)
			for {
				batch, err := db.MemoryVectorSketches(ctx, 1, cursor, 512, time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				if len(batch) == 0 {
					break
				}
				for _, v := range batch {
					dot := float64(0)
					for i, x := range v.Codes {
						dot += float64(int8(x)) * float64(query[i])
					}
					dot *= v.Scale
					if dot > best {
						best = dot
					}
					n++
				}
				cursor = batch[len(batch)-1].ID
			}
			if n != count || best <= -2 {
				t.Fatalf("scan dropped vectors n=%d best=%f", n, best)
			}
			return n, float64(time.Since(start).Microseconds()) / 1000
		}
		_, cold := scan()
		t.Logf("cold scan records=%d milliseconds=%.1f", count, cold)
		runtime.GC()
		before := readResidentBytes(t)
		var peak atomic.Uint64
		peak.Store(before)
		done := make(chan struct{})
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					rss := residentBytes()
					for old := peak.Load(); rss > old; old = peak.Load() {
						if peak.CompareAndSwap(old, rss) {
							break
						}
					}
				}
			}
		}()
		timings := make([]float64, 20)
		rows := 0
		for i := range timings {
			rows, timings[i] = scan()
		}
		sort.Float64s(timings)
		t.Logf("warm scans records=%d p95_ms=%.1f", count, timings[18])
		value := sample{Records: count, Dimensions: dimensions, ColdMS: cold, P50MS: timings[9], P95MS: timings[18], RSSBefore: before, VerifiedRows: rows}
		index, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "capacity-fixture", Dimensions: dimensions}, false)
		if err != nil {
			t.Fatal(err)
		}
		measured := &timedMemoryStore{MemoryStore: db}
		engine, err := memory.New(memory.Config{Store: measured, Model: capacityModel{}, ModelName: "unused", Embedder: corpus, Index: index, Count: counter.CountText, Clock: time.Now, Location: time.UTC, BatchTokens: 8192, ContextTokens: 8192, HistoryTokens: 8192})
		if err != nil {
			t.Fatal(err)
		}
		var standard, keyword, graph, vectorRead, exactRead []float64
		waitBefore := db.db.Stats().WaitDuration
		for iteration := range 20 {
			measured.keyword, measured.graph, measured.vector, measured.exact = 0, 0, 0, 0
			start := time.Now()
			result, err := engine.Recall(ctx, core.MemoryQuery{Query: corpus.texts[iteration], Limit: 5})
			if err != nil || len(result.Hits) != 5 {
				t.Fatalf("full standard recall: hits=%d err=%v", len(result.Hits), err)
			}
			standard = append(standard, milliseconds(time.Since(start)))
			keyword = append(keyword, milliseconds(measured.keyword))
			graph = append(graph, milliseconds(measured.graph))
			vectorRead = append(vectorRead, milliseconds(measured.vector))
			exactRead = append(exactRead, milliseconds(measured.exact))
		}
		value.StandardP95MS, value.KeywordP95MS, value.GraphP95MS, value.VectorReadP95MS = percentile95(standard), percentile95(keyword), percentile95(graph), percentile95(vectorRead)
		value.ExactReadP95MS = percentile95(exactRead)
		value.SQLiteWaitMS = milliseconds(db.db.Stats().WaitDuration - waitBefore)
		worker, err := memory.New(memory.Config{Store: db, Model: capacityWorkerModel{}, ModelName: "local-capacity-fixture", Embedder: corpus, Index: index, Count: counter.CountText, Clock: time.Now, Location: time.UTC, BatchTokens: 8192, ContextTokens: 8192, HistoryTokens: 8192})
		if err != nil {
			t.Fatal(err)
		}
		workerCtx, stopWorker := context.WithCancel(ctx)
		workerResult := make(chan error, 1)
		var ingested atomic.Int64
		go func() { workerResult <- runCapacityIngestion(workerCtx, db, worker, func() { ingested.Add(1) }) }()
		var mixed []float64
		waitBefore = db.db.Stats().WaitDuration
		for iteration := range 20 {
			start := time.Now()
			result, err := engine.Recall(ctx, core.MemoryQuery{Query: corpus.texts[iteration], Limit: 5})
			if err != nil || len(result.Hits) != 5 {
				stopWorker()
				t.Fatalf("mixed recall: hits=%d err=%v", len(result.Hits), err)
			}
			mixed = append(mixed, milliseconds(time.Since(start)))
		}
		stopWorker()
		if err := <-workerResult; err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		value.MixedP95MS, value.MixedSQLiteWaitMS = percentile95(mixed), milliseconds(db.db.Stats().WaitDuration-waitBefore)
		value.IngestedDuringReads = ingested.Load()
		if value.IngestedDuringReads == 0 {
			t.Fatal("no ingestion completed during concurrent reads")
		}
		cancelCtx, cancel := context.WithCancel(ctx)
		var cancelStart time.Time
		measured.pages = 0
		measured.cancelAfterSecondPage = func() { cancelStart = time.Now(); cancel() }
		if _, err := engine.Recall(cancelCtx, core.MemoryQuery{Query: corpus.texts[0]}); !errors.Is(err, context.Canceled) {
			cancel()
			t.Fatalf("cancelled recall: %v", err)
		}
		cancel()
		if cancelStart.IsZero() {
			t.Fatal("recall did not reach a cancellable scan page")
		}
		value.CancelMS = milliseconds(time.Since(cancelStart))
		close(done)
		<-finished
		value.PeakRSS = peak.Load()
		if value.PeakRSS > before {
			value.ExtraRSS = value.PeakRSS - before
		}
		samples = append(samples, value)
		t.Logf("mixed records=%d p95_ms=%.1f sqlite_wait_ms=%.1f ingested=%d", count, value.MixedP95MS, value.MixedSQLiteWaitMS, value.IngestedDuringReads)
		t.Logf("vectors=%d cold_ms=%.1f p50_ms=%.1f p95_ms=%.1f extra_rss_mib=%.2f", count, cold, value.P50MS, value.P95MS, float64(value.ExtraRSS)/(1<<20))
		t.Logf("standard records=%d p95_ms=%.1f keyword_p95_ms=%.1f graph_p95_ms=%.1f candidate_read_p95_ms=%.1f exact_read_p95_ms=%.1f sqlite_wait_ms=%.1f cancel_ms=%.3f", count, value.StandardP95MS, value.KeywordP95MS, value.GraphP95MS, value.VectorReadP95MS, value.ExactReadP95MS, value.SQLiteWaitMS, value.CancelMS)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.MarshalIndent(map[string]any{"samples": samples, "go": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH, "cpus": runtime.NumCPU(), "scan_batch": 512, "fixture": "32 topic clusters; distinct 1024d vectors and text; source evidence; entity and parent graphs; one concurrent extraction/embedding/consolidation worker"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, s := range samples {
		if s.P95MS >= 2000 || s.StandardP95MS >= 2000 || s.MixedP95MS >= 2000 || s.ExtraRSS > 64<<20 {
			t.Errorf("capacity gate failed for %d vectors: scan %.1fms standard %.1fms mixed %.1fms extra RSS %.1fMiB", s.Records, s.P95MS, s.StandardP95MS, s.MixedP95MS, float64(s.ExtraRSS)/(1<<20))
		}
	}
}

// Provider time is excluded with a fixed query vector. All local recall stages,
// usage persistence, fusion and the production token counter run normally.
type capacityEmbedding []float64

func (e capacityEmbedding) EmbedStrings(context.Context, []string, ...embedding.Option) ([][]float64, error) {
	return [][]float64{e}, nil
}

type capacityModel struct{}

func (capacityModel) GenerateMemory(context.Context, string, string, int) (memory.Generation, error) {
	return memory.Generation{}, errors.New("standard recall must not call the main model")
}

type timedMemoryStore struct {
	core.MemoryStore
	keyword, graph, vector, exact time.Duration
	pages                         int
	cancelAfterSecondPage         func()
}

func (s *timedMemoryStore) SearchMemoryText(ctx context.Context, q core.MemoryQuery) ([]core.MemoryRecord, error) {
	start := time.Now()
	out, err := s.MemoryStore.SearchMemoryText(ctx, q)
	s.keyword += time.Since(start)
	return out, err
}

func (s *timedMemoryStore) MemoryNeighbors(ctx context.Context, ids []string, limit int) ([]core.MemoryRecord, error) {
	start := time.Now()
	out, err := s.MemoryStore.MemoryNeighbors(ctx, ids, limit)
	s.graph += time.Since(start)
	return out, err
}

func (s *timedMemoryStore) MemoryVectorSketches(ctx context.Context, generation int64, after string, limit int, knownAt time.Time) ([]core.MemoryVectorSketch, error) {
	start := time.Now()
	out, err := s.MemoryStore.MemoryVectorSketches(ctx, generation, after, limit, knownAt)
	s.vector += time.Since(start)
	if s.cancelAfterSecondPage != nil {
		s.pages++
		if s.pages == 2 {
			s.cancelAfterSecondPage()
			s.cancelAfterSecondPage = nil
		}
	}
	return out, err
}

func (s *timedMemoryStore) MemoryVectorsByIDs(ctx context.Context, generation int64, ids []string, knownAt time.Time) ([]core.MemoryVector, error) {
	start := time.Now()
	out, err := s.MemoryStore.MemoryVectorsByIDs(ctx, generation, ids, knownAt)
	s.exact += time.Since(start)
	return out, err
}

func milliseconds(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func percentile95(values []float64) float64 {
	sort.Float64s(values)
	return values[18]
}
func readResidentBytes(t *testing.T) uint64 {
	t.Helper()
	n := residentBytes()
	if n == 0 {
		t.Fatal("benchmark requires Linux /proc/self/statm")
	}
	return n
}
func residentBytes() uint64 {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}
