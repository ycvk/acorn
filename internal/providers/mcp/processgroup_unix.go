//go:build darwin || linux

package mcpprovider

import (
	"os/exec"
	"syscall"
)

// configureCommand sets up the command's process group so that child
// processes can be signaled as a group. This is inlined from the former
// tools.ConfigureCommand to avoid a cross-package dependency on
// internal/tools (which is being refactored in a later phase).
func configureCommand(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
