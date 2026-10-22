package memory_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/store"
)

// This test sends synthetic owner messages through extraction with the real
// configured model. A task the owner hands the assistant is done in its
// conversation and leaves no fact; durable facts stated next to it remain.
func TestMemoryLiveExtractionLeavesTasksToTheirConversation(t *testing.T) {
	path := os.Getenv("ACORN_MEMORY_EVAL_CONFIG")
	if path == "" {
		t.Skip("set ACORN_MEMORY_EVAL_CONFIG for real-provider extraction")
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
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	counter, err := runtime.NewTokenCounter()
	if err != nil {
		t.Fatal(err)
	}
	model, err := runtime.NewMemoryModel(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := cfg.EnabledProvider()
	if err != nil {
		t.Fatal(err)
	}
	// Extraction is under test; embeddings only need a fixed shape.
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		data := make([]map[string]any, 0, len(input.Input))
		for i := range input.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float64{1, 0}})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "data": data, "usage": map[string]any{"total_tokens": 1}}); err != nil {
			t.Error(err)
		}
	}))
	defer embeddings.Close()
	embed, err := memory.NewVoyage(memory.VoyageConfig{BaseURL: embeddings.URL, APIKey: "fixture", Model: "fixture", Dimensions: 2, Client: embeddings.Client()})
	if err != nil {
		t.Fatal(err)
	}
	index, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "fixture", Dimensions: 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	engine, err := memory.New(memory.Config{Store: db, Owner: db, Model: model, ModelName: provider.Model, Embedder: embed, Index: index, Count: counter.CountText, Clock: func() time.Time { return clock }, Settings: settings.Fixed(core.Settings{Location: time.UTC})})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		id, text string
		// want must each appear in some extracted fact; forbid in none.
		want, forbid []string
	}{
		{id: "novel_search", text: "小重山by滕栩 搜索一下这本小说", forbid: []string{"小重山"}},
		{id: "novel_find", text: "找一下这本小说：当年不肯嫁东风，明月倾写的", forbid: []string{"东风"}},
		{id: "skill_install", text: "我在工作机 ~/downloads/word-count 放了一个技能包，帮我检查后安装它。", forbid: []string{"word-count"}},
		{id: "price_fetch", text: "把这个页面现在的价格抓下来告诉我：https://shop.example.com/item/42", forbid: []string{"example.com"}},
		{id: "standing_csv", text: "以后我会经常丢给你一个 CSV 链接，让你按某一列统计每组有多少行、从多到少排、写成知识库笔记（你刚在 reading/sp500-sectors.md 做过一次）。把这套做法存成一个技能，下次直接照着做。", want: []string{"CSV"}},
		{id: "weather_trip", text: "帮我查一下明天新加坡的天气，我下周三要飞东京出差。", want: []string{"东京"}, forbid: []string{"天气"}},
		{id: "preference", text: "我不吃香菜，以后推荐餐厅记得避开。", want: []string{"香菜"}},
	}
	for _, c := range cases {
		clock = clock.Add(time.Hour)
		source := core.MemorySource{ID: c.id, Kind: "standalone", ObjectID: c.id, Version: "1", Speaker: "owner", SessionID: "thread_" + c.id, RunID: "run_" + c.id, Body: c.text, RecordedAt: clock}
		if _, err := db.RegisterMemorySource(ctx, source); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; ; i++ {
		if i == 200 {
			t.Fatal("memory processing did not settle")
		}
		worked, processErr := engine.ProcessOne(ctx)
		status, err := db.MemoryProcessingStatus(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if processErr != nil {
			t.Logf("processing retry: %v", processErr)
		}
		if status.Failed > 0 {
			t.Fatalf("processing exhausted retries: %s", status.LastError)
		}
		if !worked && status.Pending == 0 {
			break
		}
		// A failed attempt waits out its retry delay on the fixture clock.
		if processErr != nil || !worked {
			clock = clock.Add(time.Minute)
		}
	}
	for _, c := range cases {
		facts, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Kind: "fact", SessionID: "thread_" + c.id, Mode: "current", Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		contents := make([]string, 0, len(facts))
		for _, f := range facts {
			contents = append(contents, f.Content)
		}
		t.Logf("%s: %q", c.id, contents)
		joined := strings.Join(contents, "\n")
		for _, w := range c.want {
			if !strings.Contains(joined, w) {
				t.Errorf("%s: no fact mentions %q; facts %q", c.id, w, contents)
			}
		}
		for _, f := range c.forbid {
			if strings.Contains(joined, f) {
				t.Errorf("%s: a fact records the task (%q); facts %q", c.id, f, contents)
			}
		}
	}
}
