package memorymodule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const memoryMutationVerificationPreviewBytes = 4000

type memoryMutationRollback struct {
	path   string
	exists bool
	body   []byte
}

func (s *LocalService) ApplyMemoryMutation(ctx context.Context, req PlanMemoryMutationRequest) (*MemoryMutationResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	plan, err := s.PlanMemoryMutation(ctx, req)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("memory mutation plan is nil")
	}
	if plan.Action == MemoryMutationRejectInvalid {
		return &MemoryMutationResult{
			Message:      string(MemoryMutationRejectInvalid),
			MutationPlan: plan,
			Path:         plan.Path,
		}, nil
	}
	if plan.Action == MemoryMutationNoopDuplicate {
		return &MemoryMutationResult{
			Message:      string(MemoryMutationNoopDuplicate),
			MutationPlan: plan,
			Path:         plan.Path,
		}, nil
	}

	relPath, _, err := normalizeMemoryMutationPath(plan.Path)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.root, filepath.FromSlash(relPath))
	rollback, err := captureMemoryMutationRollback(path)
	if err != nil {
		return nil, err
	}
	if err := writeMemoryMutationFile(path, plan.Action, req.Content); err != nil {
		return nil, err
	}

	// Index rebuild keeps the in-memory index current; semantic (vector) index
	// is rebuilt lazily on next search via content-hash skipping, so no eager
	// rebuild is needed here.
	if err := s.BuildIndex(ctx); err != nil {
		return nil, s.rollbackAppliedMemoryMutation(ctx, rollback, fmt.Errorf("build index after memory mutation: %w", err))
	}

	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("verify memory mutation file %s: %w", path, err)
	}
	verifiedContent, truncated := previewMemoryMutationBytes(body)
	return &MemoryMutationResult{
		Message:               "ok",
		MutationPlan:          plan,
		Path:                  relPath,
		Bytes:                 len([]byte(req.Content)),
		VerifiedBytes:         len(body),
		VerifiedContent:       verifiedContent,
		VerificationTruncated: truncated,
	}, nil
}

func captureMemoryMutationRollback(path string) (*memoryMutationRollback, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &memoryMutationRollback{path: path}, nil
		}
		return nil, fmt.Errorf("read memory mutation rollback target %s: %w", path, err)
	}
	return &memoryMutationRollback{path: path, exists: true, body: body}, nil
}

// applyNewMemoryRecord writes a brand-new memory record at relPath through the
// full mutation pipeline (ApplyMemoryMutation), so structured writers (CreateFact,
// CreateProcedure) get the same atomic write + BuildIndex + semantic rebuild +
// rollback-on-failure as the raw memory toolset. Without this, a structured write
// would land on disk and in the in-memory index but leave the semantic index
// stale, making the just-written record unfindable by memory_search/Prepare. It
// accepts a planner noop_duplicate for an equivalent existing record, but still
// rejects replace/retire actions so structured writers cannot silently mutate an
// existing record's durable content.
func (s *LocalService) applyNewMemoryRecord(ctx context.Context, relPath string, content string, kind Kind) (*Record, *MemoryMutationPlan, error) {
	absPath := filepath.Join(s.root, filepath.FromSlash(relPath))
	result, err := s.ApplyMemoryMutation(ctx, PlanMemoryMutationRequest{Path: relPath, Content: content})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || result.MutationPlan == nil {
		return nil, nil, fmt.Errorf("memory record mutation returned nil plan")
	}
	switch result.MutationPlan.Action {
	case MemoryMutationCreate, MemoryMutationNoopDuplicate:
	default:
		action, reason := MemoryMutationAction(""), ""
		action, reason = result.MutationPlan.Action, result.MutationPlan.Reason
		return nil, nil, fmt.Errorf("memory record mutation %q: %s", action, reason)
	}
	record, err := readMemoryRecord(s.root, kind, absPath)
	if err != nil {
		return nil, nil, err
	}
	return record, result.MutationPlan, nil
}

func writeMemoryMutationFile(path string, action MemoryMutationAction, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare memory mutation parent dir: %w", err)
	}
	switch action {
	case MemoryMutationCreate:
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create memory file %s: %w", path, err)
		}
		if _, err := file.WriteString(content); err != nil {
			closeErr := file.Close()
			if closeErr != nil {
				return fmt.Errorf("write memory file %s: %w; close failed: %v", path, err, closeErr)
			}
			return fmt.Errorf("write memory file %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close memory file %s: %w", path, err)
		}
		return nil
	case MemoryMutationReplaceExisting, MemoryMutationRetireExisting:
		return atomicWriteMemoryFile(path, []byte(content))
	default:
		return fmt.Errorf("cannot apply memory mutation action %q", action)
	}
}

func atomicWriteMemoryFile(path string, body []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp memory file for %s: %w", path, err)
	}
	tmpPath := file.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := file.Write(body); err != nil {
		closeErr := file.Close()
		if closeErr != nil {
			return fmt.Errorf("write temp memory file %s: %w; close failed: %v", tmpPath, err, closeErr)
		}
		return fmt.Errorf("write temp memory file %s: %w", tmpPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temp memory file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace memory file %s: %w", path, err)
	}
	removeTemp = false
	return nil
}

func (s *LocalService) rollbackAppliedMemoryMutation(ctx context.Context, rollback *memoryMutationRollback, cause error) error {
	if rollback == nil {
		return cause
	}
	var rollbackErr error
	if rollback.exists {
		rollbackErr = atomicWriteMemoryFile(rollback.path, rollback.body)
	} else {
		err := os.Remove(rollback.path)
		if err != nil && !os.IsNotExist(err) {
			rollbackErr = fmt.Errorf("remove created memory file %s: %w", rollback.path, err)
		}
	}
	if rollbackErr == nil {
		if err := s.BuildIndex(ctx); err != nil {
			rollbackErr = fmt.Errorf("rebuild memory index after rollback: %w", err)
		}
	}
	if rollbackErr != nil {
		return errors.Join(cause, fmt.Errorf("rollback memory mutation: %w", rollbackErr))
	}
	return cause
}

func previewMemoryMutationBytes(body []byte) (string, bool) {
	if len(body) <= memoryMutationVerificationPreviewBytes {
		return string(body), false
	}
	preview := string(body[:memoryMutationVerificationPreviewBytes])
	return strings.TrimRight(preview, "\n") + "\n...[truncated]", true
}
