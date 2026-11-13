package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/textedit"
	"github.com/ycvk/acorn/internal/workstation"
)

// WorkstationService is the subset of the workstation client the shell, job,
// follow_up, files and send_file tools need.
type WorkstationService interface {
	StartJob(ctx context.Context, req workstation.JobStartRequest) (workstation.JobResult, error)
	Jobs(ctx context.Context) ([]workstation.Job, error)
	Job(ctx context.Context, id string) (workstation.Job, error)
	JobOutput(ctx context.Context, id string, offset int64, limit int) (workstation.JobOutput, error)
	StopJob(ctx context.Context, id string) (workstation.Job, error)
	ReadFile(ctx context.Context, req workstation.ReadRequest) (workstation.ReadResult, error)
	WriteFile(ctx context.Context, req workstation.WriteRequest) (workstation.WriteResult, error)
	EditFile(ctx context.Context, req workstation.EditRequest) (workstation.EditResult, error)
	OpenFile(ctx context.Context, path string) (io.ReadCloser, int64, error)
}

// WorkstationToolsConfig carries the workstation behind shell, job,
// follow_up, files and send_file, the follow-ups follow_up sets and the
// thread files send_file stores into. A nil Service disables the five tools,
// and DisabledReason says why.
type WorkstationToolsConfig struct {
	Service        WorkstationService
	Files          core.ThreadFiles
	FollowUps      core.FollowUpStore
	Context        core.ToolCallContextBridge
	Settings       core.SettingsReader
	Clock          func() time.Time
	MaxOutputBytes int
	MaxSendBytes   int64
	DisabledReason string
}

// defaultShellWaitSeconds is how long shell waits for a command that does
// not say.
const defaultShellWaitSeconds = 120

type ShellInput struct {
	Command     string `json:"command" jsonschema:"required" jsonschema_description:"Bash command line to run. It runs in a login shell as the agent user."`
	Dir         string `json:"dir,omitempty" jsonschema_description:"Working directory, absolute or relative to the home directory. Defaults to the home directory."`
	WaitSeconds *int   `json:"wait_seconds,omitempty" jsonschema_description:"Seconds to wait for the command to end, 0 to 600; defaults to 120. Give 0 to start long work, such as a download or a build, and come back to it."`
}

