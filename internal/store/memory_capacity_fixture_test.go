package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
)

type capacityCorpus struct {
	vectors [][]float64
	texts   []string
}

func seedCapacityCorpus(t *testing.T, db *Store, count, dimensions int) capacityCorpus {
	t.Helper()
	rng := rand.New(rand.NewPCG(20261010, 73))
	centers := make([][]float64, 32)
	for i := range centers {
		centers[i] = make([]float64, dimensions)
		for j := range centers[i] {
			centers[i][j] = rng.Float64() - .5
		}
	}
	corpus := capacityCorpus{vectors: centers}
	tx, err := db.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for group := range 32 {
		if _, err := tx.Exec(`INSERT INTO memory_entities(id,name) VALUES(?,?)`, group+1, fmt.Sprintf("topic%02d", group)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO memory_entities(id,name) VALUES(100,'owner')`); err != nil {
		t.Fatal(err)
	}
	var previous []core.MemoryRecord
	for i := range count {
		id := fmt.Sprintf("bench_%08d", i)
		group := i % 32
		content := fmt.Sprintf("topic%02d 项目经历 %08d：在场景 %d 中记录进展，保留条件 %d，等实际使用后再确认结果。%s", group, i, i%19, i%7, strings.Repeat("此次仅验证样例。", i%5))
		at := memoryTestNow.Add(time.Duration(i-count) * time.Minute)
		if i < 32 {
			content += fmt.Sprintf(" topic%02d 也叫 alias%02d。", group, group)
		}
		r := core.MemoryRecord{ID: id, Kind: "fact", Content: content, Scope: fmt.Sprintf("条件%d", i%7), State: "current", Basis: "direct", Revision: 1, RecordedAt: at, UpdatedAt: at, Entities: []string{fmt.Sprintf("topic%02d", group)}, SourceIDs: []string{id}, Evidence: []core.MemoryEvidence{{SourceID: id, Quote: content, Relation: "supports"}}}
		if i%3 == 0 {
			r.Entities = append(r.Entities, "owner")
		}
		if i < 32 {
			r.Aliases = []core.MemoryEntityAlias{{Canonical: fmt.Sprintf("topic%02d", group), Alias: fmt.Sprintf("alias%02d", group), SourceID: id, Quote: content}}
		}
		if i%10 == 9 {
			r.Kind = "insight"
			r.Basis = "inferred"
			r.Aliases = nil
			r.Parents = []string{fmt.Sprintf("bench_%08d", i-2), fmt.Sprintf("bench_%08d", i-1)}
			for _, parent := range previous {
				r.SourceIDs = append(r.SourceIDs, parent.SourceIDs...)
				r.Evidence = append(r.Evidence, parent.Evidence...)
			}
			r.SourceIDs = uniqueMemoryStrings(r.SourceIDs)
		}
		stamp := formatTimestamp(at)
		if _, err := tx.Exec(`INSERT INTO memory_sources(id,kind,object_id,version,speaker,session_id,body,recorded_at) VALUES(?,'standalone',?,'1','owner',?,?,?)`, id, id, fmt.Sprintf("thread_%d", i%200), content, stamp); err != nil {
			t.Fatal(err)
		}
		insertCapacityRevision(t, tx, r, true)
		if _, err := tx.Exec(`INSERT INTO memory_mentions(record_id,entity_id) VALUES(?,?)`, id, group+1); err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			if _, err := tx.Exec(`INSERT INTO memory_mentions(record_id,entity_id) VALUES(?,100)`, id); err != nil {
				t.Fatal(err)
			}
		}
		if i > 0 {
			if _, err := tx.Exec(`INSERT INTO memory_links(from_id,to_id,relation) VALUES(?,?,'related_to')`, id, fmt.Sprintf("bench_%08d", i-1)); err != nil {
				t.Fatal(err)
			}
		}
		for _, parent := range r.Parents {
			if _, err := tx.Exec(`INSERT INTO memory_links(from_id,to_id,relation) VALUES(?,?,'derives_from')`, id, parent); err != nil {
				t.Fatal(err)
			}
		}
		vector := make([]float32, dimensions)
		for d := range vector {
			vector[d] = float32(.8*centers[group][d] + .2*(rng.Float64()-.5))
		}
		encoded, err := encodeMemoryVector(vector)
		if err != nil {
			t.Fatal(err)
		}
		codes, scale := quantizeMemoryVector(encoded)
		if _, err := tx.Exec(`INSERT INTO memory_embeddings(record_id,revision,generation,dimensions,vector) VALUES(?,1,1,?,?)`, id, dimensions, encoded); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO memory_vector_sketches(generation,record_id,revision,dimensions,scale,codes) VALUES(1,?,1,?,?,?)`, id, dimensions, scale, codes); err != nil {
			t.Fatal(err)
		}
		if i%5 == 0 {
			// A fifth of records have a second revision, with both vectors present.
			r.Revision = 2
			r.State = "disputed"
			r.UpdatedAt = at.Add(30 * time.Second)
			if err := persistMemoryRecord(context.Background(), tx, r, "later conflicting observation"); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`DELETE FROM memory_jobs WHERE operation='embed' AND object_id=?`, id); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO memory_embeddings(record_id,revision,generation,dimensions,vector) VALUES(?,2,1,?,?)`, id, dimensions, encoded); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO memory_vector_sketches(generation,record_id,revision,dimensions,scale,codes) VALUES(1,?,2,?,?,?)`, id, dimensions, scale, codes); err != nil {
				t.Fatal(err)
			}
		}
		if i < 20 {
			prefix := "topic"
			if i%2 == 0 && i%5 != 0 {
				prefix = "alias"
			}
			corpus.texts = append(corpus.texts, fmt.Sprintf("%s%02d 项目经历 %08d", prefix, group, i))
		}
		previous = append(previous, r)
		if len(previous) > 2 {
			previous = previous[1:]
		}
	}
	// A populated index also has its completed durable processing history.
	for _, statement := range []string{
		`INSERT INTO memory_jobs(operation,object_id,version,state,available_at,created_at,completed_at) SELECT 'extract',id,version,'done',recorded_at,recorded_at,recorded_at FROM memory_sources`,
		`INSERT INTO memory_jobs(operation,object_id,version,state,available_at,created_at,completed_at) SELECT 'embed',record_id,CAST(revision AS TEXT),'done',at,at,at FROM memory_revisions`,
		`INSERT INTO memory_jobs(operation,object_id,version,state,available_at,created_at,completed_at) SELECT 'consolidate',v.record_id,CAST(v.revision AS TEXT),'done',v.at,v.at,v.at FROM memory_revisions v JOIN memory_records r ON r.id=v.record_id WHERE r.kind='fact'`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return corpus
}

