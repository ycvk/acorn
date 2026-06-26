package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	mutationCheckpointDirName = "workspace-checkpoints"
	MutationCheckpointEffect  = "workspace_checkpoint"
	MutationRollbackEffect    = "workspace_rollback"
)

type WorkspaceMutationCheckpoint struct {
	CheckpointID       string               `json:"checkpoint_id"`
	ToolName           string               `json:"tool_name"`
	Paths              []string             `json:"paths"`
	BaselineDirtyPaths []string             `json:"baseline_dirty_paths,omitempty"`
	BaselineDirtyState []WorkspaceFileState `json:"baseline_dirty_state,omitempty"`
	Before             []WorkspaceFileState `json:"before"`
	After              []WorkspaceFileState `json:"after,omitempty"`
	DiffStat           string               `json:"diff_stat,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	CompletedAt        time.Time            `json:"completed_at,omitempty"`
}

type WorkspaceFileState struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	SHA256 string `json:"sha256,omitempty"`
	Bytes  []byte `json:"bytes,omitempty"`
}

type WorkspaceRollbackResult struct {
	CheckpointID  string    `json:"checkpoint_id"`
	RollbackID    string    `json:"rollback_id"`
	Status        string    `json:"status"`
	RestoredPaths []string  `json:"restored_paths,omitempty"`
	ConflictPaths []string  `json:"conflict_paths,omitempty"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (w *Workspace) CreateMutationCheckpoint(ctx context.Context, toolName string, paths []string) (*WorkspaceMutationCheckpoint, error) {
	if w == nil {
		return nil, errors.New("workspace is required")
	}
	normalized, err := w.normalizeCheckpointPaths(paths)
	if err != nil {
		return nil, err
	}
	baselineDirty, err := w.inspectDirtyFileStates(ctx)
	if err != nil {
		return nil, err
	}
	before, err := w.fileStates(normalized)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	checkpoint := &WorkspaceMutationCheckpoint{
		CheckpointID:       fmt.Sprintf("workspace_checkpoint_%d", now.UnixNano()),
		ToolName:           strings.TrimSpace(toolName),
		Paths:              normalized,
		BaselineDirtyPaths: statePaths(baselineDirty),
		BaselineDirtyState: baselineDirty,
		Before:             before,
		CreatedAt:          now,
	}
	if checkpoint.ToolName == "" {
		return nil, errors.New("checkpoint tool name is required")
	}
	if err := w.saveMutationCheckpoint(checkpoint); err != nil {
		return nil, err
	}
	return checkpoint, nil
}

func (w *Workspace) CompleteMutationCheckpoint(ctx context.Context, checkpointID string) (*WorkspaceMutationCheckpoint, error) {
	checkpoint, err := w.LoadMutationCheckpoint(checkpointID)
	if err != nil {
		return nil, err
	}
	after, err := w.fileStates(checkpoint.Paths)
	if err != nil {
		return nil, err
	}
	checkpoint.After = after
	checkpoint.CompletedAt = time.Now().UTC()
	diffStat, err := w.gitDiffStat(ctx, checkpoint.Paths)
	if err != nil {
		return nil, err
	}
	checkpoint.DiffStat = strings.TrimSpace(diffStat)
	if err := w.saveMutationCheckpoint(checkpoint); err != nil {
		return nil, err
	}
	return checkpoint, nil
}

func (w *Workspace) LoadMutationCheckpoint(checkpointID string) (*WorkspaceMutationCheckpoint, error) {
	if w == nil {
		return nil, errors.New("workspace is required")
	}
	path, err := w.mutationCheckpointPath(checkpointID)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("workspace mutation checkpoint %q not found", strings.TrimSpace(checkpointID))
		}
		return nil, fmt.Errorf("read workspace mutation checkpoint %s: %w", path, err)
	}
	var checkpoint WorkspaceMutationCheckpoint
	if err := json.Unmarshal(body, &checkpoint); err != nil {
		return nil, fmt.Errorf("decode workspace mutation checkpoint %s: %w", path, err)
	}
	if strings.TrimSpace(checkpoint.CheckpointID) == "" {
		return nil, fmt.Errorf("workspace mutation checkpoint %s is missing checkpoint_id", path)
	}
	if len(checkpoint.Paths) == 0 {
		return nil, fmt.Errorf("workspace mutation checkpoint %s is missing paths", path)
	}
	if strings.TrimSpace(checkpoint.ToolName) == "" {
		return nil, fmt.Errorf("workspace mutation checkpoint %s is missing tool_name", path)
	}
	if len(checkpoint.Before) == 0 {
		return nil, fmt.Errorf("workspace mutation checkpoint %s is missing before state", path)
	}
	return &checkpoint, nil
}

func (w *Workspace) RollbackMutationCheckpoint(ctx context.Context, checkpointID string) (*WorkspaceRollbackResult, error) {
	checkpoint, err := w.LoadMutationCheckpoint(checkpointID)
	if err != nil {
		return nil, err
	}
	result := &WorkspaceRollbackResult{
		CheckpointID: checkpoint.CheckpointID,
		RollbackID:   fmt.Sprintf("workspace_rollback_%d", time.Now().UTC().UnixNano()),
		Status:       "failed",
		CreatedAt:    time.Now().UTC(),
	}
	conflicts, err := w.rollbackConflicts(ctx, checkpoint)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if len(conflicts) > 0 {
		result.ConflictPaths = conflicts
		result.Error = "workspace rollback conflict"
		return result, fmt.Errorf("workspace rollback conflict: %s", strings.Join(conflicts, ", "))
	}
	if err := w.restoreFileStates(checkpoint.Before); err != nil {
		result.Error = err.Error()
		return result, err
	}
	result.Status = "succeeded"
	result.RestoredPaths = append([]string(nil), checkpoint.Paths...)
	return result, nil
}

