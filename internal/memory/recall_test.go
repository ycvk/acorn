package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type relevanceModel struct{ target string }

func (m relevanceModel) GenerateMemory(_ context.Context, _, input string, _ int) (Generation, error) {
	var candidates struct {
		Records []core.MemoryHit `json:"records"`
	}
	if err := json.Unmarshal([]byte(input), &candidates); err != nil {
		return Generation{}, err
	}
	for _, h := range candidates.Records {
		if h.Record.ID == m.target {
			return Generation{Text: fmt.Sprintf(`{"ids":[%q]}`, m.target)}, nil
		}
	}
	return Generation{Text: `{"ids":[]}`}, nil
}

func TestDeepRecallExpandsAnAdditionalEvidenceLink(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	var ids []string
	for i, content := range []string{"起点", "执行尝试", "观察结果"} {
		s, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: fmt.Sprintf("source-%d", i), Kind: "standalone", ObjectID: fmt.Sprint(i), Version: "1", Speaker: "owner", Body: content, RecordedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		r, err := db.CommitMemory(ctx, core.MemoryMutation{Now: time.Now(), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: content, Evidence: []core.MemoryEvidence{{SourceID: s.ID, Quote: content}}}, Reason: "observed event"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r[0].ID)
	}
	if _, err := db.CommitMemory(ctx, core.MemoryMutation{Now: time.Now(), Links: []core.MemoryLink{{FromID: ids[0], ToID: ids[1], Relation: "related_to"}, {FromID: ids[1], ToID: ids[2], Relation: "related_to"}}}); err != nil {
		t.Fatal(err)
	}
	standard, err := e.Recall(ctx, core.MemoryQuery{Query: "起点", Depth: "standard"})
	if err != nil || slices.ContainsFunc(standard.Hits, func(h core.MemoryHit) bool { return h.Record.ID == ids[2] }) {
		t.Fatalf("standard=%+v err=%v", standard, err)
	}
	e.cfg.Model = relevanceModel{target: ids[2]}
	deep, err := e.Recall(ctx, core.MemoryQuery{Query: "起点", Depth: "deep"})
	if err != nil || len(deep.Hits) != 1 || deep.Hits[0].Record.ID != ids[2] {
		t.Fatalf("deep=%+v err=%v", deep, err)
	}
}
