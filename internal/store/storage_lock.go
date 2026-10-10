package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Every process holds a shared lock while its store is open. Index maintenance
// takes the exclusive lock, which the OS releases even after a process crash.
func lockStorage(dir string, exclusive bool) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "acorn.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err := syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("storage is in use; stop other Acorn processes before memory index maintenance")
		}
		return nil, fmt.Errorf("lock storage: %w", err)
	}
	return f, nil
}
