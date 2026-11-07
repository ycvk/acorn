package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func registerOwnerWords(t *testing.T, db interface {
	RegisterMemorySource(context.Context, core.MemorySource) (core.MemorySource, error)
}, id, text string, at time.Time) core.MemorySource {
	t.Helper()
	source, err := db.RegisterMemorySource(context.Background(), core.MemorySource{ID: id, Kind: "import", ObjectID: id, Version: "1", Speaker: "owner", Body: text, RecordedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestPortraitRefreshWritesLinesCitingTheOwnersWordsAndMemory(t *testing.T) {
	now := time.Now()
	var seen portraitInput
	model := &protocolModel{portrait: func(input string) string {
		if err := json.Unmarshal([]byte(input), &seen); err != nil {
			t.Fatal(err)
		}
		return `{"lines":[{"text":"每天早上跑步","cites":["S1","S2"]},{"text":"在写一个叫 Acorn 的 agent","cites":["R1"]}]}`
	}}
	e, db := testEngine(t, model, 100000)
	ctx := context.Background()
	older := registerOwnerWords(t, db, "owner:run1", "我早上跑了五公里", now.Add(-2*time.Hour))
	newer := registerOwnerWords(t, db, "owner:run2", "今天早上又去跑步了", now.Add(-time.Hour))
	drainMemory(t, e)
	if err := db.QueuePortrait(ctx, "day:"+now.Format("2006-01-02"), now); err != nil {
		t.Fatal(err)
	}
	drainMemory(t, e)

	if len(seen.Messages) != 2 || seen.Messages[0].Label != "S1" || seen.Messages[0].Text != newer.Content || seen.Messages[1].Text != older.Content {
		t.Fatalf("messages the refresh read = %+v", seen.Messages)
	}
	if len(seen.Records) != 2 || seen.Records[0].Label != "R1" {
		t.Fatalf("records the refresh read = %+v", seen.Records)
	}
	records, err := db.ListMemoryRecords(ctx, core.MemoryQuery{Kind: "fact", Mode: "current", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	lines, err := db.Portrait(ctx)
	if err != nil || len(lines) != 2 {
		t.Fatalf("portrait = %+v, %v", lines, err)
	}
	if !slices.Equal(lines[0].SourceIDs, []string{newer.ID, older.ID}) || !slices.Equal(lines[1].SourceIDs, records[0].SourceIDs) {
		t.Fatalf("line sources = %v and %v; R1 stands for %v", lines[0].SourceIDs, lines[1].SourceIDs, records[0].SourceIDs)
	}

	// The next refresh sees the portrait so far and carries a line by citing it.
	model.portrait = func(input string) string {
		if err := json.Unmarshal([]byte(input), &seen); err != nil {
			t.Fatal(err)
		}
		return `{"lines":[{"text":"每天早上跑步","cites":["L1"]}]}`
	}
	if err := db.QueuePortrait(ctx, "forget:9", now); err != nil {
		t.Fatal(err)
	}
	drainMemory(t, e)
	if len(seen.Current) != 2 || seen.Current[0] != (portraitLabeled{Label: "L1", Text: "每天早上跑步"}) {
		t.Fatalf("current lines the refresh read = %+v", seen.Current)
	}
	if lines, err = db.Portrait(ctx); err != nil || len(lines) != 1 || !slices.Equal(lines[0].SourceIDs, []string{newer.ID, older.ID}) {
		t.Fatalf("carried line = %+v, %v", lines, err)
	}
}

func TestDailyPortraitRefreshCallsTheModelOnlyForSomethingNew(t *testing.T) {
	now := time.Now()
	refreshes := 0
	model := &protocolModel{portrait: func(string) string {
		refreshes++
		return `{"lines":[{"text":"住在新加坡","cites":["S1"]}]}`
	}}
	e, db := testEngine(t, model, 100000)
	ctx := context.Background()
	registerOwnerWords(t, db, "owner:home", "我住在新加坡", now.Add(-time.Hour))
	drainMemory(t, e)
	for _, version := range []string{"day:1", "day:2"} {
		if err := db.QueuePortrait(ctx, version, e.cfg.Clock()); err != nil {
			t.Fatal(err)
		}
		drainMemory(t, e)
		if refreshes != 1 {
			t.Fatalf("after %s the model refreshed the portrait %d times", version, refreshes)
		}
	}
	registerOwnerWords(t, db, "owner:later", "我开始学日语了", e.cfg.Clock().Add(time.Second))
	drainMemory(t, e)
	if err := db.QueuePortrait(ctx, "day:3", e.cfg.Clock()); err != nil {
		t.Fatal(err)
	}
	drainMemory(t, e)
	if refreshes != 2 {
		t.Fatalf("a new message did not refresh the portrait: %d refreshes", refreshes)
	}
}

func TestPortraitRefreshWithNothingKnownWritesNothing(t *testing.T) {
	model := &protocolModel{portrait: func(string) string {
		t.Fatal("the model was asked for a portrait of nothing")
		return ""
	}}
	e, db := testEngine(t, model, 100000)
	if err := db.QueuePortrait(context.Background(), "bootstrap", time.Now()); err != nil {
		t.Fatal(err)
	}
	drainMemory(t, e)
	if refreshed, err := db.PortraitRefreshedAt(context.Background()); err != nil || refreshed.IsZero() {
		t.Fatalf("empty refresh did not finish: %v, %v", refreshed, err)
	}
}

func TestPortraitDraftsMustCiteWhatTheRefreshRead(t *testing.T) {
	e, _ := testEngine(t, &protocolModel{}, 100000)
	cites := map[string][]string{"S1": {"owner:a"}, "R1": {"owner:a", "owner:b"}, "L1": {"owner:c"}}
	drafts, err := e.portraitDrafts(context.Background(), `{"lines":[{"text":" 喜欢跑步 ","cites":["S1","R1","L1"]}]}`, cites)
	if err != nil || len(drafts) != 1 || drafts[0].Content != "喜欢跑步" || !slices.Equal(drafts[0].SourceIDs, []string{"owner:a", "owner:b", "owner:c"}) {
		t.Fatalf("drafts = %+v, %v", drafts, err)
	}
	tooMany := make([]string, portraitMaxLines+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf(`{"text":"第 %d 条","cites":["S1"]}`, i)
	}
	for name, reply := range map[string]string{
		"unknown label":   `{"lines":[{"text":"喜欢跑步","cites":["S2"]}]}`,
		"no cites":        `{"lines":[{"text":"喜欢跑步","cites":[]}]}`,
		"empty text":      `{"lines":[{"text":"  ","cites":["S1"]}]}`,
		"two lines":       `{"lines":[{"text":"喜欢跑步\n也喜欢游泳","cites":["S1"]}]}`,
		"too long":        `{"lines":[{"text":"` + strings.Repeat("长", portraitLineRunes+1) + `","cites":["S1"]}]}`,
		"too many lines":  `{"lines":[` + strings.Join(tooMany, ",") + `]}`,
		"unknown field":   `{"lines":[{"text":"喜欢跑步","cites":["S1"],"why":"x"}]}`,
		"over the budget": `{"lines":[` + strings.Repeat(`{"text":"`+strings.Repeat("长", portraitLineRunes)+`","cites":["S1"]},`, 11) + `{"text":"x","cites":["S1"]}]}`,
	} {
		if _, err := e.portraitDrafts(context.Background(), reply, cites); err == nil {
			t.Errorf("%s: drafts accepted", name)
		}
	}
}

func TestPortraitInputFitsOneMemoryCall(t *testing.T) {
	e, _ := testEngine(t, &protocolModel{}, 100000)
	now := time.Now()
	var records []core.MemoryRecord
	var messages []core.MemorySource
	for i := range 300 {
		records = append(records, core.MemoryRecord{ID: fmt.Sprintf("m_%d", i), Kind: "fact", Content: strings.Repeat("事", 80), SourceIDs: []string{fmt.Sprintf("owner:%d", i)}, RecordedAt: now})
		messages = append(messages, core.MemorySource{ID: fmt.Sprintf("owner:%d", i), Content: strings.Repeat("话", 600), RecordedAt: now})
	}
	lines := []core.PortraitLine{{Content: "住在新加坡", State: core.PortraitLineShown, SourceIDs: []string{"owner:x"}}}
	in, cites, err := e.portraitInput(context.Background(), "ycvk", "2026-11-05", lines, records, messages)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	n, err := e.cfg.Count(context.Background(), portraitInstruction+"\n"+string(data))
	if err != nil || n > BatchTokens {
		t.Fatalf("portrait input uses %d tokens, %v", n, err)
	}
	if len(in.Records) == 0 || len(in.Messages) == 0 || len(in.Records) == len(records) || len(in.Messages) == len(messages) {
		t.Fatalf("fitted %d records and %d messages", len(in.Records), len(in.Messages))
	}
	if got := []rune(in.Messages[0].Text); len(got) != portraitMessageRunes+1 {
		t.Fatalf("message clipped to %d runes", len(got))
	}
	if !slices.Equal(cites["L1"], []string{"owner:x"}) || !slices.Equal(cites["S1"], []string{"owner:0"}) || !slices.Equal(cites["R1"], []string{"owner:0"}) {
		t.Fatalf("cites = L1 %v S1 %v R1 %v", cites["L1"], cites["S1"], cites["R1"])
	}
}

func TestPortraitMessagesCarryTheirConversation(t *testing.T) {
	e, _ := testEngine(t, &protocolModel{}, 100000)
	now := time.Now()
	var messages []core.MemorySource
	for i, session := range []string{"session_go", "session_news", "session_go", ""} {
		messages = append(messages, core.MemorySource{ID: fmt.Sprintf("owner:%d", i), SessionID: session, Content: "话", RecordedAt: now})
	}
	in, _, err := e.portraitInput(context.Background(), "ycvk", "2026-11-05", nil, nil, messages)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range in.Messages {
		got = append(got, m.Conversation)
	}
	if !slices.Equal(got, []string{"C1", "C2", "C1", ""}) {
		t.Fatalf("conversations = %q", got)
	}
}

// A refresh over a month of the owner's messages took grok-4.7 163 s and
// 9263 output tokens of reasoning; one memory call holds a refresh that size.
func TestPortraitRefreshLeavesAReasoningModelTimeAndRoom(t *testing.T) {
	now := time.Now()
	var left time.Duration
	model := &protocolModel{portrait: func(string) string { return `{"lines":[{"text":"每天早上跑步","cites":["S1"]}]}` }}
	e, db := testEngine(t, model, 100000)
	ctx := context.Background()
	registerOwnerWords(t, db, "owner:run", "我每天早上跑五公里", now.Add(-time.Hour))
	drainMemory(t, e)
	model.maxOutputs = nil
	model.fail = func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("the portrait call has no deadline")
		}
		left = time.Until(deadline)
		return nil
	}
	if err := db.QueuePortrait(ctx, "forget:1", now); err != nil {
		t.Fatal(err)
	}
	drainMemory(t, e)

	if lines, err := db.Portrait(ctx); err != nil || len(lines) != 1 {
		t.Fatalf("portrait = %+v, %v", lines, err)
	}
	if left < 163*time.Second {
		t.Fatalf("the portrait call had %s to reply", left)
	}
	if len(model.maxOutputs) != 1 || model.maxOutputs[0] < 9263 {
		t.Fatalf("portrait output limits = %v", model.maxOutputs)
	}
	if memoryJobLease <= memoryCallTimeout {
		t.Fatalf("lease %s ends before the call deadline %s", memoryJobLease, memoryCallTimeout)
	}
}
