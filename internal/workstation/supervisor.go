package workstation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Companion is a long-running process the workstation keeps alive next to
// its API, such as its browser and the browser daemons.
type Companion struct {
	Name    string
	Command string
}

// ParseCompanion reads "name=command".
func ParseCompanion(spec string) (Companion, error) {
	name, command, ok := strings.Cut(spec, "=")
	name, command = strings.TrimSpace(name), strings.TrimSpace(command)
	if !ok || name == "" || command == "" {
		return Companion{}, fmt.Errorf("companion %q must be name=command", spec)
	}
	return Companion{Name: name, Command: command}, nil
}

// Supervisor restarts companions that exit, waiting longer after each quick
// failure. A run that lasted StableAfter resets the wait.
type Supervisor struct {
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
	StableAfter time.Duration
	StopGrace   time.Duration
	Output      io.Writer
}

func NewSupervisor(output io.Writer) *Supervisor {
	return &Supervisor{MinBackoff: time.Second, MaxBackoff: time.Minute, StableAfter: time.Minute, StopGrace: 5 * time.Second, Output: output}
}

// Run keeps every companion running until ctx ends, then stops them and
// returns once all have exited.
func (s *Supervisor) Run(ctx context.Context, companions []Companion) {
	out := &lockedWriter{w: s.Output}
	var wg sync.WaitGroup
	for _, c := range companions {
		wg.Go(func() { s.keep(ctx, c, out) })
	}
	wg.Wait()
}

func (s *Supervisor) keep(ctx context.Context, c Companion, out *lockedWriter) {
	backoff := s.MinBackoff
	for {
		started := time.Now()
		err := s.runOnce(ctx, c, out)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= s.StableAfter {
			backoff = s.MinBackoff
		}
		out.line(c.Name, fmt.Sprintf("exited (%v); restarting in %s", err, backoff))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, s.MaxBackoff)
	}
}

// runOnce runs the command in its own process group so stopping it also
// stops whatever it started.
func (s *Supervisor) runOnce(ctx context.Context, c Companion, out *lockedWriter) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", c.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = s.StopGrace
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		out.line(c.Name, scanner.Text())
	}
	err = cmd.Wait()
	if errors.Is(err, exec.ErrWaitDelay) {
		// The group ignored SIGTERM past the grace period.
		if killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			return errors.Join(err, killErr)
		}
	}
	return err
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// ansiEscape matches terminal color sequences, which companions print even
// when their output goes to a log.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func (l *lockedWriter) line(name, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "[%s] %s\n", name, ansiEscape.ReplaceAllString(text, ""))
}
