package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workspace"
)

func buildCreateFileTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("create_file", "Create a new workspace file. Fails if the target already exists.", func(ctx context.Context, input CreateFileInput, emit tooling.ToolProgressEmitter) (CreateFileOutput, error) {
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
			return CreateFileOutput{}, err
		}
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return CreateFileOutput{}, fmt.Errorf("prepare parent dir: %w", err)
		}
		if err := os.WriteFile(resolved, []byte(input.Content), 0o644); err != nil {
			return CreateFileOutput{}, fmt.Errorf("write file %s: %w", resolved, err)
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("wrote %s (%d bytes)", filepath.ToSlash(resolved), len(input.Content))); err != nil {
			return CreateFileOutput{}, err
		}
		body, err := os.ReadFile(resolved)
		if err != nil {
			return CreateFileOutput{}, fmt.Errorf("verify file %s: %w", resolved, err)
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
	tool, err := inferProgressTool("replace_span", "Replace an explicit inclusive line range within a workspace file.", func(ctx context.Context, input ReplaceSpanInput, emit tooling.ToolProgressEmitter) (ReplaceSpanOutput, error) {
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
			return ReplaceSpanOutput{}, err
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
			return ReplaceSpanOutput{}, fmt.Errorf("write file %s: %w", resolved, err)
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("replaced %s:%d-%d", filepath.ToSlash(resolved), startLine, endLine)); err != nil {
			return ReplaceSpanOutput{}, err
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

func buildApplyUnifiedPatchTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("apply_unified_patch", "Apply a unified git patch to explicit workspace paths.", func(ctx context.Context, input ApplyUnifiedPatchInput, emit tooling.ToolProgressEmitter) (ApplyUnifiedPatchOutput, error) {
		if strings.TrimSpace(input.Patch) == "" {
			return ApplyUnifiedPatchOutput{}, errors.New("patch is required")
		}
		if len(input.Paths) == 0 {
			return ApplyUnifiedPatchOutput{}, errors.New("paths are required")
		}
		normalizedPaths, err := normalizePatchPaths(ws, input.Paths)
		if err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		patchPaths := extractPatchPaths(input.Patch)
		if len(patchPaths) == 0 {
			return ApplyUnifiedPatchOutput{}, errors.New("patch does not declare any file paths")
		}
		declared := make(map[string]struct{}, len(normalizedPaths))
		for _, item := range normalizedPaths {
			declared[item] = struct{}{}
		}
		for _, item := range patchPaths {
			if _, ok := declared[item]; !ok {
				return ApplyUnifiedPatchOutput{}, fmt.Errorf("patch touches undeclared path %q", item)
			}
		}
		patchFile, err := os.CreateTemp(ws.StorageDir(), "acorn-patch-*.diff")
		if err != nil {
			return ApplyUnifiedPatchOutput{}, fmt.Errorf("create patch temp file: %w", err)
		}
		patchPath := patchFile.Name()
		if _, err := patchFile.WriteString(input.Patch); err != nil {
			patchFile.Close()
			_ = os.Remove(patchPath)
			return ApplyUnifiedPatchOutput{}, fmt.Errorf("write patch temp file: %w", err)
		}
		if err := patchFile.Close(); err != nil {
			_ = os.Remove(patchPath)
			return ApplyUnifiedPatchOutput{}, fmt.Errorf("close patch temp file: %w", err)
		}
		defer os.Remove(patchPath)

		if _, err := runGitCommand(ctx, ws.Root(), "apply", "--check", "--recount", patchPath); err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("patch check passed for %s", strings.Join(normalizedPaths, ", "))); err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		checkpoint, err := ws.CreateMutationCheckpoint(ctx, workspace.ToolApplyUnifiedPatch, normalizedPaths)
		if err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("checkpoint %s for %s", checkpoint.CheckpointID, strings.Join(normalizedPaths, ", "))); err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		if _, err := runGitCommand(ctx, ws.Root(), "apply", "--recount", patchPath); err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("applied patch to %s", strings.Join(normalizedPaths, ", "))); err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		statArgs := []string{"diff", "--stat", "--"}
		statArgs = append(statArgs, normalizedPaths...)
		diffStat, err := runGitCommand(ctx, ws.Root(), statArgs...)
		if err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		completed, err := ws.CompleteMutationCheckpoint(ctx, checkpoint.CheckpointID)
		if err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("completed checkpoint %s", completed.CheckpointID)); err != nil {
			return ApplyUnifiedPatchOutput{}, err
		}
		return ApplyUnifiedPatchOutput{
			Paths:            normalizedPaths,
			Message:          "ok",
			CheckpointID:     completed.CheckpointID,
			CheckpointPaths:  append([]string(nil), completed.Paths...),
			VerifiedDiffStat: strings.TrimSpace(diffStat),
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build apply_unified_patch tool: %w", err)
	}
	return tool, nil
}

func buildRollbackWorkspaceCheckpointTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("rollback_workspace_checkpoint", "Explicitly rollback a workspace mutation checkpoint. Fails if current workspace state conflicts with the checkpoint.", func(ctx context.Context, input RollbackWorkspaceCheckpointInput, emit tooling.ToolProgressEmitter) (RollbackWorkspaceCheckpointOutput, error) {
		if strings.TrimSpace(input.CheckpointID) == "" {
			return RollbackWorkspaceCheckpointOutput{}, errors.New("checkpoint_id is required")
		}
		result, err := ws.RollbackMutationCheckpoint(ctx, input.CheckpointID)
		if result == nil {
			if err != nil {
				return RollbackWorkspaceCheckpointOutput{}, err
			}
			return RollbackWorkspaceCheckpointOutput{}, errors.New("rollback result is nil")
		}
		output := RollbackWorkspaceCheckpointOutput{
			CheckpointID:  result.CheckpointID,
			RollbackID:    result.RollbackID,
			Status:        result.Status,
			RestoredPaths: append([]string(nil), result.RestoredPaths...),
			ConflictPaths: append([]string(nil), result.ConflictPaths...),
			Error:         result.Error,
			Message:       "ok",
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("rollback %s status %s", output.RollbackID, output.Status)); err != nil {
			return RollbackWorkspaceCheckpointOutput{}, err
		}
		if err != nil {
			if output.Error == "" {
				output.Error = err.Error()
			}
			output.Message = output.Error
			return output, err
		}
		return output, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build rollback_workspace_checkpoint tool: %w", err)
	}
	return tool, nil
}

func normalizePatchPaths(ws WorkspaceView, values []string) ([]string, error) {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		resolved, err := ws.ResolveWritePath(value)
		if err != nil {
			return nil, err
		}
		rel, err := ws.RelativePath(resolved)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		normalized = append(normalized, rel)
	}
	return normalized, nil
}

var patchPathRegexp = regexp.MustCompile(`(?m)^(?:\+\+\+|---)\s+(?:a/|b/)?([^\s]+)$`)

func extractPatchPaths(patch string) []string {
	matches := patchPathRegexp.FindAllStringSubmatch(patch, -1)
	if len(matches) == 0 {
		return nil
	}
	paths := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		candidate := strings.TrimSpace(match[1])
		if candidate == "" || candidate == "/dev/null" {
			continue
		}
		candidate = strings.TrimPrefix(candidate, "a/")
		candidate = strings.TrimPrefix(candidate, "b/")
		candidate = filepath.ToSlash(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		paths = append(paths, candidate)
	}
	return paths
}
