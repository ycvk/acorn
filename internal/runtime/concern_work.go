package runtime

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// WorkHost keeps the directories the agent works on concerns in, one per
// concern id, on the workstation.
type WorkHost interface {
	WorkDirs(ctx context.Context) ([]string, error)
	DeleteWorkDir(ctx context.Context, id string) error
}

// removeForgottenWork deletes the work directories of concerns created in
// conversations the owner deleted, so the files derived from a deleted
// conversation leave the workstation too. Other directories stay.
func removeForgottenWork(ctx context.Context, concerns core.MemoryStore, host WorkHost) error {
	if host == nil {
		return nil
	}
	present, err := host.WorkDirs(ctx)
	if err != nil {
		return fmt.Errorf("list work directories: %w", err)
	}
	if len(present) == 0 {
		return nil
	}
	ids, err := concerns.DeletedThreadConcernIDs(ctx)
	if err != nil {
		return err
	}
	forgotten := map[string]bool{}
	for _, id := range ids {
		forgotten[id] = true
	}
	for _, id := range present {
		if !forgotten[id] {
			continue
		}
		if err := host.DeleteWorkDir(ctx, id); err != nil {
			return fmt.Errorf("delete work directory %s: %w", id, err)
		}
	}
	return nil
}
