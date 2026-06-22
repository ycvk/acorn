//go:build darwin || linux

package toolset

import (
	"os/exec"
	"testing"
	"time"
)

func TestConfigureCommandNil(t *testing.T) {
	ConfigureCommand(nil)
}

func TestConfigureCommand(t *testing.T) {
	cmd := exec.Command("sleep", "1")
	ConfigureCommand(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatalf("expected SysProcAttr to be set")
	}
}

func TestKillCommandGroupNil(t *testing.T) {
	err := KillCommandGroup(nil)
	if err != ErrProcessNotStarted {
		t.Fatalf("expected ErrProcessNotStarted for nil cmd, got %v", err)
	}
}

func TestKillCommandGroupNotStarted(t *testing.T) {
	cmd := exec.Command("sleep", "1")
	err := KillCommandGroup(cmd)
	if err != ErrProcessNotStarted {
		t.Fatalf("expected ErrProcessNotStarted for unstarted cmd, got %v", err)
	}
}

func TestKillCommandGroup(t *testing.T) {
	cmd := exec.Command("sleep", "10")
	ConfigureCommand(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer cmd.Wait()

	time.Sleep(50 * time.Millisecond)

	err := KillCommandGroup(cmd)
	if err != nil {
		t.Fatalf("KillCommandGroup: %v", err)
	}
}