// ShellOutput is a command's job as the wait left it. Output holds the
// beginning and end of what it printed once it ended, its end while it runs.
type ShellOutput struct {
	JobID           string `json:"job_id"`
	State           string `json:"state"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	Note            string `json:"note,omitempty"`
	Seconds         int64  `json:"seconds"`
	Output          string `json:"output"`
	OutputTruncated bool   `json:"output_truncated,omitempty"`
	OutputPath      string `json:"output_path"`
}

type JobInput struct {
	Action string `json:"action" jsonschema:"required,enum=list,enum=status,enum=output,enum=stop" jsonschema_description:"list shows the newest jobs; status one job; output reads a job's output from offset; stop ends a running job and everything it started."`
	JobID  string `json:"job_id,omitempty" jsonschema_description:"The job, as shell returned it; required except for list."`
	Offset int64  `json:"offset,omitempty" jsonschema_description:"Byte offset to read the output from; output only."`
}

// JobView is a job as the agent sees it.
type JobView struct {
	ID          string     `json:"id"`
	Command     string     `json:"command"`
	State       string     `json:"state"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	Note        string     `json:"note,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	OutputBytes int64      `json:"output_bytes"`
	OutputPath  string     `json:"output_path"`
}

type JobToolOutput struct {
	Jobs       []JobView `json:"jobs,omitempty"`
	Job        *JobView  `json:"job,omitempty"`
	Text       string    `json:"text,omitempty"`
	Offset     int64     `json:"offset,omitempty"`
	NextOffset int64     `json:"next_offset,omitempty"`
}

type FilesInput struct {
	Action  string     `json:"action" jsonschema:"required,enum=read,enum=write,enum=edit" jsonschema_description:"read returns a text file; write creates or replaces one, creating parent directories; edit changes parts of an existing one and leaves the rest as it is."`
	Path    string     `json:"path" jsonschema:"required" jsonschema_description:"File path, absolute or relative to the home directory."`
	Content string     `json:"content,omitempty" jsonschema_description:"Full file content for write."`
	Edits   []TextEdit `json:"edits,omitempty" jsonschema_description:"For edit: the changes, all made against the file as it is now. Each old text must occur exactly once in the file, and edits must not overlap; either all apply or none does."`
}

// TextEdit replaces one piece of a text.
type TextEdit struct {
	Old string `json:"old" jsonschema:"required" jsonschema_description:"Text to replace, copied from the file with enough around it to occur only once. Whitespace at line ends and typographic quotes and dashes may differ."`
	New string `json:"new" jsonschema:"required" jsonschema_description:"Replacement text; empty removes the old text."`
}

func textEdits(edits []TextEdit) []textedit.Edit {
	out := make([]textedit.Edit, 0, len(edits))
	for _, e := range edits {
		out = append(out, textedit.Edit{Old: e.Old, New: e.New})
	}
	return out
}

type SendFileInput struct {
	Path string `json:"path" jsonschema:"required" jsonschema_description:"File on your workstation, absolute or relative to the home directory."`
	Name string `json:"name,omitempty" jsonschema_description:"Name the owner sees, such as 红楼梦.epub. Defaults to the file's own name."`
}

type SendFileOutput struct {
	FileID    string `json:"file_id"`
	Name      string `json:"name"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

type FilesOutput struct {
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
	// Lines is where each edit's new text starts, counted from 1.
	Lines []int `json:"lines,omitempty"`
}

// workstationToolNames are the tools that work on the agent's workstation.
var workstationToolNames = []string{"shell", "job", "follow_up", "files", "send_file"}

// WorkstationTool reports whether the named local tool needs the workstation.
func WorkstationTool(name string) bool {
	return slices.Contains(workstationToolNames, name)
}

// BuildWorkstationToolSpecs returns the specs for shell, job, follow_up,
// files and send_file.
func BuildWorkstationToolSpecs(cfg WorkstationToolsConfig) ([]core.ToolSpec, error) {
	if cfg.Service == nil {
		var specs []core.ToolSpec
		for _, name := range workstationToolNames {
			spec, err := disabledLocalSpec(name, cfg.DisabledReason)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
		return specs, nil
	}
	if cfg.MaxOutputBytes < 1 || cfg.MaxSendBytes < 1 {
		return nil, errors.New("workstation tools need a positive MaxOutputBytes and MaxSendBytes")
	}
	if cfg.Files == nil || cfg.FollowUps == nil || cfg.Context == nil || cfg.Settings == nil || cfg.Clock == nil {
		return nil, errors.New("workstation tools need thread files, follow-ups, the tool call context, settings and a clock")
	}
	shell, err := buildShellTool(cfg)
	if err != nil {
		return nil, err
	}
	job, err := buildJobTool(cfg)
	if err != nil {
		return nil, err
	}
	followUp, err := buildFollowUpTool(cfg)
	if err != nil {
		return nil, err
	}
	files, err := buildFilesTool(cfg)
	if err != nil {
		return nil, err
	}
	send, err := buildSendFileTool(cfg)
	if err != nil {
		return nil, err
	}
	return []core.ToolSpec{enabledLocalSpec("shell", shell), enabledLocalSpec("job", job), enabledLocalSpec("follow_up", followUp), enabledLocalSpec("files", files), enabledLocalSpec("send_file", send)}, nil
}

func buildShellTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	description := fmt.Sprintf("Run a bash command on your own Linux workstation (Debian, user agent with passwordless sudo, internet access). Files under the home directory persist; system packages installed with apt are lost when the workstation is rebuilt. "+
		"Every command runs as a job: shell waits up to wait_seconds for it, and a command still running then keeps running in the background, for up to %d hours, with its job_id. "+
		"Output is stdout and stderr together; the whole of it stays in output_path, and the result shows its beginning and end. Check, read or stop a job with the job tool, and set a follow_up to be woken when it ends.", workstation.MaxJobSeconds/3600)
	return inferLocalTool("shell", description, func(ctx context.Context, input ShellInput) (ShellOutput, error) {
		if strings.TrimSpace(input.Command) == "" {
			return ShellOutput{}, errors.New("shell: command is required")
		}
		wait := defaultShellWaitSeconds
		if input.WaitSeconds != nil {
			wait = *input.WaitSeconds
		}
		if wait < 0 || wait > workstation.MaxJobWaitSeconds {
			return ShellOutput{}, fmt.Errorf("shell: wait_seconds must be between 0 and %d", workstation.MaxJobWaitSeconds)
		}
		got, err := cfg.Service.StartJob(ctx, workstation.JobStartRequest{
			Command:        input.Command,
			Dir:            input.Dir,
			WaitSeconds:    wait,
			MaxOutputBytes: cfg.MaxOutputBytes,
		})
		if err != nil {
			return ShellOutput{}, err
		}
		job := got.Job
		end := time.Now()
		if job.EndedAt != nil {
			end = *job.EndedAt
		}
		return ShellOutput{
			JobID:           job.ID,
			State:           job.State,
			ExitCode:        job.ExitCode,
			Note:            job.Note,
			Seconds:         int64(end.Sub(job.StartedAt).Seconds()),
			Output:          got.Output,
			OutputTruncated: got.OutputTruncated,
			OutputPath:      job.OutputPath,
		}, nil
	})
}

func buildJobTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	description := fmt.Sprintf("Check on the jobs shell started on your workstation: list the newest, see one's state, read its output %d bytes at a time from an offset, or stop it. A job is running, exited (with its exit code), stopped, or lost when the workstation restarted.", cfg.MaxOutputBytes)
	return inferLocalTool("job", description, func(ctx context.Context, input JobInput) (JobToolOutput, error) {
		if input.Action != "list" && strings.TrimSpace(input.JobID) == "" {
			return JobToolOutput{}, fmt.Errorf("job: job_id is required for %s", input.Action)
		}
		switch input.Action {
		case "list":
			jobs, err := cfg.Service.Jobs(ctx)
			if err != nil {
				return JobToolOutput{}, err
			}
			out := JobToolOutput{Jobs: []JobView{}}
			for _, job := range jobs {
				out.Jobs = append(out.Jobs, jobView(job))
			}
			return out, nil
		case "status":
			job, err := cfg.Service.Job(ctx, input.JobID)
			if err != nil {
				return JobToolOutput{}, err
			}
			view := jobView(job)
			return JobToolOutput{Job: &view}, nil
		case "output":
			got, err := cfg.Service.JobOutput(ctx, input.JobID, input.Offset, cfg.MaxOutputBytes)
			if err != nil {
				return JobToolOutput{}, err
			}
			view := jobView(got.Job)
			return JobToolOutput{Job: &view, Text: got.Text, Offset: got.Offset, NextOffset: got.NextOffset}, nil
		case "stop":
			job, err := cfg.Service.StopJob(ctx, input.JobID)
			if err != nil {
				return JobToolOutput{}, err
			}
			view := jobView(job)
			return JobToolOutput{Job: &view}, nil
		default:
			return JobToolOutput{}, fmt.Errorf("job: action must be list, status, output or stop, got %q", input.Action)
		}
	})
}

