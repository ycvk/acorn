package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/workspace"
)

func buildCreateFileTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("create_file", "Create a new workspace file. Fails if the target already exists.", func(ctx context.Context, input CreateFileInput, emit ToolProgressEmitter) (CreateFileOutput, error) {
		if strings.TrimSpace(input.Path) == "" {
			return CreateFileOutput{}, errors.New("path is required")
		}
		resolved, err := ws.ResolveWritePath(input.Path)
		if err != nil {
			return CreateFileOutput{}, err
		}
		if _, err := os.Stat(resolved); err == nil {
			return CreateFileOutput{}, fmt.Errorf("create_file target already exists: %s", resolved)
		} else if !errors.Is(err, os.ErrNotExist) {
			return CreateFileOutput{}, fmt.Errorf("stat target %s: %w", resolved, err)
		}
		checkpoint, err := ws.CreateMutationCheckpoint(ctx, workspace.ToolCreateFile, []string{input.Path})
		if err != nil {
			return CreateFileOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("checkpoint %s for %s", checkpoint.CheckpointID, filepath.ToSlash(input.Path))); err != nil {
			return CreateFileOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, err)
		}
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return CreateFileOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, fmt.Errorf("prepare parent dir: %w", err))
		}
		if err := os.WriteFile(resolved, []byte(input.Content), 0o644); err != nil {
			return CreateFileOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, fmt.Errorf("write file %s: %w", resolved, err))
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("wrote %s (%d bytes)", filepath.ToSlash(resolved), len(input.Content))); err != nil {
			return CreateFileOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, err)
		}
		body, err := os.ReadFile(resolved)
		if err != nil {
			return CreateFileOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, fmt.Errorf("verify file %s: %w", resolved, err))
		}
		completed, err := ws.CompleteMutationCheckpoint(ctx, checkpoint.CheckpointID)
		if err != nil {
			return CreateFileOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("completed checkpoint %s", completed.CheckpointID)); err != nil {
			return CreateFileOutput{}, err
		}
		verifiedContent, truncated := previewBytes(body, defaultVerificationPreviewBytes)
		return CreateFileOutput{
			Path:                  resolved,
			Bytes:                 len(input.Content),
			Message:               "ok",
			CheckpointID:          completed.CheckpointID,
			CheckpointPaths:       append([]string(nil), completed.Paths...),
			VerifiedBytes:         len(body),
			VerifiedContent:       verifiedContent,
			VerificationTruncated: truncated,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build create_file tool: %w", err)
	}
	return tool, nil
}

func buildReplaceSpanTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("replace_span", "Replace an explicit inclusive line range within a workspace file.", func(ctx context.Context, input ReplaceSpanInput, emit ToolProgressEmitter) (ReplaceSpanOutput, error) {
		if strings.TrimSpace(input.Path) == "" {
			return ReplaceSpanOutput{}, errors.New("path is required")
		}
		if input.StartLine <= 0 || input.EndLine <= 0 {
			return ReplaceSpanOutput{}, errors.New("start_line and end_line must be > 0")
		}
		resolved, err := ws.ResolveWritePath(input.Path)
		if err != nil {
			return ReplaceSpanOutput{}, err
		}
		body, err := os.ReadFile(resolved)
		if err != nil {
			return ReplaceSpanOutput{}, fmt.Errorf("read file %s: %w", resolved, err)
		}
		offsets := lineStartOffsets(body)
		totalLines := len(offsets)
		startLine, endLine, err := normalizeLineRange(totalLines, input.StartLine, input.EndLine)
		if err != nil {
			return ReplaceSpanOutput{}, err
		}
		checkpoint, err := ws.CreateMutationCheckpoint(ctx, workspace.ToolReplaceSpan, []string{input.Path})
		if err != nil {
			return ReplaceSpanOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("checkpoint %s for %s:%d-%d", checkpoint.CheckpointID, filepath.ToSlash(input.Path), startLine, endLine)); err != nil {
			return ReplaceSpanOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, err)
		}
		startByte := offsets[startLine-1]
		endByte := len(body)
		if endLine < totalLines {
			endByte = offsets[endLine]
		}
		replaced := append([]byte(nil), body[:startByte]...)
		replaced = append(replaced, []byte(input.Replacement)...)
		replaced = append(replaced, body[endByte:]...)
		if err := os.WriteFile(resolved, replaced, 0o644); err != nil {
			return ReplaceSpanOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, fmt.Errorf("write file %s: %w", resolved, err))
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("replaced %s:%d-%d", filepath.ToSlash(resolved), startLine, endLine)); err != nil {
			return ReplaceSpanOutput{}, rollbackCheckpoint(ctx, ws, checkpoint.CheckpointID, err)
		}
		completed, err := ws.CompleteMutationCheckpoint(ctx, checkpoint.CheckpointID)
		if err != nil {
			return ReplaceSpanOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("completed checkpoint %s", completed.CheckpointID)); err != nil {
			return ReplaceSpanOutput{}, err
		}
		verifiedContent, truncated := previewBytes(replaced, defaultVerificationPreviewBytes)
		return ReplaceSpanOutput{
			Path:                  resolved,
			StartLine:             startLine,
			EndLine:               endLine,
			Bytes:                 len(replaced),
			Message:               "ok",
			CheckpointID:          completed.CheckpointID,
			CheckpointPaths:       append([]string(nil), completed.Paths...),
			VerifiedBytes:         len(replaced),
			VerifiedContent:       verifiedContent,
			VerificationTruncated: truncated,
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build replace_span tool: %w", err)
	}
	return tool, nil
}

// rollbackCheckpoint rolls back a mutation checkpoint after a failed write,
// joining any rollback error with the original failure so neither is lost.
func rollbackCheckpoint(ctx context.Context, ws WorkspaceView, checkpointID string, cause error) error {
	if _, rollbackErr := ws.RollbackMutationCheckpoint(ctx, checkpointID); rollbackErr != nil {
		return errors.Join(cause, fmt.Errorf("rollback checkpoint %s: %w", checkpointID, rollbackErr))
	}
	return cause
}