func (w *Workspace) normalizeCheckpointPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("checkpoint paths are required")
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		resolved, err := w.ResolveWritePath(path)
		if err != nil {
			return nil, err
		}
		rel, err := w.RelativePath(resolved)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	slices.Sort(out)
	return out, nil
}

func (w *Workspace) inspectDirtyFileStates(ctx context.Context) ([]WorkspaceFileState, error) {
	status, err := w.InspectGitStatus(ctx, "")
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(status.Entries))
	seen := make(map[string]struct{}, len(status.Entries))
	for _, entry := range status.Entries {
		path := filepath.ToSlash(strings.TrimSpace(entry.Path))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return w.fileStates(paths)
}

func (w *Workspace) fileStates(paths []string) ([]WorkspaceFileState, error) {
	states := make([]WorkspaceFileState, 0, len(paths))
	for _, path := range paths {
		state, err := w.fileState(path)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

func (w *Workspace) fileState(path string) (WorkspaceFileState, error) {
	resolved, err := w.ResolveReadPath(path)
	if err != nil {
		resolved, err = w.ResolveWritePath(path)
		if err != nil {
			return WorkspaceFileState{}, err
		}
	}
	rel, err := w.RelativePath(resolved)
	if err != nil {
		return WorkspaceFileState{}, err
	}
	state := WorkspaceFileState{Path: filepath.ToSlash(rel)}
	body, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return WorkspaceFileState{}, fmt.Errorf("read checkpoint file %s: %w", resolved, err)
	}
	sum := sha256.Sum256(body)
	state.Exists = true
	state.SHA256 = hex.EncodeToString(sum[:])
	state.Bytes = body
	return state, nil
}

func (w *Workspace) restoreFileStates(states []WorkspaceFileState) error {
	for _, state := range states {
		resolved, err := w.ResolveWritePath(state.Path)
		if err != nil {
			return err
		}
		if !state.Exists {
			if err := os.Remove(resolved); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove rollback-created file %s: %w", state.Path, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return fmt.Errorf("prepare rollback parent dir %s: %w", state.Path, err)
		}
		if err := os.WriteFile(resolved, state.Bytes, 0o644); err != nil {
			return fmt.Errorf("restore rollback file %s: %w", state.Path, err)
		}
	}
	return nil
}

func (w *Workspace) rollbackConflicts(ctx context.Context, checkpoint *WorkspaceMutationCheckpoint) ([]string, error) {
	currentDirty, err := w.inspectDirtyFileStates(ctx)
	if err != nil {
		return nil, err
	}
	checkpointPaths := stateSetFromPaths(checkpoint.Paths)
	baseline := stateMap(checkpoint.BaselineDirtyState)
	after := stateMap(checkpoint.After)
	conflicts := make([]string, 0)
	for _, state := range currentDirty {
		if _, ok := checkpointPaths[state.Path]; ok {
			continue
		}
		base, ok := baseline[state.Path]
		if !ok || !sameFileState(base, state) {
			conflicts = append(conflicts, state.Path)
		}
	}
	for _, path := range checkpoint.Paths {
		if len(after) == 0 {
			continue
		}
		current, err := w.fileState(path)
		if err != nil {
			return nil, err
		}
		expected, ok := after[path]
		if !ok || !sameFileState(expected, current) {
			conflicts = append(conflicts, path)
		}
	}
	slices.Sort(conflicts)
	return slices.Compact(conflicts), nil
}

func (w *Workspace) gitDiffStat(ctx context.Context, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	args := []string{"diff", "--stat", "--"}
	args = append(args, paths...)
	return w.gitOutput(ctx, args...)
}

func (w *Workspace) saveMutationCheckpoint(checkpoint *WorkspaceMutationCheckpoint) error {
	if checkpoint == nil {
		return errors.New("workspace mutation checkpoint is nil")
	}
	path, err := w.mutationCheckpointPath(checkpoint.CheckpointID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("prepare workspace mutation checkpoint dir: %w", err)
	}
	body, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace mutation checkpoint: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write workspace mutation checkpoint %s: %w", path, err)
	}
	return nil
}

func (w *Workspace) mutationCheckpointPath(checkpointID string) (string, error) {
	id := strings.TrimSpace(checkpointID)
	if id == "" {
		return "", errors.New("workspace mutation checkpoint id is required")
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", fmt.Errorf("workspace mutation checkpoint id %q contains invalid character %q", id, r)
	}
	storageDir := strings.TrimSpace(w.StorageDir())
	if storageDir == "" || storageDir == "." {
		return "", errors.New("workspace storage dir is required for mutation checkpoints")
	}
	return filepath.Join(storageDir, mutationCheckpointDirName, id+".json"), nil
}

func statePaths(items []WorkspaceFileState) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Path) != "" {
			paths = append(paths, item.Path)
		}
	}
	slices.Sort(paths)
	return paths
}

func stateMap(items []WorkspaceFileState) map[string]WorkspaceFileState {
	out := make(map[string]WorkspaceFileState, len(items))
	for _, item := range items {
		out[item.Path] = item
	}
	return out
}

func stateSetFromPaths(paths []string) map[string]struct{} {
	out := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		out[filepath.ToSlash(strings.TrimSpace(path))] = struct{}{}
	}
	return out
}

func sameFileState(a, b WorkspaceFileState) bool {
	return a.Path == b.Path && a.Exists == b.Exists && a.SHA256 == b.SHA256
}
