package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

type protocolModel struct {
	calls       int
	beforeReply func()
	seen        []string
}

func (m *protocolModel) GenerateMemory(_ context.Context, instruction, input string, _ int) (Generation, error) {
	m.calls++
	m.seen = append(m.seen, input)
	if m.beforeReply != nil {
		fn := m.beforeReply
		m.beforeReply = nil
		fn()
	}
	if strings.HasPrefix(instruction, "Extract") {
		var in struct {
			Source core.MemorySource `json:"source"`
		}
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return Generation{}, err
		}
		reply, err := json.Marshal(map[string]any{"records": []core.MemoryDraft{{Kind: "fact", Content: in.Source.Content, Entities: []string{"饮食"}, Evidence: []core.MemoryEvidence{{SourceID: in.Source.ID, Quote: in.Source.Content, Relation: "supports"}}}}})
		return Generation{Text: string(reply), InputTokens: 100, OutputTokens: 30, Reported: true}, err
	}
	return Generation{Text: `{"changes":[],"links":[]}`, InputTokens: 100, OutputTokens: 3, Reported: true}, nil
}

func testEngine(t *testing.T, model *protocolModel, limit int) (*Engine, *store.Store) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing embedding auth")
		}
		var input struct {
			Input      []string `json:"input"`
			Type       string   `json:"input_type"`
			Truncation bool     `json:"truncation"`
			Dimensions int      `json:"output_dimension"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.Truncation || input.Dimensions != 2 || (input.Type != "document" && input.Type != "query") {
			t.Errorf("embedding contract=%+v", input)
		}
		data := make([]map[string]any, 0, len(input.Input))
		for i := range input.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float64{1, 0}})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "data": data, "usage": map[string]any{"total_tokens": 12}}); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	embedder, err := NewVoyage(VoyageConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture", Dimensions: 2, Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	index, err := db.ConfigureMemoryIndex(context.Background(), core.MemoryIndex{Model: "fixture", Dimensions: 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{Store: db, Model: model, ModelName: "fixture", Embedder: embedder, Index: index, Count: func(_ context.Context, s string) (int, error) { return utf8.RuneCountInString(s)/2 + 1, nil }, Clock: time.Now, Location: time.UTC, DailyTokens: limit, BatchTokens: 8192, ContextTokens: 8192, HistoryTokens: 8192})
	if err != nil {
		t.Fatal(err)
	}
	return engine, db
}

func drainMemory(t *testing.T, e *Engine) {
	t.Helper()
	for range 20 {
		worked, err := e.ProcessOne(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			return
		}
	}
	t.Fatal("memory queue did not settle")
}

func TestWorkerProcessesMessageThenRecallsSourcedFact(t *testing.T) {
	ctx := context.Background()
	model := &protocolModel{}
	engine, db := testEngine(t, model, 100000)
	if _, err := db.CreateSession(ctx, "food", "饮食"); err != nil {
		t.Fatal(err)
	}
	msg, err := db.AppendSessionMessage(ctx, "food", 1, "user", "我不吃香菜", "")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := engine.Recall(ctx, core.MemoryQuery{Query: "香菜", SessionID: "food"})
	if err != nil || pending.Pending != 1 || len(pending.Sources) != 1 {
		t.Fatalf("pending=%+v %v", pending, err)
	}
	drainMemory(t, engine)
	got, err := engine.Recall(ctx, core.MemoryQuery{Query: "饮食禁忌"})
	if err != nil || len(got.Hits) != 1 || got.Pending != 0 {
		t.Fatalf("recall=%+v %v", got, err)
	}
	sourceID := fmt.Sprintf("message:%d", msg.ID)
	if got.Hits[0].Record.Evidence[0].SourceID != sourceID || got.Hits[0].Record.Basis != "direct" {
		t.Fatalf("evidence=%+v", got.Hits[0])
	}
	read, err := db.ReadMemory(ctx, got.Hits[0].Record.ID)
	if err != nil || len(read.Sources) != 1 || read.Sources[0].Content != msg.Content {
		t.Fatalf("read=%+v %v", read, err)
	}
	usage, err := db.MemoryUsageSince(ctx, time.Now().Add(-time.Hour), "memory")
	if err != nil || usage != 166 {
		t.Fatalf("usage=%d %v", usage, err)
	}
}

func TestWorkerBudgetDefersWithoutCallingModel(t *testing.T) {
	ctx := context.Background()
	model := &protocolModel{}
	engine, db := testEngine(t, model, 1)
	_, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:1", Kind: "import", ObjectID: "1", Version: "1", Speaker: "owner", Body: "我不吃香菜", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	worked, err := engine.ProcessOne(ctx)
	if !worked || !errors.Is(err, core.ErrMemoryBudget) || model.calls != 0 {
		t.Fatalf("worked=%v err=%v calls=%d", worked, err, model.calls)
	}
	status, err := db.MemoryProcessingStatus(ctx)
	if err != nil || status.Pending != 1 || status.Failed != 0 {
		t.Fatalf("status=%+v %v", status, err)
	}
	if worked, err := engine.ProcessOne(ctx); worked || err != nil {
		t.Fatalf("budget task immediately repeated: %v %v", worked, err)
	}
}

func TestWorkerCannotCommitAcrossOwnerForget(t *testing.T) {
	ctx := context.Background()
	model := &protocolModel{}
	engine, db := testEngine(t, model, 100000)
	source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:1", Kind: "import", ObjectID: "1", Version: "1", Speaker: "owner", Body: "我不吃香菜", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:forget", Kind: "import", ObjectID: "forget", Version: "1", Speaker: "owner", Body: "忘记香菜偏好", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	model.beforeReply = func() {
		if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{source.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engine.ProcessOne(ctx); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("in-flight extraction: %v", err)
	}
	records, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history"})
	if err != nil || len(records) != 0 {
		t.Fatalf("forgotten record appeared: %+v %v", records, err)
	}
}
