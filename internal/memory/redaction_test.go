package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// Credentials in a shell result and an owner message reach extraction, record
// evidence, the keyword index and recall only as placeholders, while the thread
// keeps them for the conversation itself.
func TestExtractionNeverSeesOrStoresCredentials(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	model := &protocolModel{}
	engine, db := testEngineAt(t, dir, model, 100000)
	const apiKey = "sk-proj-Fx7Qm2Lp9Rt4Vw8Yz1Bc3Dn5Gh6Jk0Ms"
	const code = "482913"
	if _, err := db.CreateSession(ctx, "ops", "运维"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateRun(ctx, core.RunCreateParams{RunID: "run_ops", SessionID: "ops", TurnIndex: 1, Input: "登录"}); err != nil {
		t.Fatal(err)
	}
	msg, err := db.AppendSessionMessage(ctx, "ops", 1, "user", "银行发来的验证码是 "+code+"，我在用招商银行的网银", "run_ops")
	if err != nil {
		t.Fatal(err)
	}
	event, err := db.AppendEvent(ctx, "run_ops", "tool.call.succeeded", map[string]any{"tool_name": "shell", "output": "$ cat ~/.config/llm.env\nOPENAI_API_KEY=" + apiKey + "\nmodel=gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	drainMemory(t, engine)

	raw := []string{apiKey, code}
	for _, input := range model.seen {
		for _, secret := range raw {
			if strings.Contains(input, secret) {
				t.Fatalf("memory model saw %q in %s", secret, input)
			}
		}
	}
	conn, err := sql.Open("sqlite", filepath.Join(dir, "acorn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	}()
	var records int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_records`).Scan(&records); err != nil || records != 2 {
		t.Fatalf("records=%d %v", records, err)
	}
	for _, secret := range raw {
		like := "%" + secret + "%"
		var stored int
		if err := conn.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM memory_records WHERE content LIKE ?1 OR scope LIKE ?1)
 +(SELECT COUNT(*) FROM memory_revisions WHERE content LIKE ?1 OR scope LIKE ?1)
 +(SELECT COUNT(*) FROM memory_revision_evidence WHERE quote LIKE ?1)
 +(SELECT COUNT(*) FROM memory_revision_entities WHERE entity LIKE ?1)
 +(SELECT COUNT(*) FROM memory_records_fts WHERE memory_records_fts MATCH ?2)`, like, `"`+secret+`"`).Scan(&stored); err != nil || stored != 0 {
			t.Fatalf("%q stored %d times: %v", secret, stored, err)
		}
		found, err := db.SearchMemoryText(ctx, core.MemoryQuery{Query: secret, Limit: 10})
		if err != nil || len(found) != 0 {
			t.Fatalf("keyword search for %q=%+v %v", secret, found, err)
		}
	}

	for query, placeholder := range map[string]string{"招商银行 验证码": "[REDACTED:otp]", "OPENAI_API_KEY": "[REDACTED:openai_key]"} {
		recall, err := engine.Recall(ctx, core.MemoryQuery{Query: query, Mode: "current"})
		if err != nil || len(recall.Hits) == 0 {
			t.Fatalf("recall %q=%+v %v", query, recall, err)
		}
		data, err := json.Marshal(recall)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), placeholder) || strings.Contains(string(data), apiKey) || strings.Contains(string(data), code) {
			t.Fatalf("recall %q=%s", query, data)
		}
		read, err := db.ReadMemory(ctx, recall.Hits[0].Record.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(read)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), placeholder) || strings.Contains(string(data), apiKey) || strings.Contains(string(data), code) {
			t.Fatalf("memory_read %q=%s", query, data)
		}
	}

	thread, _, err := db.MemoryMessages(ctx, "ops", 0, 0, false, 10)
	if err != nil || len(thread) != 1 || thread[0].Content != msg.Content {
		t.Fatalf("thread=%+v %v", thread, err)
	}
	tool, err := db.LoadThreadSource(ctx, fmt.Sprintf("event:%d", event.Sequence))
	if err != nil || !strings.Contains(tool.Content, apiKey) {
		t.Fatalf("thread tool source=%+v %v", tool, err)
	}
}
