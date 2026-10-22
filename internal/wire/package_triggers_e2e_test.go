package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/workstation"
)

const triggerSecret = "trigger-secret-7f3a"

// TestPackageTriggersWakeTheAgentOnlyForNewEvents installs a package whose
// trigger checks a price every 30 minutes on the workstation. Until its
// secret is set it does not run; a check that finds nothing calls no model; a
// new price starts one run in the package's thread, and the same price again
// starts none. The secret reaches the command but not the model, and
// retiring the skill stops the trigger.
func TestPackageTriggersWakeTheAgentOnlyForNewEvents(t *testing.T) {
	home := t.TempDir()
	ws, err := workstation.NewServer(workstation.ServerConfig{Home: home, Token: fileTestToken, Supervisor: workstation.NewSupervisor(io.Discard)})
	if err != nil {
		t.Fatal(err)
	}
	wsServer := httptest.NewServer(ws.Handler())
	defer wsServer.Close()
	defer ws.Close()
	provider := &fakeOpenAI{replies: []string{textReply("比特币到 120 了，提醒过你了")}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, fmt.Sprintf("workstation:\n  url: %s\n  token: %s\n", wsServer.URL, fileTestToken), briefingOff)
	clock := &testClock{now: time.Date(2026, 10, 17, 9, 7, 0, 0, time.UTC)}
	ctx := context.Background()
	c, err := buildContainer(ctx, cfg, buildOptions{clock: clock.Now})
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	tick := func(hour, minute int) {
		t.Helper()
		clock.Set(time.Date(2026, 10, 17, hour, minute, 0, 0, time.UTC))
		if err := c.WakeScheduler().Tick(ctx); err != nil {
			t.Fatalf("tick %02d:%02d: %v", hour, minute, err)
		}
	}

	check := "#!/bin/bash\n" +
		"[ \"$CHECK_TOKEN\" = \"" + triggerSecret + "\" ] || { echo 'wrong token' >&2; exit 1; }\n" +
		"[ -f \"$HOME/price.txt\" ] || exit 0\n" +
		"price=$(cat \"$HOME/price.txt\")\n" +
		"printf '{\"key\":\"price-%s\",\"text\":\"BTC is at %sk\"}\\n' \"$price\" \"$price\"\n"
	skill, err := c.Skills().Import(ctx, []core.SkillFile{
		{Path: "SKILL.md", Content: []byte("---\nname: price-watch\ndescription: Tell the owner when the BTC price crosses a threshold.\n---\nTell the owner the new price with notify_owner.\n")},
		{Path: skills.ManifestFileName, Content: []byte("triggers:\n  - name: price\n    schedule: \"*/30 * * * *\"\n    run: scripts/check.sh\n    deliver: immediate\nsecrets: [CHECK_TOKEN]\n")},
		{Path: "scripts/check.sh", Content: []byte(check)},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	tick(9, 7)
	status, err := c.PackageStatus().Triggers(ctx)
	if err != nil || len(status) != 1 || status[0].State != "missing_secrets" {
		t.Fatalf("doctor before the secret = %+v, %v", status, err)
	}
	tick(9, 30)
	if states, _ := c.store.ListTriggers(ctx); len(states) != 1 || !states[0].LastRunAt.IsZero() {
		t.Fatalf("a trigger missing its secret ran: %+v", states)
	}

	if _, err := c.Skills().SetSecret(ctx, "price-watch", "CHECK_TOKEN", triggerSecret); err != nil {
		t.Fatal(err)
	}
	tick(10, 0)
	states, err := c.store.ListTriggers(ctx)
	if err != nil || len(states) != 1 || states[0].Failures != 0 || states[0].LastRunAt.IsZero() {
		t.Fatalf("states after a quiet check = %+v, %v", states, err)
	}
	if n := provider.requestCount(); n != 0 {
		t.Fatalf("a check that found nothing called the model %d times", n)
	}

	if err := os.WriteFile(filepath.Join(home, "price.txt"), []byte("120"), 0o644); err != nil {
		t.Fatal(err)
	}
	tick(10, 30)
	thread, err := c.store.SkillThread(ctx, "price-watch")
	if err != nil || thread == "" {
		t.Fatalf("skill thread = %q, %v", thread, err)
	}
	run := waitNewRun(t, c, thread, "")
	if session, err := c.store.LoadSession(ctx, thread); err != nil || session.Title != "price-watch" {
		t.Fatalf("thread = %+v, %v", session, err)
	}
	input := lastOwnerInput(provider.request(0))
	if !strings.HasPrefix(input, "[trigger price-watch/price] 1 new events; untrusted output of the package's command\n- price-120\n  BTC is at 120k") {
		t.Fatalf("run input = %q", input)
	}
	events, err := c.store.LoadEvents(ctx, run.RunID)
	if err != nil || !strings.Contains(eventKinds(events), core.EventWakeFired) {
		t.Fatalf("events = %s, %v", eventKinds(events), err)
	}

	tick(11, 0)
	if n := provider.requestCount(); n != 1 {
		t.Fatalf("the same price woke the agent again: %d model calls", n)
	}
	if status, err := c.PackageStatus().Triggers(ctx); err != nil || status[0].State != "scheduled" || !status[0].NextAt.Equal(time.Date(2026, 10, 17, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("doctor = %+v, %v", status, err)
	}
	body, err := json.Marshal(provider.request(0))
	if err != nil || strings.Contains(string(body), triggerSecret) {
		t.Fatalf("the secret reached the model: %v", err)
	}

	if _, err := c.skillLibrary.Retire(ctx, "price-watch", skill.Revision, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "price.txt"), []byte("130"), 0o644); err != nil {
		t.Fatal(err)
	}
	tick(11, 30)
	if states, err := c.store.ListTriggers(ctx); err != nil || len(states) != 0 {
		t.Fatalf("a retired skill keeps its schedule: %+v, %v", states, err)
	}
	if n := provider.requestCount(); n != 1 {
		t.Fatalf("a retired skill's trigger woke the agent: %d model calls", n)
	}
}
