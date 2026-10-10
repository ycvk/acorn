package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
)

func TestMemoryCandidateIndexPreservesExactTopFive(t *testing.T) {
	const count, dimensions = 5000, 64
	ctx := context.Background()
	db := openTestStore(t)
	index, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "capacity-fixture", Dimensions: dimensions}, false)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(71, 29))
	vectors := make([][]float32, count)
	ids := make([]string, count)
	for i := range count {
		ids[i] = fmt.Sprintf("rank_%05d", i)
		vector := make([]float32, dimensions)
		for j := range vector {
			vector[j] = float32(rng.NormFloat64())
		}
		data, err := encodeMemoryVector(vector)
		if err != nil {
			t.Fatal(err)
		}
		codes, scale := quantizeMemoryVector(data)
		for j := range vector {
			vector[j] = math.Float32frombits(binary.LittleEndian.Uint32(data[j*4:]))
			if math.Abs(float64(vector[j])-float64(int8(codes[j]))*scale) > scale/2+1e-8 {
				t.Fatal("candidate quantization exceeded its rounding bound")
			}
		}
		vectors[i] = vector
		if _, err := tx.Exec(`INSERT INTO memory_records(id,kind,content,scope,state,basis,revision,recorded_at,updated_at) VALUES(?,'fact',?,'','current','direct',1,?,?)`, ids[i], ids[i], formatTimestamp(memoryTestNow), formatTimestamp(memoryTestNow)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO memory_revisions(record_id,revision,kind,content,scope,state,basis,at,reason) VALUES(?,1,'fact',?,'','current','direct',?,'fixture')`, ids[i], ids[i], formatTimestamp(memoryTestNow)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO memory_embeddings(record_id,revision,generation,dimensions,vector) VALUES(?,1,1,?,?)`, ids[i], dimensions, data); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO memory_vector_sketches(generation,record_id,revision,dimensions,scale,codes) VALUES(1,?,1,?,?,?)`, ids[i], dimensions, scale, codes); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		query := make([]float64, dimensions)
		var norm float64
		for i := range query {
			query[i] = rng.NormFloat64()
			norm += query[i] * query[i]
		}
		for i := range query {
			query[i] /= math.Sqrt(norm)
		}
		type ranked struct {
			id    string
			score float64
		}
		exact := make([]ranked, count)
		for i, v := range vectors {
			exact[i].id = ids[i]
			for j, x := range v {
				exact[i].score += float64(x) * query[j]
			}
		}
		slices.SortFunc(exact, func(a, b ranked) int {
			if a.score > b.score {
				return -1
			}
			if a.score < b.score {
				return 1
			}
			return 0
		})
		e, err := memory.New(memory.Config{Store: db, Model: capacityModel{}, ModelName: "unused", Embedder: capacityEmbedding(query), Index: index, Count: func(_ context.Context, s string) (int, error) { return len(s) / 2, nil }, Clock: time.Now, Location: time.UTC, BatchTokens: 8192, ContextTokens: 8192, HistoryTokens: 8192})
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.Recall(ctx, core.MemoryQuery{Query: "目标", Limit: 5})
		if err != nil || len(got.Hits) != 5 {
			t.Fatalf("recall=%+v err=%v", got, err)
		}
		for i, h := range got.Hits {
			if h.Record.ID != exact[i].id {
				t.Fatalf("rank %d got %s want exact %s", i, h.Record.ID, exact[i].id)
			}
		}
	}
}