// insertCapacityRevision writes one revision and its relations directly, so the
// fixture controls the processing history recorded afterwards.
func insertCapacityRevision(t *testing.T, tx *sql.Tx, r core.MemoryRecord, current bool) {
	t.Helper()
	stamp := formatTimestamp(r.UpdatedAt)
	if current {
		if _, err := tx.Exec(`INSERT INTO memory_records(id,kind,content,scope,state,basis,revision,recorded_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.ID, r.Kind, r.Content, r.Scope, r.State, r.Basis, r.Revision, formatTimestamp(r.RecordedAt), stamp); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO memory_revisions(record_id,revision,kind,content,scope,state,basis,at,reason) VALUES(?,?,?,?,?,?,?,?,'capacity fixture')`, r.ID, r.Revision, r.Kind, r.Content, r.Scope, r.State, r.Basis, stamp); err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Evidence {
		if _, err := tx.Exec(`INSERT INTO memory_revision_evidence(record_id,revision,source_id,quote,relation) VALUES(?,?,?,?,'supports')`, r.ID, r.Revision, e.SourceID, e.Quote); err != nil {
			t.Fatal(err)
		}
	}
	for table, values := range map[string][]string{"memory_revision_entities(record_id,revision,entity)": r.Entities, "memory_revision_parents(record_id,revision,parent_id)": r.Parents, "memory_revision_sources(record_id,revision,source_id)": r.SourceIDs} {
		for _, value := range values {
			if _, err := tx.Exec(`INSERT INTO `+table+` VALUES(?,?,?)`, r.ID, r.Revision, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := persistMemoryAliases(context.Background(), tx, r); err != nil {
		t.Fatal(err)
	}
}

func (c capacityCorpus) EmbedStrings(_ context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, text := range texts {
		var group int
		format := "topic%02d"
		if strings.HasPrefix(text, "alias") {
			format = "alias%02d"
		}
		if _, err := fmt.Sscanf(text, format, &group); err != nil {
			return nil, fmt.Errorf("capacity embedding requires topic: %w", err)
		}
		out = append(out, c.vectors[group%len(c.vectors)])
	}
	return out, nil
}

type capacityWorkerModel struct{}

func (capacityWorkerModel) GenerateMemory(_ context.Context, instruction, input string, _ int) (memory.Generation, error) {
	if !strings.HasPrefix(instruction, "Extract") {
		return memory.Generation{Text: `{"changes":[],"links":[]}`}, nil
	}
	var in struct {
		Source core.MemorySource `json:"source"`
	}
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return memory.Generation{}, err
	}
	data, err := json.Marshal(map[string]any{"records": []core.MemoryDraft{{Kind: "fact", Content: in.Source.Content, Entities: []string{strings.Fields(in.Source.Content)[0]}, Evidence: []core.MemoryEvidence{{SourceID: in.Source.ID, Quote: in.Source.Content}}}}})
	return memory.Generation{Text: string(data)}, err
}

func runCapacityIngestion(ctx context.Context, db *Store, engine *memory.Engine, completed func()) error {
	for i := 0; ctx.Err() == nil; i++ {
		id := fmt.Sprintf("ingestion_%06d", i)
		if _, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: "standalone", ObjectID: id, Version: "1", Speaker: "owner", Body: fmt.Sprintf("topic%02d 记录第 %d 次实际试用，今天完成样例检查。", i%32, i), RecordedAt: time.Now()}); err != nil {
			return err
		}
		for {
			worked, err := engine.ProcessOne(ctx)
			if err != nil {
				return err
			}
			if !worked {
				break
			}
		}
		completed()
	}
	return ctx.Err()
}
