package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/wire"
)

func runBackup(ctx context.Context, args []string) error {
	fs := newFlagSet("backup")
	configPath := addConfigFlag(fs)
	dir := fs.String("dir", "", "backup directory")
	keep := fs.Int("keep", 14, "number of snapshots to keep")
	jsonMode := fs.Bool("json", false, "print the backup report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *dir == "" {
		return errors.New("usage: acorn backup [-c path] [--json] --dir path [--keep n]")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	report, err := wire.Backup(ctx, cfg, *dir, *keep, time.Now())
	if err != nil {
		return err
	}
	if *jsonMode {
		return printJSON(report)
	}
	fmt.Printf("Snapshot: %s (database %d bytes)\n", report.Snapshot, report.DatabaseBytes)
	fmt.Printf("Mirrored files copied: %d\n", report.FilesCopied)
	for _, removed := range report.Removed {
		fmt.Printf("Removed old snapshot: %s\n", removed)
	}
	return nil
}

func runRestore(ctx context.Context, args []string) error {
	fs := newFlagSet("restore")
	configPath := addConfigFlag(fs)
	from := fs.String("from", "", "snapshot directory under <backup dir>/snapshots")
	jsonMode := fs.Bool("json", false, "print the restore report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *from == "" {
		return errors.New("usage: acorn restore [-c path] [--json] --from snapshot")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	report, err := wire.Restore(ctx, cfg, *from)
	if err != nil {
		return err
	}
	if *jsonMode {
		return printJSON(report)
	}
	fmt.Printf("Restored %s into %s; copied %d mirrored files.\n", report.Snapshot, cfg.Runtime.StorageDir, report.FilesCopied)
	return nil
}
