//go:build !darwin && !linux

package mcp

import "os/exec"

// configureCommand is a no-op on platforms without process-group support.
func configureCommand(cmd *exec.Cmd) {}
