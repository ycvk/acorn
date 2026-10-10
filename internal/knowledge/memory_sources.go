package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type CommitVersion struct {
	ID    string
	RunID string
	At    time.Time
	Paths []string
}

// SourceReader reads exactly the Git object named by a memory source.
type SourceReader struct {
	Git Git
	Dir string
}

func (r *SourceReader) ReadMemorySource(ctx context.Context, source core.MemorySource) (string, error) {
	if r.Git == nil || r.Dir == "" {
		return "", errors.New("knowledge source requires git and dir")
	}
	return r.Git.ReadVersion(ctx, r.Dir, source.Version, source.ObjectID)
}

func knowledgeSourceID(path, version string) string {
	sum := sha256.Sum256([]byte(path + "\x00" + version))
	return "knowledge:" + hex.EncodeToString(sum[:16])
}

// SyncMemory registers a bounded prefix of committed versions atomically with
// its cursor. The worker calls it again to consume the remaining history.
func (v *Vault) SyncMemory(ctx context.Context, memories core.MemoryStore) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.syncMemoryLocked(ctx, memories)
}

func (v *Vault) syncMemoryLocked(ctx context.Context, memories core.MemoryStore) error {
	cursor, err := memories.MemorySourceCursor(ctx, "knowledge")
	if err != nil {
		return err
	}
	commits, err := v.git.CommitVersions(ctx, v.dir, cursor, 32)
	if err != nil {
		return err
	}
	for _, commit := range commits {
		var sources []core.MemorySource
		for _, path := range commit.Paths {
			if !strings.HasSuffix(path, ".md") || strings.HasPrefix(path, attachmentsDir+"/") {
				continue
			}
			if _, err := CleanNotePath(path); err != nil {
				return err
			}
			speaker := "external"
			if commit.RunID != "" {
				speaker = "assistant"
			}
			sources = append(sources, core.MemorySource{ID: knowledgeSourceID(path, commit.ID), Kind: "knowledge", ObjectID: path, Version: commit.ID, Speaker: speaker, RunID: commit.RunID, RecordedAt: v.clock(), OccurredAt: commit.At})
		}
		if err := memories.AdvanceMemorySourceCursor(ctx, "knowledge", cursor, commit.ID, sources); err != nil {
			return err
		}
		cursor = commit.ID
	}
	return nil
}

func validCommit(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (g *ExecGit) ReadVersion(ctx context.Context, dir, version, path string) (string, error) {
	if !validCommit(version) {
		return "", errors.New("knowledge version must be a full commit hash")
	}
	if _, err := CleanNotePath(path); err != nil {
		return "", err
	}
	return g.run(ctx, dir, "show", version+":"+path)
}

func (g *ExecGit) CommitVersions(ctx context.Context, dir, after string, limit int) ([]CommitVersion, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("knowledge commit limit must be 1..100")
	}
	if after != "" && !validCommit(after) {
		return nil, errors.New("knowledge cursor must be a full commit hash")
	}
	// An empty repository has no refs, and therefore no sources.
	refs, err := g.run(ctx, dir, "for-each-ref", "--format=%(objectname)", "refs/heads")
	if err != nil || strings.TrimSpace(refs) == "" {
		return nil, err
	}
	args := []string{"rev-list", "--reverse", "--topo-order", "HEAD"}
	if after != "" {
		if _, err := g.run(ctx, dir, "merge-base", "--is-ancestor", after, "HEAD"); err != nil {
			return nil, fmt.Errorf("knowledge history no longer contains memory cursor %s: %w", after, err)
		}
		args = append(args, "^"+after)
	}
	list, err := g.run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(list)
	ids = ids[:min(len(ids), limit)]
	result := make([]CommitVersion, 0, len(ids))
	for _, id := range ids {
		metadata, err := g.run(ctx, dir, "show", "-s", "--format=%cI%x00%(trailers:key=Acorn-Run,valueonly)", id)
		if err != nil {
			return nil, err
		}
		parts := strings.SplitN(metadata, "\x00", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid knowledge commit metadata")
		}
		at, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			return nil, err
		}
		paths, err := g.run(ctx, dir, "diff-tree", "--root", "--no-commit-id", "--name-only", "--diff-filter=AM", "-r", "-z", id)
		if err != nil {
			return nil, err
		}
		result = append(result, CommitVersion{ID: id, RunID: strings.TrimSpace(parts[1]), At: at, Paths: strings.Split(strings.TrimSuffix(paths, "\x00"), "\x00")})
	}
	return result, nil
}
