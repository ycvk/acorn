package workstation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestParseCompanion(t *testing.T) {
	c, err := ParseCompanion("browser=exec chromium --a=b")
	if err != nil || c.Name != "browser" || c.Command != "exec chromium --a=b" {
		t.Fatalf("companion = %+v err = %v", c, err)
	}
	for _, bad := range []string{"browser", "=cmd", "name="} {
		if _, err := ParseCompanion(bad); err == nil {
			t.Fatalf("ParseCompanion(%q) succeeded", bad)
		}
	}
}

func TestSupervisorRestartsAndStopsCompanions(t *testing.T) {
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts")
	child := filepath.Join(dir, "child-alive")
	out := &syncBuffer{}
	s := &Supervisor{MinBackoff: 50 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, StableAfter: time.Minute, StopGrace: time.Second, Output: out}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		s.Run(ctx, []Companion{
			{Name: "flaky", Command: "echo started >> " + starts + "; printf '\\033[32mhello\\033[0m\\n'; exit 3"},
			{Name: "steady", Command: "(while true; do touch " + child + "; sleep 0.1; done) & wait"},
		})
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(starts)
		if strings.Count(string(data), "started") >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("flaky companion started %d times", strings.Count(string(data), "started"))
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(child); !os.IsNotExist(err) {
		t.Fatalf("steady companion's child outlived the supervisor: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "[flaky] hello") || !strings.Contains(got, "[flaky] exited (exit status 3)") {
		t.Fatalf("output = %q", got)
	}
}
