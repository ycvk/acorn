//go:build !darwin && !linux

package mcp

import "os/exec"

func configureCommand(*exec.Cmd) {}
