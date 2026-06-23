//go:build !darwin && !linux

package tools

import (
	"errors"
	"os"
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

func SignalCommandGroup(cmd *exec.Cmd, signal os.Signal) error {
	return ErrUnsupportedPlatform
}
