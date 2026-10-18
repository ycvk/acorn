package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// A fact the owner kept from a message is shown to extraction of that same
// message as an existing record, with the name the owner saved; a draft that repeats
// its content is dropped, a differently worded one is still committed.
func TestExtractionDropsDraftsRepeatingFactsTheSourceAlreadySupports(t *testing.T) {
	cases := []struct {
		name  string
		kept  string
		facts int
	}{
		{"identical content is dropped", "我不吃香菜", 1},
		{"different wording is committed", "owner 不吃香菜", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			model := &protocolModel{}
			engine, db := testEngine(t, model, 100000)
			if _, err := db.SetOwnerName(ctx, "小明", time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.CreateSession(ctx, "food", "饮食"); err != nil {
				t.Fatal(err)
			}
			if err := db.CreateRun(ctx, core.RunCreateParams{RunID: "run_food", SessionID: "food", TurnIndex: 1, Input: "记住"}); err != nil {
				t.Fatal(err)
			}
			msg, err := db.AppendSessionMessage(ctx, "food", 1, "user", "我不吃香菜", "run_food")
			if err != nil {
				t.Fatal(err)
			}
			sourceID := fmt.Sprintf("message:%d", msg.ID)
			kept, err := engine.Keep(ctx, "run_food", WriteInput{Content: tc.kept, SourceID: sourceID, Quote: "我不吃香菜", Entities: []string{"饮食"}})
			if err != nil {
				t.Fatal(err)
			}
			drainMemory(t, engine)
			if model.calls < 1 {
				t.Fatalf("extraction did not run: %d calls", model.calls)
			}
			var in struct {
				OwnerName string               `json:"owner_name"`
				Existing  []extractionExisting `json:"existing_records"`
			}
			if err := json.Unmarshal([]byte(model.seen[0]), &in); err != nil {
				t.Fatal(err)
			}
			if in.OwnerName != "小明" || len(in.Existing) != 1 || in.Existing[0].ID != kept.ID || in.Existing[0].Content != tc.kept {
				t.Fatalf("extraction input=%s", model.seen[0])
			}
			facts, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history", Kind: "fact", Limit: 10})
			if err != nil || len(facts) != tc.facts {
				t.Fatalf("facts=%+v err=%v", facts, err)
			}
			status, err := db.MemoryProcessingStatus(ctx)
			if err != nil || status.Pending != 0 || status.Failed != 0 {
				t.Fatalf("status=%+v %v", status, err)
			}
		})
	}
}
