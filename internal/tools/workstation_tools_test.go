package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workstation"
)

const workstationTestToken = "0123456789abcdef0123456789abcdef"

func workstationSpecs(t *testing.T) map[string]core.ToolSpec {
	t.Helper()
	specs, _ := workstationSpecsWithFiles(t, 1<<20)
	return specs
}

// workstationSpecsWithFiles builds the workstation tools on a real
// workstation server and store; send_file runs as run_send in thread
// thread_send.
func workstationSpecsWithFiles(t *testing.T, maxSend int64) (map[string]core.ToolSpec, *store.Store) {
	t.Helper()
	server, err := workstation.NewServer(workstation.ServerConfig{Home: t.TempDir(), Token: workstationTestToken, Supervisor: workstation.NewSupervisor(io.Discard)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	client, err := workstation.NewClient(ts.URL, workstationTestToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.CreateSession(context.Background(), "thread_send", "files"); err != nil {
		t.Fatal(err)
	}
	specs, err := BuildWorkstationToolSpecs(WorkstationToolsConfig{
		Service: client, Files: db, FollowUps: db, Context: fixedArtifactContext{runID: "run_send", sessionID: "thread_send"},
		Settings: settings.Fixed(core.Settings{Location: time.UTC}), Clock: func() time.Time { return followUpNow },
		MaxOutputBytes: 4096, MaxSendBytes: maxSend,
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]core.ToolSpec{}
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	return byName, db
}

func invoke(t *testing.T, spec core.ToolSpec, args string) (string, error) {
	t.Helper()
	return spec.Tool.(einotool.InvokableTool).InvokableRun(context.Background(), args)
}

func TestWorkstationToolsRunCommandsAndFiles(t *testing.T) {
	specs := workstationSpecs(t)
	for _, name := range []string{"shell", "job", "follow_up", "files"} {
		spec := specs[name]
		if !spec.Enabled() || spec.Loading.Mode != core.ToolLoadingModeDeferred {
			t.Fatalf("%s enabled=%v loading=%s", name, spec.Enabled(), spec.Loading.Mode)
		}
	}
	if _, err := invoke(t, specs["files"], `{"action":"write","path":"data/n.txt","content":"3\n4\n"}`); err != nil {
		t.Fatal(err)
	}
	out, err := invoke(t, specs["shell"], `{"command":"awk '{s+=$1} END {print s}' n.txt","dir":"data"}`)
	if err != nil {
		t.Fatal(err)
	}
	var result ShellOutput
	if err := json.Unmarshal([]byte(out), &result); err != nil || strings.TrimSpace(result.Output) != "7" || result.State != workstation.JobExited || result.ExitCode == nil || *result.ExitCode != 0 || result.OutputPath == "" {
		t.Fatalf("shell = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["files"], `{"action":"read","path":"data/n.txt"}`)
	if err != nil || !strings.Contains(out, `"content":"3\n4\n"`) {
		t.Fatalf("read = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["files"], `{"action":"edit","path":"data/n.txt","edits":[{"old":"4\n","new":"5\n6\n"}]}`)
	if err != nil || !strings.Contains(out, `"lines":[2]`) {
		t.Fatalf("edit = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["files"], `{"action":"read","path":"data/n.txt"}`)
	if err != nil || !strings.Contains(out, `"content":"3\n5\n6\n"`) {
		t.Fatalf("read after edit = %s err = %v", out, err)
	}
	if _, err := invoke(t, specs["files"], `{"action":"delete","path":"data/n.txt"}`); err == nil || !strings.Contains(err.Error(), "read, write or edit") {
		t.Fatalf("delete err = %v", err)
	}
}

// A command still running when shell stops waiting keeps running as a job,
// which job reads and stops.
func TestShellLeavesLongCommandsRunningAsJobs(t *testing.T) {
	specs := workstationSpecs(t)
	out, err := invoke(t, specs["shell"], `{"command":"echo start; sleep 2; echo 完成","wait_seconds":0}`)
	if err != nil {
		t.Fatal(err)
	}
	var started ShellOutput
	if err := json.Unmarshal([]byte(out), &started); err != nil || started.State != workstation.JobRunning || started.JobID == "" || started.ExitCode != nil {
		t.Fatalf("shell = %s err = %v", out, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var status JobToolOutput
	for {
		out, err = invoke(t, specs["job"], `{"action":"status","job_id":"`+started.JobID+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(out), &status); err != nil {
			t.Fatal(err)
		}
		if status.Job.State != workstation.JobRunning || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if status.Job.State != workstation.JobExited || *status.Job.ExitCode != 0 || status.Job.EndedAt == nil {
		t.Fatalf("status = %+v", status.Job)
	}
	out, err = invoke(t, specs["job"], `{"action":"output","job_id":"`+started.JobID+`","offset":6}`)
	var read JobToolOutput
	if err != nil || json.Unmarshal([]byte(out), &read) != nil || read.Text != "完成\n" || read.NextOffset != int64(len("start\n完成\n")) {
		t.Fatalf("output = %s err = %v", out, err)
	}

	out, err = invoke(t, specs["shell"], `{"command":"sleep 30","wait_seconds":0}`)
	if err != nil || json.Unmarshal([]byte(out), &started) != nil {
		t.Fatalf("shell = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["job"], `{"action":"stop","job_id":"`+started.JobID+`"}`)
	var stopped JobToolOutput
	if err != nil || json.Unmarshal([]byte(out), &stopped) != nil || stopped.Job.State != workstation.JobStopped || stopped.Job.Note != "stopped on request" {
		t.Fatalf("stop = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["job"], `{"action":"list"}`)
	var listed JobToolOutput
	if err != nil || json.Unmarshal([]byte(out), &listed) != nil || len(listed.Jobs) != 2 || listed.Jobs[0].ID != started.JobID {
		t.Fatalf("list = %s err = %v", out, err)
	}
	if _, err := invoke(t, specs["shell"], `{"command":"true","wait_seconds":601}`); err == nil || !strings.Contains(err.Error(), "between 0 and 600") {
		t.Fatalf("long wait err = %v", err)
	}
	if _, err := invoke(t, specs["job"], `{"action":"status","job_id":"job-00000000"}`); err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("missing job err = %v", err)
	}
}

var followUpNow = time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)

// follow_up waits on a running job or on a path, up to its deadline, and
// cancels only what still waits.
func TestFollowUpWaitsOnJobsAndFiles(t *testing.T) {
	specs, db := workstationSpecsWithFiles(t, 1<<20)
	out, err := invoke(t, specs["shell"], `{"command":"sleep 30","wait_seconds":0}`)
	var running ShellOutput
	if err != nil || json.Unmarshal([]byte(out), &running) != nil {
		t.Fatalf("shell = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["shell"], `{"command":"true"}`)
	var ended ShellOutput
	if err != nil || json.Unmarshal([]byte(out), &ended) != nil {
		t.Fatalf("shell = %s err = %v", out, err)
	}

	out, err = invoke(t, specs["follow_up"], `{"task":"下载完就整理","job_id":"`+running.JobID+`","output_contains":"100%"}`)
	var waiting FollowUpOutput
	if err != nil || json.Unmarshal([]byte(out), &waiting) != nil || waiting.State != core.FollowUpWaiting || waiting.Deadline != "2026-11-04 Wed 08:00" {
		t.Fatalf("follow_up = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["follow_up"], `{"task":"书到了就读","path":"books/book.epub","within":"3d"}`)
	var file FollowUpOutput
	if err != nil || json.Unmarshal([]byte(out), &file) != nil || file.Deadline != "2026-11-06 Fri 08:00" {
		t.Fatalf("follow_up = %s err = %v", out, err)
	}
	for args, want := range map[string]string{
		`{"task":"x","job_id":"` + ended.JobID + `"}`:                     "already ended",
		`{"task":"x","job_id":"job-00000000"}`:                            "job not found",
		`{"task":"x","job_id":"` + running.JobID + `","path":"a"}`:        "either job_id or path",
		`{"task":"x","path":"a","output_contains":"done"}`:                "needs job_id",
		`{"task":"x","path":"a","within":"8d"}`:                           "at most",
		`{"job_id":"` + running.JobID + `"}`:                              "task is required",
		`{"cancel":` + strconv.FormatInt(waiting.ID, 10) + `,"task":"x"}`: "cancel alone",
	} {
		if _, err := invoke(t, specs["follow_up"], args); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("follow_up %s err = %v, want %q", args, err, want)
		}
	}
	all, err := db.WaitingFollowUps(context.Background(), "thread_send")
	if err != nil || len(all) != 2 || all[0].JobID != running.JobID || all[0].OutputContains != "100%" || all[0].SourceRunID != "run_send" || all[1].Path != "books/book.epub" {
		t.Fatalf("waiting = %+v, %v", all, err)
	}
	out, err = invoke(t, specs["follow_up"], `{"cancel":`+strconv.FormatInt(waiting.ID, 10)+`}`)
	if err != nil || !strings.Contains(out, `"state":"cancelled"`) {
		t.Fatalf("cancel = %s err = %v", out, err)
	}
	if _, err := invoke(t, specs["follow_up"], `{"cancel":`+strconv.FormatInt(waiting.ID, 10)+`}`); err == nil || !strings.Contains(err.Error(), "not waiting") {
		t.Fatalf("second cancel err = %v", err)
	}
}

func TestWorkstationToolsDisabledWithoutService(t *testing.T) {
	if _, err := BuildWorkstationToolSpecs(WorkstationToolsConfig{}); err == nil {
		t.Fatal("missing service without a reason must fail")
	}
	specs, err := BuildWorkstationToolSpecs(WorkstationToolsConfig{DisabledReason: "workstation.url is not configured"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if spec.Enabled() || spec.Health.Reason != "workstation.url is not configured" {
			t.Fatalf("%s enabled=%v reason=%q", spec.Name, spec.Enabled(), spec.Health.Reason)
		}
	}
}

func TestSendFileHandsTheOwnerAWorkstationFile(t *testing.T) {
	specs, db := workstationSpecsWithFiles(t, 1<<20)
	if spec := specs["send_file"]; !spec.Enabled() || spec.Loading.Mode != core.ToolLoadingModeDeferred {
		t.Fatalf("send_file enabled=%v loading=%s", spec.Enabled(), spec.Loading.Mode)
	}
	if _, err := invoke(t, specs["shell"], `{"command":"mkdir -p books && printf 'PK\\003\\004book' > books/hongloumeng.epub && head -c 2000000 /dev/zero > big.bin"}`); err != nil {
		t.Fatal(err)
	}
	out, err := invoke(t, specs["send_file"], `{"path":"books/hongloumeng.epub","name":"红楼梦.epub"}`)
	if err != nil {
		t.Fatal(err)
	}
	var sent SendFileOutput
	if err := json.Unmarshal([]byte(out), &sent); err != nil || sent.Name != "红楼梦.epub" || sent.MIMEType != "application/epub+zip" || sent.SizeBytes != 8 {
		t.Fatalf("send_file = %s err = %v", out, err)
	}
	file, content, err := db.OpenThreadFile(context.Background(), "thread_send", sent.FileID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(content)
	_ = content.Close()
	if err != nil || !bytes.Equal(data, []byte("PK\003\004book")) || file.Origin != core.ThreadFileFromAgent || file.RunID != "run_send" {
		t.Fatalf("stored file = %+v %q err = %v", file, data, err)
	}
	if _, err := invoke(t, specs["send_file"], `{"path":"big.bin"}`); err == nil || !strings.Contains(err.Error(), "more than the 1 MiB") {
		t.Fatalf("oversized send err = %v", err)
	}
	if _, err := invoke(t, specs["send_file"], `{"path":"missing.pdf"}`); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing send err = %v", err)
	}
	if files, err := db.ThreadFiles(context.Background(), "thread_send"); err != nil || len(files) != 1 {
		t.Fatalf("thread files = %+v err = %v", files, err)
	}
}
