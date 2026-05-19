package runtime

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestToolSchemaCacheNewIsEmpty(t *testing.T) {
	c := NewToolSchemaCache()
	if len(c.lastHashes) != 0 {
		t.Fatalf("expected empty hashes, got %d", len(c.lastHashes))
	}
}

func TestToolSchemaCacheDetectsChange(t *testing.T) {
	c := NewToolSchemaCache()
	c.UpdateHash("tool_a", `{"type":"object","properties":{"x":{"type":"string"}}}`)
	if !c.HasChanged("tool_a", `{"type":"object","properties":{"x":{"type":"integer"}}}`) {
		t.Fatal("expected schema change to be detected")
	}
}

func TestToolSchemaCacheNoChange(t *testing.T) {
	c := NewToolSchemaCache()
	schema := `{"type":"object","properties":{"x":{"type":"string"}}}`
	c.UpdateHash("tool_a", schema)
	if c.HasChanged("tool_a", schema) {
		t.Fatal("expected no change detected for identical schema")
	}
}

type mockTool struct {
	info *schema.ToolInfo
}

func (m *mockTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return m.info, nil
}

func TestToolSchemaCacheAnyChanged(t *testing.T) {
	c := NewToolSchemaCache()

	tools := []einotool.BaseTool{
		&mockTool{info: &schema.ToolInfo{Name: "t1", Desc: "tool 1"}},
		&mockTool{info: &schema.ToolInfo{Name: "t2", Desc: "tool 2"}},
	}

	if !c.AnyChanged(context.Background(), tools) {
		t.Fatal("expected AnyChanged=true on first call (no prior hashes)")
	}

	if c.AnyChanged(context.Background(), tools) {
		t.Fatal("expected AnyChanged=false on second call (hashes match)")
	}

	changedTools := []einotool.BaseTool{
		&mockTool{info: &schema.ToolInfo{Name: "t1", Desc: "tool 1 updated"}},
		&mockTool{info: &schema.ToolInfo{Name: "t2", Desc: "tool 2"}},
	}
	if !c.AnyChanged(context.Background(), changedTools) {
		t.Fatal("expected AnyChanged=true when tool description changes")
	}
}
