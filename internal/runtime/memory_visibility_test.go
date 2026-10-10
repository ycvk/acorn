package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/store"
)

func TestMemoryVisibilityRebuildsCompactedStateBeforeModel(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.CreateSession(ctx, "thread", "thread"); err != nil {
		t.Fatal(err)
	}
	msg, err := db.AppendSessionMessage(ctx, "thread", 1, "user", "红色是我的偏好；我也喜欢散步", "")
	if err != nil {
		t.Fatal(err)
	}
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "forget", Kind: "fixture", ObjectID: "forget", Version: "1", Speaker: "owner", Body: "忘记红色偏好", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	records, err := db.CommitMemory(ctx, core.MemoryMutation{Now: time.Now(), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: "喜欢红色", Evidence: []core.MemoryEvidence{{SourceID: "message:1", Quote: "红色是我的偏好", Relation: "supports"}}}, Reason: "owner preference"}}})
	if err != nil {
		t.Fatal(err)
	}
	mem := &runMemory{store: db, history: memory.History{Messages: []core.SessionMessageRecord{*msg}, SourceIDs: []string{"message:1"}, Summary: &core.ThreadSummary{Content: "owner喜欢红色"}}}
	state := &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: []*schema.AgenticMessage{schema.SystemAgenticMessage("compacted: owner喜欢红色"), schema.UserAgenticMessage("old private text")}}
	mw := &memoryVisibilityMiddleware{TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{}, memory: mem}
	if _, err = db.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{records[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, filtered, err := mw.BeforeModelRewriteState(ctx, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range filtered.Messages {
		text := messageText(m)
		if strings.Contains(text, "红色") || strings.Contains(text, "old private") {
			t.Fatalf("stale content: %s", text)
		}
	}
	if !strings.Contains(messageText(filtered.Messages[0]), "喜欢散步") {
		t.Fatal("unrelated source fragment lost")
	}
	if !strings.Contains(messageText(state.Messages[0]), "红色") {
		t.Fatal("caller state mutated")
	}
	if mem.epoch.Load() != 1 {
		t.Fatal("visibility version not advanced")
	}
}

func TestCheckpointExclusionVersionFencesRestoreAndSave(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	src, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner", Kind: "fixture", ObjectID: "owner", Version: "1", Speaker: "owner", Body: "forget", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	mem := &runMemory{clock: time.Now}
	cp := storeCheckpointStore{store: db, memory: db, visibility: mem}
	if err = cp.Set(ctx, "paused", []byte("old input")); err != nil {
		t.Fatal(err)
	}
	if data, ok, err := cp.Get(ctx, "paused"); err != nil || !ok || string(data) != "old input" {
		t.Fatalf("checkpoint=%s %t %v", data, ok, err)
	}
	if _, err = db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{src.ID}, RequestSourceID: src.ID, Reason: "owner request", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = cp.Get(ctx, "paused"); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("restore err=%v", err)
	}
	if err = cp.Set(ctx, "paused", []byte("stale write")); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("save err=%v", err)
	}
}

func TestForgetBarrierWaitsForAffectedRunToExit(t *testing.T) {
	controller := NewRunController()
	ctx, cancel := context.WithCancel(context.Background())
	cleanup := controller.Register("old", cancel)
	done := make(chan error, 1)
	go func() {
		done <- controller.ForgetBarrier(context.Background(), core.MemoryExclusion{RunIDs: []string{"old", "caller"}}, "caller")
	}()
	<-ctx.Done()
	select {
	case <-done:
		t.Fatal("barrier acknowledged before old output finished")
	default:
	}
	cleanup()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("barrier did not finish")
	}
}

func TestMemoryWatcherCancelsForeignRunAndPreservesForgetCaller(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "watch-source", Kind: "standalone", ObjectID: "source", Version: "1", Speaker: "owner", Body: "需要遗忘的原话", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "watch-request", Kind: "standalone", ObjectID: "request", Version: "1", Speaker: "owner", RunID: "forget-caller", Body: "忘掉这句话", RecordedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"foreign-run", "forget-caller"} {
		if err := db.SaveMemorySnapshotRefs(ctx, "watch-snapshot", run, []string{source.ID}, nil, 0); err != nil {
			t.Fatal(err)
		}
	}
	foreign, cancelForeign := context.WithCancel(ctx)
	defer cancelForeign()
	caller, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	doneForeign := make(chan struct{})
	doneCaller := make(chan struct{})
	go func() { defer close(doneForeign); watchRunMemory(foreign, db, "foreign-run", 0, cancelForeign) }()
	go func() { defer close(doneCaller); watchRunMemory(caller, db, "forget-caller", 0, cancelCaller) }()
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{source.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-foreign.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("foreign model context did not cancel")
	}
	select {
	case <-caller.Done():
		t.Fatal("forget caller was cancelled before acknowledgement")
	case <-time.After(300 * time.Millisecond):
	}
	cancelCaller()
	<-doneCaller
	<-doneForeign
}

func TestForgetAcknowledgementRequiresSuccessfulMatchingToolResult(t *testing.T) {
	success := toolResultMessage(`{"exclusion_version":7,"excluded_record_ids":["record"]}`, "call", "memory_forget")
	if !memoryForgetConfirmed([]*schema.AgenticMessage{success}, 7) {
		t.Fatal("successful cancellation outcome missing")
	}
	for _, message := range []*schema.AgenticMessage{toolResultMessage("memory exclusions committed; termination unconfirmed", "call", "memory_forget"), toolResultMessage(`{"exclusion_version":6,"excluded_record_ids":["record"]}`, "call", "memory_forget"), toolResultMessage(`{"exclusion_version":7,"excluded_record_ids":["record"]}`, "call", "recall")} {
		if memoryForgetConfirmed([]*schema.AgenticMessage{message}, 7) {
			t.Fatal("unconfirmed cancellation was acknowledged as complete")
		}
	}
}
