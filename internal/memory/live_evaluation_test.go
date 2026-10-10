package memory_test

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

//go:embed testdata/personal_zh.json
var personalChineseEvaluation []byte

type evalSource struct {
	ID, Text, At, Corrects string
	ValidFrom              string `json:"valid_from"`
}
type evalQuery struct {
	ID, Category, Query, Mode, Depth string
	AsOf                             string `json:"as_of"`
	KnownAt                          string `json:"known_at"`
	Expected, Forbidden              []string
}
type evalResult struct {
	ID, Category                        string
	Expected, Found, Missing, Forbidden []string
	CitationErrors                      []string
	Milliseconds                        int64
}

type evalProviderCall struct {
	Stage        string `json:"stage"`
	Kind         string `json:"kind"`
	Milliseconds int64  `json:"milliseconds"`
	Succeeded    bool   `json:"succeeded"`
}

type measuredEvaluationModel struct {
	inner memory.Model
	stage *string
	calls *[]evalProviderCall
}

func (m measuredEvaluationModel) GenerateMemory(ctx context.Context, instruction, input string, maxOutput int) (memory.Generation, error) {
	start := time.Now()
	result, err := m.inner.GenerateMemory(ctx, instruction, input, maxOutput)
	*m.calls = append(*m.calls, evalProviderCall{Stage: *m.stage, Kind: "main_model", Milliseconds: time.Since(start).Milliseconds(), Succeeded: err == nil})
	return result, err
}

type measuredEvaluationEmbedding struct {
	inner embedding.Embedder
	stage *string
	calls *[]evalProviderCall
}

func (m measuredEvaluationEmbedding) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	start := time.Now()
	result, err := m.inner.EmbedStrings(ctx, texts, opts...)
	*m.calls = append(*m.calls, evalProviderCall{Stage: *m.stage, Kind: "embedding", Milliseconds: time.Since(start).Milliseconds(), Succeeded: err == nil})
	return result, err
}

