package wire

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/store"
)

// A backup directory holds one snapshot per run under snapshots/ (the
// database and persona at that moment) beside add-only mirrors of the
// storage directories whose files never change once written.
var (
	mirroredDirs    = []string{"attachments", "artifacts"}
	snapshotPattern = regexp.MustCompile(`^\d{8}T\d{6}Z$`)
)

// BackupReport describes one backup run.
type BackupReport struct {
	Snapshot      string   `json:"snapshot"`
	DatabaseBytes int64    `json:"database_bytes"`
	FilesCopied   int      `json:"files_copied"`
	Removed       []string `json:"removed_snapshots"`
}

// Backup snapshots the database and persona into dir, mirrors attachments and
// artifacts, and keeps the newest keep snapshots. A running Acorn keeps
// working meanwhile.
func Backup(ctx context.Context, cfg *config.Config, dir string, keep int, now time.Time) (BackupReport, error) {
	if dir == "" || keep < 1 {
		return BackupReport{}, errors.New("backup needs a directory and keep of at least 1")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return BackupReport{}, err
	}
	snapshots := filepath.Join(dir, "snapshots")
	if err := os.MkdirAll(snapshots, 0o700); err != nil {
		return BackupReport{}, err
	}
	name := now.UTC().Format("20060102T150405Z")
	final := filepath.Join(snapshots, name)
	if _, err := os.Stat(final); err == nil {
		return BackupReport{}, fmt.Errorf("snapshot %s already exists", final)
	}
	partial := filepath.Join(snapshots, ".partial-"+name)
	if err := os.RemoveAll(partial); err != nil {
		return BackupReport{}, err
	}
	if err := os.Mkdir(partial, 0o700); err != nil {
		return BackupReport{}, err
	}
	database := filepath.Join(partial, "acorn.db")
	if err := store.SnapshotDatabase(ctx, cfg.Runtime.StorageDir, database); err != nil {
		return BackupReport{}, err
	}
	if err := store.VerifyDatabase(ctx, database); err != nil {
		return BackupReport{}, err
	}
	report := BackupReport{Snapshot: final}
	info, err := os.Stat(database)
	if err != nil {
		return BackupReport{}, err
	}
	report.DatabaseBytes = info.Size()
	// Mirroring after the snapshot covers every file the snapshot refers to.
	for _, name := range mirroredDirs {
		n, err := mirrorFiles(filepath.Join(cfg.Runtime.StorageDir, name), filepath.Join(dir, name))
		if err != nil {
			return BackupReport{}, err
		}
		report.FilesCopied += n
	}
	if err := copyFileAtomic(cfg.PersonaPath(), filepath.Join(partial, "persona.md")); err != nil {
		return BackupReport{}, fmt.Errorf("back up persona: %w", err)
	}
	if err := os.Rename(partial, final); err != nil {
		return BackupReport{}, err
	}
	report.Removed, err = pruneSnapshots(snapshots, keep)
	return report, err
}

// RestoreReport describes one restore.
type RestoreReport struct {
	Snapshot    string `json:"snapshot"`
	FilesCopied int    `json:"files_copied"`
}

// Restore puts a snapshot's database and persona back into the storage
// directory and copies mirrored files that are missing there. Acorn must be
// stopped.
func Restore(ctx context.Context, cfg *config.Config, snapshot string) (RestoreReport, error) {
	snapshot, err := filepath.Abs(snapshot)
	if err != nil {
		return RestoreReport{}, err
	}
	if filepath.Base(filepath.Dir(snapshot)) != "snapshots" || !snapshotPattern.MatchString(filepath.Base(snapshot)) {
		return RestoreReport{}, fmt.Errorf("%s is not a snapshot directory made by acorn backup", snapshot)
	}
	dir := filepath.Dir(filepath.Dir(snapshot))
	if err := os.MkdirAll(cfg.Runtime.StorageDir, 0o755); err != nil {
		return RestoreReport{}, err
	}
	report := RestoreReport{Snapshot: snapshot}
	err = store.RestoreStorage(ctx, cfg.Runtime.StorageDir, filepath.Join(snapshot, "acorn.db"), func() error {
		if err := copyFileAtomic(filepath.Join(snapshot, "persona.md"), cfg.PersonaPath()); err != nil {
			return fmt.Errorf("restore persona: %w", err)
		}
		for _, name := range mirroredDirs {
			n, err := mirrorFiles(filepath.Join(dir, name), filepath.Join(cfg.Runtime.StorageDir, name))
			if err != nil {
				return err
			}
			report.FilesCopied += n
		}
		return nil
	})
	return report, err
}

// mirrorFiles copies files under from that to lacks or holds at a different
// size. Mirrored directories only gain files, so nothing is deleted. A
// missing source directory has nothing to mirror yet.
func mirrorFiles(from, to string) (int, error) {
	if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	copied := 0
	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || !d.Type().IsRegular() {
			return walkErr
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		dest := filepath.Join(to, rel)
		if existing, err := os.Stat(dest); err == nil && existing.Size() == info.Size() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		if err := copyFileAtomic(path, dest); err != nil {
			return err
		}
		copied++
		return nil
	})
	if err != nil {
		return copied, fmt.Errorf("mirror %s: %w", from, err)
	}
	return copied, nil
}

func copyFileAtomic(from, to string) (err error) {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(to), ".copy-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), to)
}

func pruneSnapshots(dir string, keep int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && snapshotPattern.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	var removed []string
	for len(names) > keep {
		path := filepath.Join(dir, names[0])
		if err := os.RemoveAll(path); err != nil {
			return removed, err
		}
		removed = append(removed, path)
		names = names[1:]
	}
	return removed, nil
}