func jobView(job workstation.Job) JobView {
	return JobView{ID: job.ID, Command: job.Command, State: job.State, ExitCode: job.ExitCode, Note: job.Note, StartedAt: job.StartedAt, EndedAt: job.EndedAt, OutputBytes: job.OutputBytes, OutputPath: job.OutputPath}
}

func buildFilesTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	description := fmt.Sprintf("Read, write or edit a UTF-8 text file on your workstation. read returns at most %d bytes and says when the file is longer. To change part of a file, such as a line of a script or a setting, use edit instead of writing the whole file again; line endings stay as they were. Use shell for other files and listings.", cfg.MaxOutputBytes)
	return inferLocalTool("files", description, func(ctx context.Context, input FilesInput) (FilesOutput, error) {
		switch input.Action {
		case "read":
			got, err := cfg.Service.ReadFile(ctx, workstation.ReadRequest{Path: input.Path, MaxBytes: cfg.MaxOutputBytes})
			if err != nil {
				return FilesOutput{}, err
			}
			return FilesOutput{Path: got.Path, Content: got.Content, Size: got.Size, Truncated: got.Truncated}, nil
		case "write":
			got, err := cfg.Service.WriteFile(ctx, workstation.WriteRequest{Path: input.Path, Content: input.Content})
			if err != nil {
				return FilesOutput{}, err
			}
			return FilesOutput{Path: got.Path, Bytes: got.Bytes}, nil
		case "edit":
			got, err := cfg.Service.EditFile(ctx, workstation.EditRequest{Path: input.Path, Edits: textEdits(input.Edits)})
			if err != nil {
				return FilesOutput{}, err
			}
			return FilesOutput{Path: got.Path, Bytes: got.Bytes, Lines: got.Lines}, nil
		default:
			return FilesOutput{}, fmt.Errorf("files: action must be read, write or edit, got %q", input.Action)
		}
	})
}

func buildSendFileTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	description := fmt.Sprintf("Send the owner a file from your workstation, such as a book or document you downloaded or a file you made. It appears under your reply in this conversation, where the owner can open it or save it to their phone; images show inline. Files are at most %d MiB. Say in your reply what you sent.", cfg.MaxSendBytes>>20)
	return inferLocalTool("send_file", description, func(ctx context.Context, input SendFileInput) (SendFileOutput, error) {
		if strings.TrimSpace(input.Path) == "" {
			return SendFileOutput{}, errors.New("send_file: path is required")
		}
		sessionID, runID := strings.TrimSpace(cfg.Context.CurrentSessionID(ctx)), strings.TrimSpace(cfg.Context.CurrentRunID(ctx))
		if sessionID == "" || runID == "" {
			return SendFileOutput{}, errors.New("send_file needs the current conversation and run")
		}
		content, size, err := cfg.Service.OpenFile(ctx, input.Path)
		if err != nil {
			return SendFileOutput{}, err
		}
		defer content.Close()
		if size > cfg.MaxSendBytes {
			return SendFileOutput{}, fmt.Errorf("send_file: %s is %d bytes, more than the %d MiB a file can be", input.Path, size, cfg.MaxSendBytes>>20)
		}
		name := input.Name
		if strings.TrimSpace(name) == "" {
			name = path.Base(input.Path)
		}
		file, err := cfg.Files.SaveThreadFile(ctx, core.ThreadFileWrite{SessionID: sessionID, RunID: runID, Origin: core.ThreadFileFromAgent, Name: name, Content: content, MaxBytes: cfg.MaxSendBytes})
		if err != nil {
			return SendFileOutput{}, err
		}
		return SendFileOutput{FileID: file.ID, Name: file.Name, MIMEType: file.MIME, SizeBytes: file.Size}, nil
	})
}
