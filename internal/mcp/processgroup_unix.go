//go:build darwin || linux

package mcp

import (
	"os/exec"
	"syscall"
)

// configureCommand starts the stdio MCP server in its own process group so
// signals aimed at acorn do not reach the server directly.
func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
