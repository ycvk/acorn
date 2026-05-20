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
		Use memory_read_file to inspect a memory file when you need the full content.
		Use memory_replace_span or memory_create_file to update memory; both validate a mutation plan before writing and refresh retrieval after successful writes.
	If you learned a stable fact, update the matching facts file.
	If you learned a reusable procedure, create or patch a procedure skill file.
	If nothing is worth saving, leave memory unchanged.
	New agent-written facts must use status: unverified.
	New agent-written procedure skills must use origin: agent_draft, status: unverified, and source_run for the current run.
	Only action-verified procedure skills may use origin: action_verified, and they must include evidence_refs.
	Do not rewrite an entire facts file when a targeted patch is enough.
	`, s.root, workspace))
	return instruction, nil
}
