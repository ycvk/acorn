package memorymodule

import (
	"context"
	"fmt"
	"strings"
)

func (s *LocalService) BuildMemoryInstruction(ctx context.Context, workspaceSlug string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("memory service is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	workspace := strings.TrimSpace(workspaceSlug)
	if workspace == "" {
		workspace = "current workspace"
	}
	instruction := strings.TrimSpace(fmt.Sprintf(`
Before your final answer, check whether this run produced stable memory.

Memory root: %s
Workspace: %s

	Use memory_search to retrieve relevant memory records through semantic search.
	Use remember to store a new durable fact: pass a title, text, and optional tags. Acorn generates the record metadata and timestamps for you, so do not hand-write frontmatter or dates.
	Use memory_read_file to inspect a memory file when you need the full content.
	Use memory_replace_span or memory_create_file for advanced edits (precise patches, retiring a record, or non-fact files); both validate a mutation plan before writing.
	If you learned a stable fact, remember it.
	If nothing is worth saving, leave memory unchanged.
	Do not rewrite an entire facts file when a targeted patch is enough.
	`, s.root, workspace))
	return instruction, nil
}
