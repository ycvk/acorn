package workstation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// A job is a shell command the workstation keeps running past the request
// that started it. Its record and its whole output, stdout and stderr
// interleaved, live under ~/.jobs/<id>/, where the agent can also read them
// with its own tools.
const (
	jobsDirName = ".jobs"
	// MaxJobWaitSeconds bounds how long starting a job waits for it to end.
	MaxJobWaitSeconds = 600
	// MaxJobSeconds is how long a job may run before it is stopped.
	MaxJobSeconds = 24 * 60 * 60
	// MaxJobOutputBytes is how much output a job keeps; the rest is counted
	// and dropped.
	MaxJobOutputBytes = 64 << 20
	// MaxJobReadBytes bounds one read of a job's output.
	MaxJobReadBytes = 256 << 10
	// jobRetention is how long a job's record and output stay after it ends.
	jobRetention = 7 * 24 * time.Hour
	// jobStopGrace is how long a stopped job has between SIGTERM and SIGKILL.
	jobStopGrace = 5 * time.Second
	// maxListedJobs bounds the jobs listed at once, newest first.
	maxListedJobs = 20
)

// Job states.
const (
	JobRunning = "running"
	JobExited  = "exited"
	// JobStopped jobs were stopped by request or for running past
	// MaxJobSeconds.
	JobStopped = "stopped"
	// JobLost jobs were running when the workstation restarted.
	JobLost = "lost"
)

// ErrJobNotFound is returned for an id that names no job.
var ErrJobNotFound = errors.New("job not found")

// Job is a job's record.
type Job struct {
	ID        string     `json:"id"`
	Command   string     `json:"command"`
	Dir       string     `json:"dir"`
	State     string     `json:"state"`
	ExitCode  *int       `json:"exit_code,omitempty"`
	Note      string     `json:"note,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// OutputPath is the file holding the output on the workstation.
	OutputPath  string `json:"output_path"`
	OutputBytes int64  `json:"output_bytes"`
}

// JobStartRequest starts Command and waits up to WaitSeconds for it to end.
// The result shows at most MaxOutputBytes of its output.
type JobStartRequest struct {
	Command        string `json:"command"`
	Dir            string `json:"dir,omitempty"`
	WaitSeconds    int    `json:"wait_seconds"`
	MaxOutputBytes int    `json:"max_output_bytes"`
}

// JobResult is a job as it stands, with its output: the beginning and end of
// it once the job ended, its end while the job runs.
type JobResult struct {
	Job             Job    `json:"job"`
	Output          string `json:"output"`
	OutputTruncated bool   `json:"output_truncated,omitempty"`
}

// JobOutput is a slice of a job's output from Offset; NextOffset continues it.
type JobOutput struct {
	Job        Job    `json:"job"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Text       string `json:"text"`
}

// JobFindRequest searches a job's output for Text from Offset on.
type JobFindRequest struct {
	Text   string `json:"text"`
	Offset int64  `json:"offset"`
}

// JobFind is the job as it stood before its output was searched, so a job
// that ended without Text in its output never printed it. NextOffset is
// where the next search starts, early enough to find Text the job had only
// begun to print.
type JobFind struct {
	Job        Job   `json:"job"`
	Found      bool  `json:"found"`
	NextOffset int64 `json:"next_offset"`
}

// MaxJobFindText bounds the text a job's output is searched for.
const MaxJobFindText = 1024

type jobs struct {
	dir  string
	home string

	mu   sync.Mutex
	live map[string]*liveJob
	wg   sync.WaitGroup
}

type liveJob struct {
	cmd  *exec.Cmd
	done chan struct{}
	// stopNote, when set, says why the job was stopped.
	stopNote string
}

// newJobs opens the jobs under home. Jobs recorded as running belong to a
// workstation that has since restarted and are marked lost; jobs that ended
// more than jobRetention ago are removed.
func newJobs(home string) (*jobs, error) {
	dir := filepath.Join(home, jobsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create jobs directory: %w", err)
	}
	j := &jobs{dir: dir, home: home, live: map[string]*liveJob{}}
	all, err := j.records()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, job := range all {
		if job.State != JobRunning {
			continue
		}
		job.State, job.EndedAt, job.Note = JobLost, &now, "the workstation restarted while it ran"
		if err := j.save(job); err != nil {
			return nil, err
		}
	}
	return j, j.prune(now)
}

