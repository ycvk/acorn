package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

// fakeVault records calls and returns canned notes.
type fakeVault struct {
	written  knowledge.WriteNote
	editArgs []string
	query    string
	prefix   string
	limit    int
	err      error
}

var knowledgeTestTime = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

func (v *fakeVault) Write(_ context.Context, n knowledge.WriteNote) (core.KnowledgeNote, error) {
	v.written = n
	return core.KnowledgeNote{Path: n.Path, Title: n.Title, UpdatedAt: knowledgeTestTime, Revision: 3}, v.err
}

func (v *fakeVault) Edit(_ context.Context, path, old, replacement, runID string) (core.KnowledgeNote, error) {
	v.editArgs = []string{path, old, replacement, runID}
	return core.KnowledgeNote{Path: path, Title: "T", UpdatedAt: knowledgeTestTime, Revision: 4}, v.err
}

func (v *fakeVault) Read(_ context.Context, path string) (core.KnowledgeNote, error) {
	if v.err != nil {
		return core.KnowledgeNote{}, v.err
	}
	return core.KnowledgeNote{Path: path, Title: "Tokio", Tags: []string{"rust"}, Source: "https://tokio.rs", CreatedAt: knowledgeTestTime, UpdatedAt: knowledgeTestTime, Body: "Async runtime."}, nil
}

func (v *fakeVault) Search(_ context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	v.query, v.limit = query, limit
	return []core.KnowledgeHit{{Path: "inbox/tokio.md", Title: "Tokio", Snippet: "…runtime…", UpdatedAt: knowledgeTestTime}}, v.err
}

func (v *fakeVault) Recent(_ context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	v.prefix, v.limit = prefix, limit
	return nil, v.err
}

func newKnowledgeToolsForTest(t *testing.T) (*fakeVault, map[string]einotool.BaseTool) {
	t.Helper()
	vault := &fakeVault{}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	deps := KnowledgeToolDeps{Vault: vault, Context: fixedArtifactContext{runID: "run_9", sessionID: "thread_1", callID: "call_1"}, Location: loc}
	tools := map[string]einotool.BaseTool{}
	for name, build := range map[string]func(KnowledgeToolDeps) (einotool.BaseTool, error){
		"knowledge_write":  buildKnowledgeWriteTool,
		"knowledge_edit":   buildKnowledgeEditTool,
		"knowledge_read":   buildKnowledgeReadTool,
		"knowledge_search": buildKnowledgeSearchTool,
		"knowledge_list":   buildKnowledgeListTool,
	} {
		tool, err := build(deps)
		if err != nil {
			t.Fatalf("build %s: %v", name, err)
		}
		tools[name] = tool
	}
	return vault, tools
}

func TestKnowledgeWriteAndEditCarryTheRun(t *testing.T) {
	vault, tools := newKnowledgeToolsForTest(t)
	out, err := runPresenceTool(t, tools["knowledge_write"], `{"path":"inbox/tokio.md","title":"Tokio","body":"Async runtime.","tags":["rust"],"source":"https://tokio.rs"}`)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if vault.written.RunID != "run_9" || vault.written.Source != "https://tokio.rs" || vault.written.Tags[0] != "rust" {
		t.Fatalf("written = %+v", vault.written)
	}
	if !strings.Contains(out, `"revision":3`) || !strings.Contains(out, "2026-10-03 Sat 09:00") {
		t.Fatalf("write output = %s", out)
	}
	if _, err := runPresenceTool(t, tools["knowledge_edit"], `{"path":"a.md","old":"x","new":"y"}`); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if fmt.Sprint(vault.editArgs) != "[a.md x y run_9]" {
		t.Fatalf("edit args = %v", vault.editArgs)
	}
}

func TestKnowledgeReadSearchAndList(t *testing.T) {
	vault, tools := newKnowledgeToolsForTest(t)
	out, err := runPresenceTool(t, tools["knowledge_read"], `{"path":"inbox/tokio.md"}`)
	if err != nil || !strings.Contains(out, `"body":"Async runtime."`) || !strings.Contains(out, `"source":"https://tokio.rs"`) {
		t.Fatalf("read = %s, %v", out, err)
	}
	out, err = runPresenceTool(t, tools["knowledge_search"], `{"query":" tokio "}`)
	if err != nil || vault.query != "tokio" || vault.limit != defaultKnowledgeLimit || !strings.Contains(out, `"path":"inbox/tokio.md"`) {
		t.Fatalf("search = %s, %v (query %q limit %d)", out, err, vault.query, vault.limit)
	}
	out, err = runPresenceTool(t, tools["knowledge_list"], `{"prefix":"inbox/","limit":5}`)
	if err != nil || vault.prefix != "inbox/" || vault.limit != 5 || out != `{"notes":[]}` {
		t.Fatalf("list = %s, %v", out, err)
	}
}

func TestKnowledgeToolErrors(t *testing.T) {
	vault, tools := newKnowledgeToolsForTest(t)
	if _, err := runPresenceTool(t, tools["knowledge_search"], `{"query":"  "}`); err == nil {
		t.Fatal("empty query must fail")
	}
	if _, err := runPresenceTool(t, tools["knowledge_list"], `{"limit":51}`); err == nil {
		t.Fatal("limit over 50 must fail")
	}
	vault.err = fmt.Errorf("%w: ../x.md", knowledge.ErrInvalidPath)
	if _, err := runPresenceTool(t, tools["knowledge_write"], `{"path":"../x.md","title":"x","body":"y"}`); !errors.Is(err, knowledge.ErrInvalidPath) {
		t.Fatalf("write err = %v", err)
	}
	vault.err = core.ErrKnowledgeNoteNotFound
	if _, err := runPresenceTool(t, tools["knowledge_read"], `{"path":"missing.md"}`); !errors.Is(err, core.ErrKnowledgeNoteNotFound) {
		t.Fatalf("read err = %v", err)
	}
}
