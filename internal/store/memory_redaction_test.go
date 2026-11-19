package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/redact"
)

func TestMemorySourcesAreRedactedOnlyForMemory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "bank", "bank"); err != nil {
		t.Fatal(err)
	}
	message, err := s.AppendSessionMessage(ctx, "bank", 1, "user", "我的验证码是 482913，另外我住在杭州", "")
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("message:%d", message.ID)
	source, err := s.LoadMemorySource(ctx, id)
	if err != nil || source.Content != "我的验证码是 [REDACTED:otp]，另外我住在杭州" {
		t.Fatalf("memory source=%q %v", source.Content, err)
	}
	thread, err := s.LoadThreadSource(ctx, id)
	if err != nil || thread.Content != message.Content {
		t.Fatalf("thread source=%q %v", thread.Content, err)
	}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: "owner 的验证码是 482913",
		Evidence: []core.MemoryEvidence{{SourceID: id, Quote: "我的验证码是 [REDACTED:otp]"}}}, Reason: "owner statement"}}, Now: memoryTestNow}); !errors.Is(err, core.ErrMemoryCredential) {
		t.Fatalf("credential in content: %v", err)
	}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: "owner 收到一个验证码",
		Evidence: []core.MemoryEvidence{{SourceID: id, Quote: "我的验证码是 482913"}}}, Reason: "owner statement"}}, Now: memoryTestNow}); err == nil {
		t.Fatal("evidence quoted the raw code")
	}
	facts, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: "owner 收到一个验证码",
		Evidence: []core.MemoryEvidence{{SourceID: id, Quote: "我的验证码是 [REDACTED:otp]"}}}, Reason: "owner statement"}}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}

	// Forgetting a quote of the redacted text hides the same words, code
	// included, in the thread.
	request := seedMemorySource(t, s, "owner:forget-code", "owner", "忘掉验证码")
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{facts[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	want := redact.ExcludedPlaceholder + "，另外我住在杭州"
	for name, load := range map[string]func(context.Context, string) (core.MemorySource, error){"memory": s.LoadMemorySource, "thread": s.LoadThreadSource} {
		got, err := load(ctx, id)
		if err != nil || got.Content != want || !got.Excluded {
			t.Fatalf("%s source after forget=%+v %v", name, got, err)
		}
	}
}
