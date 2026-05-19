package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ToolSchemaCache detects when tool schemas change between runs.
type ToolSchemaCache struct {
	lastHashes map[string]string
	mu         sync.RWMutex
}

func NewToolSchemaCache() *ToolSchemaCache {
	return &ToolSchemaCache{
		lastHashes: make(map[string]string),
	}
}

// ComputeHash returns a SHA-256 hash of the tool name + schema JSON,
// truncated to 16 hex characters.
func (c *ToolSchemaCache) ComputeHash(toolName, schemaJSON string) string {
	h := sha256.Sum256([]byte(toolName + ":" + schemaJSON))
	return hex.EncodeToString(h[:])[:16]
}

// HasChanged returns true if the current schema hash differs from the
// last recorded hash for this tool.
func (c *ToolSchemaCache) HasChanged(toolName, currentSchemaJSON string) bool {
	current := c.ComputeHash(toolName, currentSchemaJSON)
	c.mu.RLock()
	last, ok := c.lastHashes[toolName]
	c.mu.RUnlock()
	return !ok || last != current
}

// UpdateHash records the current schema hash for the given tool.
func (c *ToolSchemaCache) UpdateHash(toolName, schemaJSON string) {
	h := c.ComputeHash(toolName, schemaJSON)
	c.mu.Lock()
	c.lastHashes[toolName] = h
	c.mu.Unlock()
}

// AnyChanged checks whether any tool in the list has a schema that
// differs from the last recorded hash. It also updates hashes for
// all tools so subsequent calls reflect the current state.
func (c *ToolSchemaCache) AnyChanged(ctx context.Context, tools []einotool.BaseTool) bool {
	anyChanged := false
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			anyChanged = true
			continue
		}
		schemaJSON := toolSchemaJSON(info)
		if c.HasChanged(info.Name, schemaJSON) {
			anyChanged = true
		}
		c.UpdateHash(info.Name, schemaJSON)
	}
	return anyChanged
}

// toolSchemaJSON serializes a ToolInfo's parameter schema to JSON.
// Returns empty string if the tool has no parameters.
func toolSchemaJSON(info *schema.ToolInfo) string {
	if info == nil {
		return ""
	}
	payload := map[string]any{
		"name": info.Name,
		"desc": info.Desc,
	}
	if info.ParamsOneOf != nil {
		schema, err := info.ToJSONSchema()
		if err != nil {
			return info.Name + ":" + info.Desc
		}
		payload["params"] = schema
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return info.Name + ":" + info.Desc
	}
	return string(data)
}
