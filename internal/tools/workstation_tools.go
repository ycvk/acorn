package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

// WorkstationService is the subset of the workstation client the shell,
// files and send_file tools need.
type WorkstationService interface {
	Exec(ctx context.Context, req workstation.ExecRequest) (workstation.ExecResult, error)
	ReadFile(ctx context.Context, req workstation.ReadRequest) (workstation.ReadResult, error)
	WriteFile(ctx context.Context, req workstation.WriteRequest) (workstation.WriteResult, error)
	OpenFile(ctx context.Context, path string) (io.ReadCloser, int64, error)
}

// WorkstationToolsConfig carries the workstation behind shell, files and
// send_file, and the thread files send_file stores into. A nil Service
// disables the three tools, and DisabledReason says why.
type WorkstationToolsConfig struct {
	Service               WorkstationService
	Files                 core.ThreadFiles
	Context               core.ToolCallContextBridge
	CommandTimeoutSeconds int
	MaxOutputBytes        int
	MaxSendBytes          int64
	DisabledReason        string
}

type ShellInput struct {
	Command        string `json:"command" jsonschema:"required" jsonschema_description:"Bash command line to run. It runs in a login shell as the agent user."`
	Dir            string `json:"dir,omitempty" jsonschema_description:"Working directory, absolute or relative to the home directory. Defaults to the home directory."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema_description:"Seconds before the command and everything it started are killed. Defaults to and cannot exceed the configured limit."`
}

type FilesInput struct {
	Action  string `json:"action" jsonschema:"required,enum=read,enum=write" jsonschema_description:"read returns a text file; write creates or replaces one, creating parent directories."`
	Path    string `json:"path" jsonschema:"required" jsonschema_description:"File path, absolute or relative to the home directory."`
	Content string `json:"content,omitempty" jsonschema_description:"Full file content for write."`
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
}

// BuildWorkstationToolSpecs returns the specs for shell and files.
func BuildWorkstationToolSpecs(cfg WorkstationToolsConfig) ([]core.ToolSpec, error) {
	if cfg.Service == nil {
		var specs []core.ToolSpec
		for _, name := range []string{"shell", "files", "send_file"} {
			spec, err := disabledLocalSpec(name, cfg.DisabledReason)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
		return specs, nil
	}
	if cfg.CommandTimeoutSeconds < 1 || cfg.MaxOutputBytes < 1 || cfg.MaxSendBytes < 1 {
		return nil, errors.New("workstation tools need a positive CommandTimeoutSeconds, MaxOutputBytes and MaxSendBytes")
	}
	if cfg.Files == nil || cfg.Context == nil {
		return nil, errors.New("workstation tools need thread files and the tool call context")
	}
	shell, err := buildShellTool(cfg)
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
	return []core.ToolSpec{enabledLocalSpec("shell", shell), enabledLocalSpec("files", files), enabledLocalSpec("send_file", send)}, nil
}

func buildShellTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	description := fmt.Sprintf("Run a bash command on your own Linux workstation (Debian, user agent with passwordless sudo, internet access). Files under the home directory persist; system packages installed with apt are lost when the workstation is rebuilt. Commands time out after %d seconds at most. stdout and stderr each keep their beginning and end when long.", cfg.CommandTimeoutSeconds)
	return inferProgressTool("shell", description, func(ctx context.Context, input ShellInput, _ ToolProgressEmitter) (workstation.ExecResult, error) {
		if strings.TrimSpace(input.Command) == "" {
			return workstation.ExecResult{}, errors.New("shell: command is required")
		}
		timeout := cfg.CommandTimeoutSeconds
		if input.TimeoutSeconds > 0 && input.TimeoutSeconds < timeout {
			timeout = input.TimeoutSeconds
		}
		return cfg.Service.Exec(ctx, workstation.ExecRequest{
			Command:        input.Command,
			Dir:            input.Dir,
			TimeoutSeconds: timeout,
			MaxOutputBytes: cfg.MaxOutputBytes,
		})
	})
}

func buildFilesTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	description := fmt.Sprintf("Read or write a UTF-8 text file on your workstation. read returns at most %d bytes and says when the file is longer; use shell for other files and listings.", cfg.MaxOutputBytes)
	return inferProgressTool("files", description, func(ctx context.Context, input FilesInput, _ ToolProgressEmitter) (FilesOutput, error) {
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
		default:
			return FilesOutput{}, fmt.Errorf("files: action must be read or write, got %q", input.Action)
		}
	})
}

func buildSendFileTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	description := fmt.Sprintf("Send the owner a file from your workstation, such as a book or document you downloaded or a file you made. It appears under your reply in this conversation, where the owner can open it or save it to their phone; images show inline. Files are at most %d MiB. Say in your reply what you sent.", cfg.MaxSendBytes>>20)
	return inferProgressTool("send_file", description, func(ctx context.Context, input SendFileInput, _ ToolProgressEmitter) (SendFileOutput, error) {
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
