package wire

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
)

// TestMemoryLiveRuntime uses the real primary model against an isolated store,
// knowledge repository and tool catalog. Production owner data is never opened.
func TestMemoryLiveRuntime(t *testing.T) {
	path := os.Getenv("ACORN_MEMORY_EVAL_CONFIG")
	if path == "" {
		t.Skip("set ACORN_MEMORY_EVAL_CONFIG for real-model runtime acceptance")
	}
	if envPath := os.Getenv("ACORN_MEMORY_EVAL_ENV"); envPath != "" {
		raw, err := os.ReadFile(envPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok && k == "ACORN_MODEL_API_KEY" {
				t.Setenv(k, strings.Trim(v, "\"'"))
			}
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(os.Getenv("ACORN_MEMORY_EVAL_VOYAGE_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Memory.Embedding.APIKey = strings.TrimSpace(string(key))
	cfg.Memory.DailyTokens = 0
	cfg.Wake.DailyTokens = 0
	cfg.Runtime.StorageDir = t.TempDir()
	cfg.Knowledge.Dir = filepath.Join(cfg.Runtime.StorageDir, "knowledge")
	cfg.Tools.Workspace.RootDir = t.TempDir()
	cfg.Notify.FCM.ServiceAccountFile = ""
	cfg.MCP.Providers = nil
	cfg.Briefing.At = ""
	cfg.Thinking.NightAt = "00:00"
	cfg.Thinking.WanderAt = nil
	if err := os.WriteFile(cfg.PersonaPath(), []byte(presence.DefaultPersona), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	run := func(input string) *RunOnceResult {
		t.Helper()
		result, err := c.RunOnce(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != "succeeded" {
			t.Fatalf("run status=%s error=%s output=%s", result.Status, result.Error, result.Output)
		}
		t.Logf("run=%s output=%s", result.RunID, result.Output)
		return result
	}
	drain := func() {
		t.Helper()
		for i := 0; i < 300; i++ {
			worked, err := c.memory.ProcessOne(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !worked {
				return
			}
		}
		t.Fatal("memory queue did not settle")
	}
	run("这是隔离验收数据。请用 keep 记住：我的虚构测试房间叫风铃阁。只保存这个事实，不写笔记，不调用其他外部工具，不推送。")
	drain()
	answer := run("请先用 recall，再用 memory_read 查看来源，回答我在前一个线程说的虚构测试房间叫什么。最后只回答这个称呼。")
	if !strings.Contains(answer.Output, "风铃阁") {
		t.Fatal("cross-thread memory was not recalled")
	}
	run("更正隔离验收数据：我的虚构测试房间现在改叫松涛轩，原来的名字已不再使用。请检索目标记录，用 memory_correct 引用这条消息完成更正。不写笔记，不推送。")
	drain()
	answer = run("只按当前有效记忆回答，我的虚构测试房间现在叫什么？")
	if !strings.Contains(answer.Output, "松涛轩") || strings.Contains(answer.Output, "风铃阁") {
		t.Fatal("current correction did not control the answer")
	}
	answer = run("请检索并忘记所有关于我虚构测试房间称呼的记忆，包括当前和历史称呼及派生认识。只处理这些隔离验收数据，调用 memory_forget，最后只确认已经忘记，不复述称呼。")
	if strings.Contains(answer.Output, "风铃阁") || strings.Contains(answer.Output, "松涛轩") {
		t.Fatal("forget acknowledgement repeated excluded text")
	}
	drain()
	visible, err := c.store.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range visible {
		if strings.Contains(record.Content, "风铃阁") || strings.Contains(record.Content, "松涛轩") {
			t.Fatalf("excluded memory remains: %s", record.ID)
		}
	}
	run("创建一个关切：隔离验收中需要继续验证记忆迁移；状态 waiting，理由是等待人工复核，下一次复查时间设为一个月后。不调用外部工具，不推送。")
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	thread, err := c.store.LatestRoutineThread(ctx, "night")
	if err != nil || thread == "" {
		t.Fatalf("night reflection did not start: %s %v", thread, err)
	}
	wait := func(runID string) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
			record, err := c.store.LoadRun(ctx, runID)
			if err != nil {
				t.Fatal(err)
			}
			if record.Status == core.RunStatusSucceeded {
				return
			}
			if record.Status == core.RunStatusFailed || record.Status == core.RunStatusInterrupted {
				t.Fatalf("scheduled run %s: %s %s", runID, record.Status, record.Error)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
		t.Fatal("scheduled run timed out")
	}
	night, err := c.store.LoadLatestRunForSession(ctx, thread)
	if err != nil || night == nil {
		t.Fatalf("night run %v", err)
	}
	wait(night.RunID)
	now := time.Now().UTC()
	commitment, err := c.store.AddCommitment(ctx, core.Commitment{Content: "隔离验收任务：调用 knowledge_list 检查知识库，调用成功即完成本任务；随后用 settle done 完成本次 occurrence，引用该成功工具结果。不要推送。", SessionID: thread, WakeAt: now, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.wake.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	occurrences, err := c.store.ListDueOccurrences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	for _, o := range occurrences {
		if o.CommitmentID == commitment.ID {
			runID = o.RunID
		}
	}
	if runID == "" {
		t.Fatal("commitment occurrence was not bound before execution")
	}
	wait(runID)
	final, err := c.store.LoadCommitment(ctx, commitment.ID)
	if err != nil || final.State != "completed" {
		t.Fatalf("commitment outcome %+v %v", final, err)
	}
	t.Log("real-model save, cross-thread recall, correction, forgetting, night reflection and commitment closure passed")
}
