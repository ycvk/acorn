package workstation

import (
	"bufio"
	"bytes"
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

// Supervisor restarts processes that exit, waiting longer after each quick
// failure. A run that lasted StableAfter resets the wait. Each process runs
// in its own process group, and its output goes to Output line by line under
// its name.
type Supervisor struct {
	MinBackoff  time.Duration
	MaxBackoff  time.Duration
	StableAfter time.Duration
	StopGrace   time.Duration
	Output      io.Writer

	outOnce sync.Once
	out     *lockedWriter
}

func NewSupervisor(output io.Writer) *Supervisor {
	return &Supervisor{MinBackoff: time.Second, MaxBackoff: time.Minute, StableAfter: time.Minute, StopGrace: 5 * time.Second, Output: output}
}

// Run keeps every companion running until ctx ends, then stops them and
// returns once all have exited.
func (s *Supervisor) Run(ctx context.Context, companions []Companion) {
	var wg sync.WaitGroup
	for _, c := range companions {
		wg.Go(func() { s.Keep(ctx, c.Name, func(ctx context.Context) error { return s.runCompanion(ctx, c) }) })
	}
	wg.Wait()
}

// Keep runs attempt again each time it returns, until ctx ends.
func (s *Supervisor) Keep(ctx context.Context, name string, attempt func(context.Context) error) {
	backoff := s.MinBackoff
	for {
		started := time.Now()
		err := attempt(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= s.StableAfter {
			backoff = s.MinBackoff
		}
		s.log().line(name, fmt.Sprintf("exited (%v); restarting in %s", err, backoff))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, s.MaxBackoff)
	}
}

// runCompanion runs a companion's shell command once, logging its output.
func (s *Supervisor) runCompanion(ctx context.Context, c Companion) error {
	cmd := s.Command(ctx, "bash", "-c", c.Command)
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
		s.log().line(c.Name, scanner.Text())
	}
	return s.Wait(cmd)
}

// Command prepares a process in its own process group, so stopping it also
// stops whatever it started: ctx ending sends the group SIGTERM.
func (s *Supervisor) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = s.StopGrace
	return cmd
}

// Wait waits for a process Command started, killing its group when the group
// ignored SIGTERM past the grace period.
func (s *Supervisor) Wait(cmd *exec.Cmd) error {
	err := cmd.Wait()
	if errors.Is(err, exec.ErrWaitDelay) {
		if killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			return errors.Join(err, killErr)
		}
	}
	return err
}

// Lines is a writer that logs what it gets line by line under name.
func (s *Supervisor) Lines(name string) io.Writer {
	return &lineWriter{name: name, out: s.log()}
}

func (s *Supervisor) log() *lockedWriter {
	s.outOnce.Do(func() { s.out = &lockedWriter{w: s.Output} })
	return s.out
}

// lineWriter splits writes into lines; a line longer than maxLogLine is
// logged in pieces.
type lineWriter struct {
	name    string
	out     *lockedWriter
	mu      sync.Mutex
	pending []byte
}

const maxLogLine = 64 * 1024

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		w.out.line(w.name, strings.TrimSuffix(string(w.pending[:i]), "\r"))
		w.pending = w.pending[i+1:]
	}
	for len(w.pending) > maxLogLine {
		w.out.line(w.name, string(w.pending[:maxLogLine]))
		w.pending = w.pending[maxLogLine:]
	}
	return len(p), nil
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
