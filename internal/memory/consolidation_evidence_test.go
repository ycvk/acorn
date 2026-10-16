package memory

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// newInsightModel answers every consolidation with one new insight whose
// parents are all the records it was shown.
type newInsightModel struct{}

func (newInsightModel) GenerateMemory(_ context.Context, _, input string, _ int) (Generation, error) {
	var in consolidationInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return Generation{}, err
	}
	var parents []string
	for _, r := range append(in.Changed, in.Related...) {
		parents = append(parents, r.ID)
	}
	out, err := json.Marshal(map[string]any{"changes": []core.MemoryChange{{Reason: "shared pattern", Draft: core.MemoryDraft{Kind: "insight", Content: "owner 偏好安静的独处活动", State: "current", Parents: parents}}}, "links": []core.MemoryLink{}})
	return Generation{Text: string(out)}, err
}

// An insight needs at least two distinct owner sources behind its parents.
// Facts from one owner statement, or from the owner plus a tool result, are
// not independent evidence and the whole change set is rejected.
func TestConsolidationInsightNeedsTwoIndependentOwnerSources(t *testing.T) {
	cases := []struct {
		name     string
		speakers []string
		accepted bool
	}{
		{"one owner statement", []string{"owner"}, false},
		{"owner statement and tool result", []string{"owner", "tool"}, false},
		{"two owner statements", []string{"owner", "owner"}, true},
	}
	contents := []string{"周末我一个人去爬山", "周三晚上我独自去图书馆"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, db := testEngine(t, &protocolModel{}, 0)
			now := time.Now()
			e.cfg.Clock = func() time.Time { return now }
			ctx := context.Background()
			var changes []core.MemoryChange
			if len(tc.speakers) == 1 {
				source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "walks", Kind: "standalone", ObjectID: "walks", Version: "1", Speaker: tc.speakers[0], Body: strings.Join(contents, "。"), RecordedAt: now})
				if err != nil {
					t.Fatal(err)
				}
				for _, quote := range contents {
					changes = append(changes, core.MemoryChange{Reason: "owner statement", Draft: core.MemoryDraft{Kind: "fact", Content: quote, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: quote}}}})
				}
			} else {
				for i, speaker := range tc.speakers {
					id := "source-" + strconv.Itoa(i)
					source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: "standalone", ObjectID: id, Version: "1", Speaker: speaker, Body: contents[i], RecordedAt: now})
					if err != nil {
						t.Fatal(err)
					}
					changes = append(changes, core.MemoryChange{Reason: speaker + " evidence", Draft: core.MemoryDraft{Kind: "fact", Content: contents[i], Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: contents[i]}}}})
				}
			}
			facts, err := db.CommitMemory(ctx, core.MemoryMutation{Now: now, Changes: changes})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range facts {
				semanticFixtureVector(t, db, f, []float32{1, 0}, now)
			}
			e.cfg.Model = newInsightModel{}
			job, err := db.ClaimMemoryJob(ctx, now, time.Minute, "consolidate")
			if err != nil || job == nil {
				t.Fatalf("consolidation job=%+v err=%v", job, err)
			}
			err = e.consolidate(ctx, *job)
			if tc.accepted != (err == nil) {
				t.Fatalf("accepted=%v err=%v", tc.accepted, err)
			}
			insights, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", Kind: "insight", Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			if tc.accepted && (len(insights) != 1 || len(insights[0].Parents) != 2) {
				t.Fatalf("insight=%+v", insights)
			}
			if !tc.accepted && len(insights) != 0 {
				t.Fatalf("insight without independent owner evidence committed: %+v", insights)
			}
		})
	}
}
