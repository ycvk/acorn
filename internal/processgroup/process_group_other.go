//go:build !darwin && !linux

package processgroup

import (
	"errors"
	"os/exec"
)

var (
	ErrUnsupportedPlatform = errors.New("process-group kill is unsupported on this platform")
	ErrProcessNotStarted   = errors.New("command process is not started")
	ErrProcessGroupGone    = errors.New("process group no longer exists")
)

func ConfigureCommand(cmd *exec.Cmd) {
}

func KillCommandGroup(cmd *exec.Cmd) error {
	return ErrUnsupportedPlatform
}