// This test uses synthetic owner statements and real configured providers.
// It is opt-in, never opens the configured production database, and writes a
// report only to ACORN_MEMORY_EVAL_REPORT when explicitly supplied.
func TestMemoryLiveEvaluation(t *testing.T) {
	path := os.Getenv("ACORN_MEMORY_EVAL_CONFIG")
	if path == "" {
		t.Skip("set ACORN_MEMORY_EVAL_CONFIG and ACORN_MEMORY_EVAL_VOYAGE_KEY_FILE for real-provider evaluation")
	}
	if envPath := os.Getenv("ACORN_MEMORY_EVAL_ENV"); envPath != "" {
		raw, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if key, value, ok := strings.Cut(line, "="); ok && key == "ACORN_MODEL_API_KEY" {
				t.Setenv(key, strings.Trim(value, "\"'"))
			}
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(os.Getenv("ACORN_MEMORY_EVAL_VOYAGE_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Memory.Embedding.APIKey = strings.TrimSpace(string(key))
	cfg.Memory.DailyTokens = 0
	cfg.Wake.DailyTokens = 0
	if err := cfg.ValidateExecutionReady(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	counter, err := runtime.NewTokenCounter()
	if err != nil {
		t.Fatal(err)
	}
	model, err := runtime.NewMemoryModel(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	embed, err := memory.NewVoyage(memory.VoyageConfig{BaseURL: cfg.Memory.Embedding.BaseURL, APIKey: cfg.Memory.Embedding.APIKey, Model: cfg.Memory.Embedding.Model, Dimensions: cfg.Memory.Embedding.Dimensions, Client: &http.Client{Timeout: 60 * time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	index, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: cfg.Memory.Embedding.Model, Dimensions: cfg.Memory.Embedding.Dimensions}, false)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := cfg.EnabledProvider()
	if err != nil {
		t.Fatal(err)
	}
	stage := "ingestion"
	var providerCalls []evalProviderCall
	engine, err := memory.New(memory.Config{Store: db, Model: measuredEvaluationModel{inner: model, stage: &stage, calls: &providerCalls}, ModelName: provider.Model, Embedder: measuredEvaluationEmbedding{inner: embed, stage: &stage, calls: &providerCalls}, Index: index, Count: counter.CountText, Clock: func() time.Time { return clock }, Location: time.UTC, BatchTokens: cfg.Memory.BatchTokens, ContextTokens: cfg.Memory.ContextTokens, HistoryTokens: cfg.Memory.HistoryTokens})
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version string
		Sources []evalSource
		Queries []evalQuery
	}
	if err := json.Unmarshal(personalChineseEvaluation, &fixture); err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(fixture.Sources, func(i, j int) bool { return fixture.Sources[i].At < fixture.Sources[j].At })
	started := time.Now()
	retryErrors := 0

	drain := func() {
		t.Helper()
		for i := 0; i < 500; i++ {
			worked, processErr := engine.ProcessOne(ctx)
			status, err := db.MemoryProcessingStatus(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if processErr != nil {
				retryErrors++
				t.Logf("processing retry: %v", processErr)
			}
			if status.Failed > 0 {
				t.Fatalf("processing exhausted retries: %s", status.LastError)
			}
			if !worked && status.Pending == 0 {
				return
			}
			// Fixture time follows the durable retry schedule; source dates stay fixed.
			if processErr != nil || !worked {
				clock = clock.Add(time.Minute)
			}
		}
		t.Fatal("processing did not settle")
	}

	for _, s := range fixture.Sources {
		clock, err = time.Parse(time.RFC3339, s.At)
		if err != nil {
			t.Fatal(err)
		}
		source := core.MemorySource{ID: s.ID, Kind: "standalone", ObjectID: s.ID, Version: "1", Speaker: "owner", SessionID: "thread_" + s.ID, RunID: "run_" + s.ID, Body: s.Text, RecordedAt: clock}
		if _, err := db.RegisterMemorySource(ctx, source); err != nil {
			t.Fatal(err)
		}
		if s.Corrects != "" {
			old, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Kind: "fact", SessionID: "thread_" + s.Corrects, Mode: "current", Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(old) == 0 {
				t.Fatalf("correction target %s was not extracted", s.Corrects)
			}
			for _, r := range old {
				if _, err := engine.Correct(ctx, source.RunID, memory.CorrectInput{ID: r.ID, Revision: r.Revision, Content: "我从九月一日起居住在苏州", SourceID: s.ID, Quote: s.Text, ValidFrom: s.ValidFrom}); err != nil {
					t.Fatal(err)
				}
			}
		}
		drain()
		t.Logf("ingested %s elapsed=%s", s.ID, time.Since(started).Round(time.Second))
	}
	clock = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	// An assistant suggestion has no authority to establish an owner preference.
	const suggestionID = "assistant_suggestion_guard"
	if _, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: suggestionID, Kind: "standalone", ObjectID: suggestionID, Version: "1", Speaker: "assistant", Body: "我猜 owner 喜欢很辣的食物，这只是未经 owner 确认的建议。", RecordedAt: clock}); err != nil {
		t.Fatal(err)
	}
	drain()
	records, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	inferenceErrors := 0
	for _, r := range records {
		if r.Kind == "fact" && slices.Contains(r.SourceIDs, suggestionID) {
			inferenceErrors++
		}
	}
	var results []evalResult
	found, wanted, citations, badCitations, correctionErrors, unrelatedErrors := 0, 0, 0, 0, 0, 0
	for _, q := range fixture.Queries {
		stage = "query:" + q.ID
		query := core.MemoryQuery{Query: q.Query, Mode: q.Mode, Depth: q.Depth, Limit: 5, MaxTokens: 8192}
		if q.AsOf != "" {
			query.AsOf, err = time.Parse(time.RFC3339, q.AsOf)
			if err != nil {
				t.Fatal(err)
			}
		}
		if q.KnownAt != "" {
			query.KnownAt, err = time.Parse(time.RFC3339, q.KnownAt)
			if err != nil {
				t.Fatal(err)
			}
		}
		start := time.Now()
		out, err := engine.Recall(memory.WithRun(ctx, "query_"+q.ID, false), query)
		if err != nil {
			t.Fatal(err)
		}
		result := evalResult{ID: q.ID, Category: q.Category, Expected: q.Expected, Milliseconds: time.Since(start).Milliseconds()}
		for _, hit := range out.Hits {
			result.Found = append(result.Found, hit.Record.SourceIDs...)
			for _, ev := range hit.Record.Evidence {
				citations++
				source, err := db.LoadMemorySource(ctx, ev.SourceID)
				if err != nil || !strings.Contains(source.Content, ev.Quote) {
					badCitations++
					result.CitationErrors = append(result.CitationErrors, ev.SourceID)
				}
			}
			for _, id := range q.Forbidden {
				if hit.Record.Kind == "fact" && slices.Contains(hit.Record.SourceIDs, id) {
					result.Forbidden = append(result.Forbidden, id)
					correctionErrors++
				}
			}
		}
		slices.Sort(result.Found)
		result.Found = slices.Compact(result.Found)
		for _, id := range q.Expected {
			wanted++
			if slices.Contains(result.Found, id) {
				found++
			} else {
				result.Missing = append(result.Missing, id)
			}
		}
		if len(q.Expected) == 0 && len(out.Hits) != 0 {
			unrelatedErrors++
		}
		results = append(results, result)
		t.Logf("query %s category=%s missing=%v forbidden=%v ms=%d", q.ID, q.Category, result.Missing, result.Forbidden, result.Milliseconds)
	}
	stage = "forget"
	// Explicit forgetting must remove the source and every derived record from all modes.
	request := core.MemorySource{ID: "forget_request", Kind: "standalone", ObjectID: "forget_request", Version: "1", Speaker: "owner", RunID: "run_forget", Body: "忘记书房的私密昵称", RecordedAt: clock}
	if _, err := db.RegisterMemorySource(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Forget(ctx, request.RunID, core.MemoryForget{SourceIDs: []string{"sensitive"}, RequestSourceID: request.ID, Reason: request.Body}); err != nil {
		t.Fatal(err)
	}
	forgetErrors := 0
	for _, mode := range []string{"current", "history"} {
		out, err := engine.Recall(ctx, core.MemoryQuery{Query: "书房月光小站的昵称", Mode: mode, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range out.Hits {
			if strings.Contains(h.Record.Content, "月光小站") || slices.Contains(h.Record.SourceIDs, "sensitive") {
				forgetErrors++
			}
		}
	}
	if _, err := db.LoadMemorySource(ctx, "sensitive"); !errors.Is(err, core.ErrMemoryExcluded) {
		forgetErrors++
	}
	drain()
	usage, err := db.MemoryUsageSince(ctx, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(personalChineseEvaluation)
	recall := float64(found) / float64(wanted)
	report := map[string]any{"fixture_version": fixture.Version, "fixture_sha256": hex.EncodeToString(hash[:]), "model": provider.Model, "embedding": index.Model, "dimensions": index.Dimensions, "query_count": len(results), "source_count": len(fixture.Sources), "evidence_recall_at_5": recall, "evidence_found": found, "evidence_expected": wanted, "citations": citations, "citation_errors": badCitations, "correction_errors": correctionErrors, "forget_errors": forgetErrors, "unrelated_errors": unrelatedErrors, "tokens": usage, "processing_retry_errors": retryErrors, "elapsed_seconds": time.Since(started).Seconds(), "queries": results}
	report["assistant_promotion_errors"] = inferenceErrors
	report["guard_source_count"] = 1
	report["records_before_forget"] = records
	report["provider_calls"] = providerCalls
	if dest := os.Getenv("ACORN_MEMORY_EVAL_REPORT"); dest != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("recall=%.3f citations=%d citation_errors=%d correction_errors=%d forget_errors=%d unrelated_errors=%d tokens=%d", recall, citations, badCitations, correctionErrors, forgetErrors, unrelatedErrors, usage)
	if recall < .9 || badCitations != 0 || correctionErrors != 0 || forgetErrors != 0 || unrelatedErrors != 0 || inferenceErrors != 0 {
		t.Fatalf("memory evaluation failed: recall %.3f", recall)
	}
}