// start launches a job and waits up to req.WaitSeconds for it to end.
func (j *jobs) start(ctx context.Context, req JobStartRequest, dir string) (JobResult, error) {
	id, err := newJobID()
	if err != nil {
		return JobResult{}, err
	}
	jobDir := filepath.Join(j.dir, id)
	if err := os.Mkdir(jobDir, 0o700); err != nil {
		return JobResult{}, fmt.Errorf("create job directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(jobDir, "output.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return JobResult{}, fmt.Errorf("create job output: %w", err)
	}
	output := &cappedFile{file: file, limit: MaxJobOutputBytes}
	// The job outlives the request that started it.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), "bash", "-lc", req.Command)
	cmd.Dir = dir
	cmd.Env = childEnv(j.home, nil)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout, cmd.Stderr = output, output
	// Children the command leaves running keep the output open; the job ends
	// with the command itself.
	cmd.WaitDelay = 2 * time.Second
	job := Job{ID: id, Command: req.Command, Dir: dir, State: JobRunning, StartedAt: time.Now().UTC(), OutputPath: file.Name()}
	if err := j.save(job); err != nil {
		return JobResult{}, errors.Join(err, file.Close())
	}
	if err := cmd.Start(); err != nil {
		now := time.Now().UTC()
		job.State, job.EndedAt, job.Note = JobExited, &now, "could not start: "+err.Error()
		return JobResult{}, errors.Join(fmt.Errorf("start job: %w", err), j.save(job), file.Close())
	}
	live := &liveJob{cmd: cmd, done: make(chan struct{})}
	j.mu.Lock()
	j.live[id] = live
	j.mu.Unlock()
	j.wg.Go(func() { j.watch(job, live, output) })
	if err := j.prune(job.StartedAt); err != nil {
		slog.Warn("prune old jobs", "error", err)
	}
	select {
	case <-live.done:
	case <-time.After(time.Duration(req.WaitSeconds) * time.Second):
	case <-ctx.Done():
	}
	return j.result(id, req.MaxOutputBytes)
}

// watch waits for the job, stops it past MaxJobSeconds and records its end.
func (j *jobs) watch(job Job, live *liveJob, output *cappedFile) {
	defer close(live.done)
	limit := time.AfterFunc(MaxJobSeconds*time.Second, func() {
		j.stop(job.ID, fmt.Sprintf("ran longer than %d hours", MaxJobSeconds/3600))
	})
	err := live.cmd.Wait()
	limit.Stop()
	now := time.Now().UTC()
	job.EndedAt = &now
	code := live.cmd.ProcessState.ExitCode()
	j.mu.Lock()
	note := live.stopNote
	delete(j.live, job.ID)
	j.mu.Unlock()
	switch {
	case note != "":
		job.State, job.Note = JobStopped, note
	default:
		job.State = JobExited
		job.ExitCode = &code
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !errors.Is(err, exec.ErrWaitDelay) {
		job.Note = strings.TrimSpace(job.Note + " " + err.Error())
	}
	if dropped := output.dropped(); dropped > 0 {
		job.Note = strings.TrimSpace(fmt.Sprintf("%s output past %d MiB dropped (%d bytes)", job.Note, MaxJobOutputBytes>>20, dropped))
	}
	if err := output.file.Close(); err != nil {
		slog.Error("close job output", "job", job.ID, "error", err)
	}
	if err := j.save(job); err != nil {
		slog.Error("record job end", "job", job.ID, "error", err)
	}
}

// stop ends a running job's process group, SIGTERM first; a job that ended
// is left as it is.
func (j *jobs) stop(id, note string) {
	j.mu.Lock()
	live, ok := j.live[id]
	if ok && live.stopNote == "" {
		live.stopNote = note
	}
	j.mu.Unlock()
	if !ok {
		return
	}
	pid := live.cmd.Process.Pid
	signalGroup(id, pid, syscall.SIGTERM)
	select {
	case <-live.done:
		return
	case <-time.After(jobStopGrace):
	}
	signalGroup(id, pid, syscall.SIGKILL)
	select {
	case <-live.done:
	case <-time.After(jobStopGrace):
		slog.Error("job did not end after SIGKILL", "job", id)
	}
}

// signalGroup signals a job's process group; a group that already ended is
// no failure.
func signalGroup(id string, pid int, sig syscall.Signal) {
	if err := syscall.Kill(-pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		slog.Error("signal job", "job", id, "signal", sig, "error", err)
	}
}

// close stops every running job and waits for their records.
func (j *jobs) close() {
	j.mu.Lock()
	ids := make([]string, 0, len(j.live))
	for id := range j.live {
		ids = append(ids, id)
	}
	j.mu.Unlock()
	for _, id := range ids {
		j.stop(id, "the workstation stopped")
	}
	j.wg.Wait()
}

func (j *jobs) job(id string) (Job, error) {
	if !validJobID(id) {
		return Job{}, fmt.Errorf("%w: %q", ErrJobNotFound, id)
	}
	data, err := os.ReadFile(filepath.Join(j.dir, id, "job.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Job{}, fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, fmt.Errorf("job %s record: %w", id, err)
	}
	info, err := os.Stat(job.OutputPath)
	if err != nil {
		return Job{}, fmt.Errorf("job %s output: %w", id, err)
	}
	job.OutputBytes = info.Size()
	return job, nil
}

// result is the job with its output: beginning and end once it ended, the
// end while it runs.
func (j *jobs) result(id string, maxBytes int) (JobResult, error) {
	job, err := j.job(id)
	if err != nil {
		return JobResult{}, err
	}
	text, truncated, err := outputExcerpt(job, maxBytes)
	if err != nil {
		return JobResult{}, err
	}
	return JobResult{Job: job, Output: text, OutputTruncated: truncated}, nil
}

// list returns the newest jobs first.
func (j *jobs) list() ([]Job, error) {
	all, err := j.records()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(all, func(a, b Job) int { return b.StartedAt.Compare(a.StartedAt) })
	if len(all) > maxListedJobs {
		all = all[:maxListedJobs]
	}
	for i := range all {
		if info, err := os.Stat(all[i].OutputPath); err == nil {
			all[i].OutputBytes = info.Size()
		}
	}
	return all, nil
}

// read returns up to limit bytes of a job's output from offset, ending on a
// whole UTF-8 character.
func (j *jobs) read(id string, offset int64, limit int) (JobOutput, error) {
	job, err := j.job(id)
	if err != nil {
		return JobOutput{}, err
	}
	if offset < 0 || offset > job.OutputBytes {
		return JobOutput{}, fmt.Errorf("offset %d is outside the job's %d bytes of output", offset, job.OutputBytes)
	}
	if limit < 1 || limit > MaxJobReadBytes {
		return JobOutput{}, fmt.Errorf("limit must be between 1 and %d", MaxJobReadBytes)
	}
	buf, err := readAt(job.OutputPath, offset, limit)
	if err != nil {
		return JobOutput{}, err
	}
	whole := wholeRunes(buf, offset+int64(len(buf)) < job.OutputBytes)
	return JobOutput{Job: job, Offset: offset, NextOffset: offset + int64(len(whole)), Text: strings.ToValidUTF8(string(whole), "�")}, nil
}

// find searches a job's output for req.Text from req.Offset to its current
// end, reading it in chunks that overlap by one byte less than the text.
func (j *jobs) find(id string, req JobFindRequest) (JobFind, error) {
	if req.Text == "" || len(req.Text) > MaxJobFindText {
		return JobFind{}, fmt.Errorf("text must be 1 to %d bytes", MaxJobFindText)
	}
	job, err := j.job(id)
	if err != nil {
		return JobFind{}, err
	}
	if req.Offset < 0 || req.Offset > job.OutputBytes {
		return JobFind{}, fmt.Errorf("offset %d is outside the job's %d bytes of output", req.Offset, job.OutputBytes)
	}
	f, err := os.Open(job.OutputPath)
	if err != nil {
		return JobFind{}, err
	}
	defer f.Close()
	text, overlap := []byte(req.Text), int64(len(req.Text)-1)
	buf := make([]byte, 1<<20+len(text))
	for pos := req.Offset; pos < job.OutputBytes; {
		n := min(int64(len(buf)), job.OutputBytes-pos)
		got, err := f.ReadAt(buf[:n], pos)
		if err != nil && !errors.Is(err, io.EOF) {
			return JobFind{}, err
		}
		if i := bytes.Index(buf[:got], text); i >= 0 {
			return JobFind{Job: job, Found: true, NextOffset: pos + int64(i+len(text))}, nil
		}
		if int64(got) < n || pos+n >= job.OutputBytes {
			break
		}
		pos += n - overlap
	}
	return JobFind{Job: job, NextOffset: max(req.Offset, job.OutputBytes-overlap)}, nil
}

func (j *jobs) records() ([]Job, error) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	var out []Job
	for _, entry := range entries {
		if !entry.IsDir() || !validJobID(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(j.dir, entry.Name(), "job.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var job Job
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, fmt.Errorf("job %s record: %w", entry.Name(), err)
		}
		out = append(out, job)
	}
	return out, nil
}

// save writes the job's record whole, in place of the old one.
func (j *jobs) save(job Job) error {
	job.OutputBytes = 0
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	path := filepath.Join(j.dir, job.ID, "job.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("record job %s: %w", job.ID, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("record job %s: %w", job.ID, err)
	}
	return nil
}

// prune removes the jobs that ended more than jobRetention before now.
func (j *jobs) prune(now time.Time) error {
	all, err := j.records()
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range all {
		if job.EndedAt != nil && now.Sub(*job.EndedAt) > jobRetention {
			errs = append(errs, os.RemoveAll(filepath.Join(j.dir, job.ID)))
		}
	}
	return errors.Join(errs...)
}

func newJobID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("job id: %w", err)
	}
	return "job-" + hex.EncodeToString(b[:]), nil
}

func validJobID(id string) bool {
	rest, ok := strings.CutPrefix(id, "job-")
	if !ok || len(rest) != 8 {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil
}

// outputExcerpt is up to maxBytes of a job's output: its beginning and end
// once it ended, its end while it runs, with the omission said in place.
func outputExcerpt(job Job, maxBytes int) (string, bool, error) {
	size := job.OutputBytes
	if size <= int64(maxBytes) {
		buf, err := readAt(job.OutputPath, 0, int(size))
		return strings.ToValidUTF8(string(buf), "�"), false, err
	}
	if job.State == JobRunning {
		tail, err := readAt(job.OutputPath, size-int64(maxBytes), maxBytes)
		if err != nil {
			return "", false, err
		}
		tail = trimLeadingPartialRune(tail)
		return fmt.Sprintf("[%d earlier bytes; read them with job output or from %s]\n", size-int64(len(tail)), job.OutputPath) +
			strings.ToValidUTF8(string(tail), "�"), true, nil
	}
	half := maxBytes / 2
	head, err := readAt(job.OutputPath, 0, half)
	if err != nil {
		return "", false, err
	}
	tail, err := readAt(job.OutputPath, size-int64(half), half)
	if err != nil {
		return "", false, err
	}
	head, tail = wholeRunes(head, true), trimLeadingPartialRune(tail)
	omitted := size - int64(len(head)) - int64(len(tail))
	return strings.ToValidUTF8(string(head), "�") +
		fmt.Sprintf("\n[%d bytes omitted; read them with job output from offset %d or from %s]\n", omitted, len(head), job.OutputPath) +
		strings.ToValidUTF8(string(tail), "�"), true, nil
}

func readAt(path string, offset int64, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:got], nil
}

// wholeRunes drops a UTF-8 character cut at the end of buf when more follows.
func wholeRunes(buf []byte, more bool) []byte {
	if !more {
		return buf
	}
	for i := len(buf) - 1; i >= 0 && i >= len(buf)-utf8.UTFMax; i-- {
		if utf8.RuneStart(buf[i]) {
			if !utf8.FullRune(buf[i:]) {
				return buf[:i]
			}
			break
		}
	}
	return buf
}

func trimLeadingPartialRune(buf []byte) []byte {
	for i := 0; i < len(buf) && i < utf8.UTFMax; i++ {
		if utf8.RuneStart(buf[i]) {
			return buf[i:]
		}
	}
	return buf
}

// cappedFile writes to file up to limit bytes and counts the rest.
type cappedFile struct {
	file  *os.File
	limit int64

	mu      sync.Mutex
	written int64
	over    int64
}

func (c *cappedFile) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.limit - c.written
	if room <= 0 {
		c.over += int64(len(p))
		return len(p), nil
	}
	keep := p
	if int64(len(keep)) > room {
		keep = keep[:room]
		c.over += int64(len(p)) - room
	}
	n, err := c.file.Write(keep)
	c.written += int64(n)
	if err != nil {
		return n, err
	}
	return len(p), nil
}

