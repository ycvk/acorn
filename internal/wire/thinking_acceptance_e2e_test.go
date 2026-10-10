package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
)

func withUsage(reply string, total int) string {
	chunk := fmt.Sprintf("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fake\",\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":5,\"total_tokens\":%d}}\n\n", total-5, total)
	return strings.Replace(reply, "data: [DONE]", chunk+"data: [DONE]", 1)
}

func TestThinkingNightRevisesThoughtWithEvidenceAndRecordsUsage(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 9, 19, 0, 0, 0, time.UTC)
	h, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+"owner:\n  timezone: Asia/Shanghai\nbriefing:\n  at: \"\"\nthinking:\n  night_at: \"03:00\"\n")
	installSeedSkill(t, cfg.WorkspaceRoot(), "night_reflection")
	c := h.open(t, cfg)
	defer c.Close()
	source, err := c.store.RegisterMemorySource(ctx, core.MemorySource{ID: "night-origin", Kind: "fixture", ObjectID: "night-origin", Version: "1", Speaker: "owner", Body: "review this idea", RecordedAt: start.Add(-72 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	records, err := c.store.CommitMemory(ctx, core.MemoryMutation{Now: start, Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "thought", Content: "obsolete idea", Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: source.Content, Relation: "supports"}}}, Reason: "question"}}})
	if err != nil {
		t.Fatal(err)
	}
	provider.replies = []string{
		withUsage(toolCallChunk("skill", "skill", map[string]string{"skill": "skill.night.reflection"}), 20),
		withUsage(toolCallChunk("release", "think", map[string]any{"id": records[0].ID, "revision": records[0].Revision, "state": "released", "content": "obsolete idea", "source_id": "message:1", "quote": "night reflection", "reason": "reviewed and no longer useful"}), 20),
		withUsage(textReply("已放下这个念头。"), 20),
	}
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	thread, err := c.store.LatestRoutineThread(ctx, "night")
	if err != nil || thread == "" {
		t.Fatalf("thread=%q err=%v", thread, err)
	}
	run := waitNewRun(t, c, thread, "")
	read, err := c.store.ReadMemory(ctx, records[0].ID)
	if err != nil || read.Record.State != "released" || read.Record.Revision != 2 {
		t.Fatalf("thought=%+v err=%v", read.Record, err)
	}
	events, err := c.store.LoadEvents(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	calls, wakes := 0, 0
	for _, event := range events {
		if event.Kind == core.EventModelUsage {
			calls++
			payload := event.Payload.(map[string]any)
			if payload["total_tokens"] != float64(20) || payload["reported"] != true {
				t.Fatalf("usage=%v", payload)
			}
		}
		if event.Kind == core.EventWakeFired {
			wakes++
		}
	}
	if calls != 3 || wakes != 1 {
		t.Fatalf("model calls=%d wakes=%d", calls, wakes)
	}
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.requestCount() != 3 || len(h.fcm.sent()) != 0 {
		t.Fatal("night repeated or pushed")
	}
}

func TestThinkingPhoneNotificationsReachPresenceAndBriefing(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 10, 9, 23, 0, 0, 0, time.UTC)
	h, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{replies: []string{
		textReply("知道了"),
		toolCallChunk("note", "knowledge_write", map[string]any{"path": "briefings/2026-10-10.md", "title": "早安", "body": "手机通知：银行支出 2799 元。"}),
		toolCallChunk("push", "notify_owner", map[string]string{"title": "早安", "body": "银行支出已整理"}),
		textReply("简报已写入"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+watchTestConfig+"thinking:\n  night_at: \"\"\n")
	c := h.open(t, cfg)
	defer c.Close()
	registerPushToken(t, c, "phone")
	client := newPairedClient(t, c)
	batch := api.PhoneNotificationBatch{Notifications: []api.PhoneNotificationInput{{Key: "bank", Package: "bank", App: "Bank", Title: "支付提醒", Text: "支出 2799 元", PostedAt: start}, {Key: "ad", Package: "shop", App: "Shop", Title: "广告", Text: "大促销", PostedAt: start}}}
	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	unauth := httptest.NewRecorder()
	client.handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, "/v1/phone-notifications", strings.NewReader(string(body))))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth=%d", unauth.Code)
	}
	for _, want := range []int{2, 0} {
		var accepted api.PhoneNotificationAccepted
		client.do(t, httptest.NewRequest(http.MethodPost, "/v1/phone-notifications", strings.NewReader(string(body))), http.StatusOK, &accepted)
		if accepted.Accepted != want {
			t.Fatalf("accepted=%+v", accepted)
		}
	}
	thread, err := c.threads.CreateThread(ctx, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.runs.CreateRun(ctx, thread.ID, "", "早上好"); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, thread.ID, "")
	msgs := requestMessages(provider.request(0))
	present := chatContentText(msgs[len(msgs)-1]["content"])
	if !strings.Contains(present, "## Phone notifications") || !strings.Contains(present, "2799") {
		t.Fatalf("presence=%s", present)
	}
	h.clock.Set(start.Add(time.Hour))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	brief, err := c.store.LatestRoutineThread(ctx, "briefing")
	if err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, brief, "")
	msgs = requestMessages(provider.request(1))
	input := chatContentText(msgs[len(msgs)-3]["content"])
	if !strings.Contains(input, "Phone notifications since the last briefing") || !strings.Contains(input, "大促销") || !strings.Contains(input, "2799") {
		t.Fatalf("briefing=%s", input)
	}
	note, err := os.ReadFile(cfg.KnowledgeDir() + "/briefings/2026-10-10.md")
	if err != nil || !strings.Contains(string(note), "2799") || strings.Contains(string(note), "大促销") {
		t.Fatalf("note=%s %v", note, err)
	}
	if len(h.fcm.sent()) != 1 {
		t.Fatalf("pushes=%v", h.fcm.sent())
	}
	if !strings.Contains(gitLog(t, cfg.KnowledgeDir(), "--format=%B"), "Acorn-Run:") {
		t.Fatal("note not committed with run identity")
	}
}

func TestThinkingTokenBudgetAcrossOwnerDays(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, time.UTC)
	h, extra := newPushHarness(t, start)
	provider := &fakeOpenAI{replies: []string{withUsage(textReply("first wake"), 150), withUsage(textReply("briefing"), 150), withUsage(textReply("next day"), 150)}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, extra+"owner:\n  timezone: UTC\nwake:\n  daily_tokens: 100\nbriefing:\n  at: \"08:00\"\nthinking:\n  night_at: \"\"\n")
	c := h.open(t, cfg)
	defer c.Close()
	thread, err := c.threads.CreateThread(ctx, "budget")
	if err != nil {
		t.Fatal(err)
	}
	add := func(text string) {
		t.Helper()
		if _, err := c.store.AddCommitment(ctx, core.Commitment{State: "scheduled", Content: text, SessionID: thread.ID, WakeAt: h.clock.Now(), CreatedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	add("first")
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	first := waitNewRun(t, c, thread.ID, "")
	add("second")
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if provider.requestCount() != 1 {
		t.Fatal("budget did not stop the second wake")
	}
	h.clock.Set(start.Add(7 * time.Hour))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	brief, err := c.store.LatestRoutineThread(ctx, "briefing")
	if err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, brief, "")
	h.clock.Set(start.Add(24 * time.Hour))
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, thread.ID, first.RunID)
	if provider.requestCount() != 3 {
		t.Fatalf("calls=%d", provider.requestCount())
	}
}
