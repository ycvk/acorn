//go:build darwin || linux

package processgroup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

var (
	ErrUnsupportedPlatform = errors.New("process-group kill is unsupported on this platform")
	ErrProcessNotStarted   = errors.New("command process is not started")
	ErrProcessGroupGone    = errors.New("process group no longer exists")
)

func ConfigureCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func KillCommandGroup(cmd *exec.Cmd) error {
	return SignalCommandGroup(cmd, syscall.SIGKILL)
}

func SignalCommandGroup(cmd *exec.Cmd, signal os.Signal) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return ErrProcessNotStarted
	}
	sysSignal, ok := signal.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported process signal %T", signal)
	}
	if err := syscall.Kill(-cmd.Process.Pid, sysSignal); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("%w: pid %d", ErrProcessGroupGone, cmd.Process.Pid)
		}
		return fmt.Errorf("signal process group for pid %d: %w", cmd.Process.Pid, err)
	}
	return nil
}

func ParseSignal(raw string) (os.Signal, error) {
	switch raw {
	case "", "TERM", "SIGTERM":
		return syscall.SIGTERM, nil
	case "INT", "SIGINT":
		return syscall.SIGINT, nil
	case "KILL", "SIGKILL":
		return syscall.SIGKILL, nil
	case "HUP", "SIGHUP":
		return syscall.SIGHUP, nil
	default:
		return nil, fmt.Errorf("unsupported process signal %q", raw)
	}
}
