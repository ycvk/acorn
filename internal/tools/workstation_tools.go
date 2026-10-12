package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

// WorkstationService is the subset of the workstation client the shell and
// files tools need.
type WorkstationService interface {
	Exec(ctx context.Context, req workstation.ExecRequest) (workstation.ExecResult, error)
	ReadFile(ctx context.Context, req workstation.ReadRequest) (workstation.ReadResult, error)
	WriteFile(ctx context.Context, req workstation.WriteRequest) (workstation.WriteResult, error)
}

// WorkstationToolsConfig carries the workstation behind shell and files. A
// nil Service disables both tools, and DisabledReason says why.
type WorkstationToolsConfig struct {
	Service               WorkstationService
	CommandTimeoutSeconds int
	MaxOutputBytes        int
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
		for _, name := range []string{"shell", "files"} {
			spec, err := disabledLocalSpec(name, cfg.DisabledReason)
			if err != nil {
				return nil, err
			}
			specs = append(specs, spec)
		}
		return specs, nil
	}
	if cfg.CommandTimeoutSeconds < 1 || cfg.MaxOutputBytes < 1 {
		return nil, errors.New("workstation tools need a positive CommandTimeoutSeconds and MaxOutputBytes")
	}
	shell, err := buildShellTool(cfg)
	if err != nil {
		return nil, err
	}
	files, err := buildFilesTool(cfg)
	if err != nil {
		return nil, err
	}
	return []core.ToolSpec{enabledLocalSpec("shell", shell), enabledLocalSpec("files", files)}, nil
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