func (c *cappedFile) dropped() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.over
}

// --- HTTP ---

func (s *Server) startJob(w http.ResponseWriter, r *http.Request) {
	var req JobStartRequest
	if !decode(w, r, &req) {
		return
	}
	switch {
	case strings.TrimSpace(req.Command) == "":
		writeError(w, http.StatusBadRequest, errors.New("command is required"))
		return
	case req.WaitSeconds < 0 || req.WaitSeconds > MaxJobWaitSeconds:
		writeError(w, http.StatusBadRequest, fmt.Errorf("wait_seconds must be between 0 and %d", MaxJobWaitSeconds))
		return
	case req.MaxOutputBytes < 2 || req.MaxOutputBytes > MaxOutputBytes:
		writeError(w, http.StatusBadRequest, fmt.Errorf("max_output_bytes must be between 2 and %d", MaxOutputBytes))
		return
	}
	result, err := s.jobs.start(r.Context(), req, s.resolve(req.Dir))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listJobs(w http.ResponseWriter, _ *http.Request) {
	all, err := s.jobs.list()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, all)
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.job(r.PathValue("id"))
	if err != nil {
		writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) readJob(w http.ResponseWriter, r *http.Request) {
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("offset: %w", err))
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("limit: %w", err))
		return
	}
	out, err := s.jobs.read(r.PathValue("id"), offset, limit)
	if err != nil {
		writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) findJob(w http.ResponseWriter, r *http.Request) {
	var req JobFindRequest
	if !decode(w, r, &req) {
		return
	}
	out, err := s.jobs.find(r.PathValue("id"), req)
	if err != nil {
		writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) stopJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.jobs.job(id); err != nil {
		writeJobError(w, err)
		return
	}
	s.jobs.stop(id, "stopped on request")
	job, err := s.jobs.job(id)
	if err != nil {
		writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func writeJobError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrJobNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusBadRequest, err)
}
