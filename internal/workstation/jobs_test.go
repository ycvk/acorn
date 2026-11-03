package workstation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A job that ends within the wait comes back whole; a long output shows its
// beginning and end and says where the rest is.
func TestJobsShowTheBeginningAndEndOfLongOutput(t *testing.T) {
	home := t.TempDir()
	j, err := newJobs(home)
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	got, err := j.start(context.Background(), JobStartRequest{Command: "printf 'hello'; exit 3", WaitSeconds: 10, MaxOutputBytes: 64}, home)
	if err != nil {
		t.Fatal(err)
	}
	if got.Job.State != JobExited || *got.Job.ExitCode != 3 || got.Output != "hello" || got.OutputTruncated {
		t.Fatalf("short job = %+v", got)
	}
	got, err = j.start(context.Background(), JobStartRequest{Command: "printf '开头'; head -c 1000 /dev/zero | tr '\\0' x; printf '结尾'", WaitSeconds: 10, MaxOutputBytes: 40}, home)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OutputTruncated || !strings.HasPrefix(got.Output, "开头") || !strings.HasSuffix(got.Output, "结尾") || !strings.Contains(got.Output, "bytes omitted; read them with job output from offset") {
		t.Fatalf("long job output = %q", got.Output)
	}
	if got.Job.OutputBytes != int64(len("开头结尾"))+1000 || !strings.HasPrefix(got.Job.OutputPath, filepath.Join(home, ".jobs", got.Job.ID)) {
		t.Fatalf("long job = %+v", got.Job)
	}
	// A read never ends inside a character.
	out, err := j.read(got.Job.ID, 0, 4)
	if err != nil || out.Text != "开" || out.NextOffset != 3 {
		t.Fatalf("read = %+v, %v", out, err)
	}
	if _, err := j.read(got.Job.ID, got.Job.OutputBytes+1, 10); err == nil {
		t.Fatal("a read past the output must fail")
	}
}

// Jobs a restarted workstation finds running are lost; jobs that ended more
// than a week ago are removed.
func TestJobsAfterARestart(t *testing.T) {
	home := t.TempDir()
	j, err := newJobs(home)
	if err != nil {
		t.Fatal(err)
	}
	running, err := j.start(context.Background(), JobStartRequest{Command: "sleep 30", WaitSeconds: 0, MaxOutputBytes: 64}, home)
	if err != nil {
		t.Fatal(err)
	}
	ended, err := j.start(context.Background(), JobStartRequest{Command: "true", WaitSeconds: 10, MaxOutputBytes: 64}, home)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour).UTC()
	record, err := j.job(ended.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.EndedAt = &old
	if err := j.save(record); err != nil {
		t.Fatal(err)
	}
	// The process of the first job would die with the workstation; here it is
	// ended by hand, without the record knowing.
	j.mu.Lock()
	live := j.live[running.Job.ID]
	delete(j.live, running.Job.ID)
	j.mu.Unlock()
	_ = live.cmd.Process.Kill()
	<-live.done
	record, err = j.job(running.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	record.State, record.EndedAt, record.ExitCode = JobRunning, nil, nil
	if err := j.save(record); err != nil {
		t.Fatal(err)
	}

	again, err := newJobs(home)
	if err != nil {
		t.Fatal(err)
	}
	defer again.close()
	lost, err := again.job(running.Job.ID)
	if err != nil || lost.State != JobLost || lost.EndedAt == nil {
		t.Fatalf("job after restart = %+v, %v", lost, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".jobs", ended.Job.ID)); !os.IsNotExist(err) {
		t.Fatalf("old job still on disk: %v", err)
	}
	if _, err := again.job("../escape"); err == nil {
		t.Fatal("an invalid job id must fail")
	}
}

// A search finds text anywhere in the output, also across the chunks it
// reads, and continues where text cut at the end could still complete.
func TestJobsFindTextInTheirOutput(t *testing.T) {
	client, home := newTestClient(t)
	ctx := context.Background()
	started, err := client.StartJob(ctx, JobStartRequest{Command: "head -c 1048578 /dev/zero | tr '\\0' x; printf 'MARK\\nready on :8080\\n'", WaitSeconds: 10, MaxOutputBytes: 64})
	if err != nil || started.Job.State != JobExited {
		t.Fatalf("start = %+v, %v", started, err)
	}
	id, size := started.Job.ID, started.Job.OutputBytes
	if got, err := client.FindJobOutput(ctx, id, "MARK", 0); err != nil || !got.Found || got.NextOffset != 1048582 {
		t.Fatalf("find across chunks = %+v, %v", got, err)
	}
	if got, err := client.FindJobOutput(ctx, id, "ready on", 1048582); err != nil || !got.Found {
		t.Fatalf("find from an offset = %+v, %v", got, err)
	}
	got, err := client.FindJobOutput(ctx, id, "listening", 0)
	if err != nil || got.Found || got.NextOffset != size-int64(len("listening")-1) || got.Job.State != JobExited {
		t.Fatalf("find missing text = %+v, %v", got, err)
	}
	if _, err := client.FindJobOutput(ctx, id, "", 0); err == nil {
		t.Fatal("empty text must fail")
	}
	if _, err := client.FindJobOutput(ctx, "job-00000000", "x", 0); err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("missing job err = %v", err)
	}

	missing, err := client.Stat(ctx, "book.epub")
	if err != nil || missing.Exists || missing.Path != filepath.Join(home, "book.epub") {
		t.Fatalf("stat missing = %+v, %v", missing, err)
	}
	if err := os.WriteFile(filepath.Join(home, "book.epub"), []byte("epub"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := client.Stat(ctx, "book.epub")
	if err != nil || !file.Exists || file.Dir || file.Size != 4 || file.ModifiedAt.IsZero() {
		t.Fatalf("stat file = %+v, %v", file, err)
	}
	dir, err := client.Stat(ctx, home)
	if err != nil || !dir.Exists || !dir.Dir {
		t.Fatalf("stat dir = %+v, %v", dir, err)
	}
}

// Output past the limit is counted and dropped.
func TestCappedFileDropsWhatIsPastTheLimit(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c := &cappedFile{file: f, limit: 5}
	for _, chunk := range []string{"abc", "defg", "hi"} {
		if n, err := c.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write %q = %d, %v", chunk, n, err)
		}
	}
	data, err := os.ReadFile(f.Name())
	if err != nil || string(data) != "abcde" || c.dropped() != 4 {
		t.Fatalf("kept %q, dropped %d, %v", data, c.dropped(), err)
	}
}
