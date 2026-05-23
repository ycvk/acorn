package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/terminalsession"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/toolresult"
)

type TerminalSessionContext interface {
	CurrentRunID(context.Context) string
	CurrentSessionID(context.Context) string
	CurrentToolCallID(context.Context) string
}

type TerminalSessionStartInput struct {
	Command     []string `json:"command,omitempty" jsonschema:"description=Command argv to start as a persistent Acorn-owned process."`
	Shell       bool     `json:"shell,omitempty" jsonschema:"description=Start the user's shell when command is omitted."`
	Cwd         string   `json:"cwd,omitempty" jsonschema:"description=Workspace-relative or absolute working directory."`
	Label       string   `json:"label,omitempty" jsonschema:"description=Short human-readable session label."`
	Interactive bool     `json:"interactive,omitempty" jsonschema:"description=Whether the process expects stdin writes."`
	PTY         bool     `json:"pty,omitempty" jsonschema:"description=Use a pseudo-terminal and read the pty stream."`
}

type TerminalSessionWriteInput struct {
	TerminalSessionID string `json:"terminal_session_id" jsonschema:"description=Opaque terminal session id returned by terminal_session_start."`
	Input             string `json:"input" jsonschema:"description=Bytes to write to session stdin or pty."`
}

type TerminalSessionReadInput struct {
	TerminalSessionID string `json:"terminal_session_id" jsonschema:"description=Opaque terminal session id."`
	Stream            string `json:"stream" jsonschema:"description=Log stream: stdout, stderr, or pty."`
	Offset            int64  `json:"offset" jsonschema:"description=Zero-based byte offset."`
	Limit             int64  `json:"limit" jsonschema:"description=Maximum bytes to read. Must be greater than zero."`
}

type TerminalSessionSignalInput struct {
	TerminalSessionID string `json:"terminal_session_id" jsonschema:"description=Opaque terminal session id."`
	Signal            string `json:"signal" jsonschema:"description=Signal name: TERM, INT, KILL, or HUP."`
}

type TerminalSessionCloseInput struct {
	TerminalSessionID string `json:"terminal_session_id" jsonschema:"description=Opaque terminal session id."`
	Force             bool   `json:"force,omitempty" jsonschema:"description=Use SIGKILL instead of SIGTERM."`
}

type TerminalSessionListInput struct {
	RunID string `json:"run_id,omitempty" jsonschema:"description=Optional run id. Defaults to current run."`
}

type ProcessStatusInput struct {
	TerminalSessionID string `json:"terminal_session_id" jsonschema:"description=Opaque terminal session id."`
}

type TerminalSessionSummary struct {
	TerminalSessionID string `json:"terminal_session_id"`
	RunID             string `json:"run_id"`
	SessionID         string `json:"session_id,omitempty"`
	Label             string `json:"label,omitempty"`
	Command           string `json:"command"`
	Cwd               string `json:"cwd"`
	Interactive       bool   `json:"interactive"`
	PTY               bool   `json:"pty"`
	Status            string `json:"status"`
	ProcessID         *int   `json:"pid,omitempty"`
	ProcessGroupID    *int   `json:"process_group_id,omitempty"`
	ExitCode          *int   `json:"exit_code,omitempty"`
	Signal            string `json:"signal,omitempty"`
	StdoutArtifactID  string `json:"stdout_artifact_id,omitempty"`
	StderrArtifactID  string `json:"stderr_artifact_id,omitempty"`
	PTYArtifactID     string `json:"pty_artifact_id,omitempty"`
	StartedAt         string `json:"started_at,omitempty"`
	EndedAt           string `json:"ended_at,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type TerminalSessionStartOutput struct {
	TerminalSessionSummary
}

type TerminalSessionWriteOutput struct {
	TerminalSessionSummary
	BytesWritten int `json:"bytes_written"`
}

type TerminalSessionReadOutput struct {
	TerminalSessionSummary
	Stream     string `json:"stream"`
	ArtifactID string `json:"artifact_id,omitempty"`
	Offset     int64  `json:"offset"`
	Bytes      int    `json:"bytes"`
	SizeBytes  int64  `json:"size_bytes"`
	EOF        bool   `json:"eof"`
	Content    string `json:"content"`
}

type TerminalSessionSignalOutput struct {
	TerminalSessionSummary
	SignalSent string `json:"signal_sent"`
}

type TerminalSessionCloseOutput struct {
	TerminalSessionSummary
	SignalSent string `json:"signal_sent"`
}

type TerminalSessionListOutput struct {
	RunID string                   `json:"run_id"`
	Items []TerminalSessionSummary `json:"items"`
}

type ProcessStatusOutput struct {
	TerminalSessionSummary
}

func buildTerminalSessionTools(service TerminalService, ws WorkspaceView, bridge TerminalSessionContext) ([]einotool.BaseTool, error) {
	if service == nil {
		return nil, errors.New("terminal session service is required")
	}
	if ws == nil {
		return nil, errors.New("workspace is required for terminal session tools")
	}
	if bridge == nil {
		return nil, errors.New("terminal session context bridge is required")
	}
	builders := []func(TerminalService, WorkspaceView, TerminalSessionContext) (einotool.BaseTool, error){
		buildTerminalSessionStartTool,
		buildTerminalSessionWriteTool,
		buildTerminalSessionReadTool,
		buildTerminalSessionSignalTool,
		buildTerminalSessionCloseTool,
		buildTerminalSessionListTool,
		buildProcessStatusTool,
	}
	tools := make([]einotool.BaseTool, 0, len(builders))
	for _, builder := range builders {
		tool, err := builder(service, ws, bridge)
		if err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func buildTerminalSessionStartTool(service TerminalService, ws WorkspaceView, bridge TerminalSessionContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("terminal_session_start", "Start a persistent Acorn-owned terminal/process session.", func(ctx context.Context, input TerminalSessionStartInput, emit tooling.ToolProgressEmitter) (TerminalSessionStartOutput, error) {
		runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
		if runID == "" {
			return TerminalSessionStartOutput{}, errors.New("terminal_session_start requires current run context")
		}
		callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
		if callID == "" {
			return TerminalSessionStartOutput{}, errors.New("terminal_session_start requires current tool call context")
		}
		command := append([]string(nil), input.Command...)
		if len(command) == 0 && input.Shell {
			command = defaultShellCommand()
		}
		if len(command) == 0 {
			return TerminalSessionStartOutput{}, errors.New("terminal_session_start command is required unless shell=true")
		}
		cwd, err := ws.ResolveCwd(input.Cwd)
		if err != nil {
			return TerminalSessionStartOutput{}, err
		}
		record, err := service.Start(ctx, terminalsession.StartRequest{
			RunID:               runID,
			SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
			SourceToolResultRef: toolresult.BuildRef(runID, callID),
			Label:               input.Label,
			Command:             command,
			Cwd:                 cwd,
			Env:                 filterWhitelistedEnv(os.Environ(), ws.RunCommandEnvWhitelist()),
			Interactive:         input.Interactive,
			PTY:                 input.PTY,
		})
		if err != nil {
			return TerminalSessionStartOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("started terminal session %s pid %s", record.TerminalSessionID, formatOptionalInt(record.ProcessID))); err != nil {
			return TerminalSessionStartOutput{}, err
		}
		return TerminalSessionStartOutput{TerminalSessionSummary: terminalSessionSummaryFromRecord(record)}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build terminal_session_start tool: %w", err)
	}
	return tool, nil
}

func buildTerminalSessionWriteTool(service TerminalService, _ WorkspaceView, _ TerminalSessionContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("terminal_session_write", "Write input to a running terminal/process session.", func(ctx context.Context, input TerminalSessionWriteInput, emit tooling.ToolProgressEmitter) (TerminalSessionWriteOutput, error) {
		result, err := service.Write(ctx, terminalsession.WriteRequest{TerminalSessionID: input.TerminalSessionID, Input: input.Input})
		if err != nil {
			return TerminalSessionWriteOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("wrote %d bytes to terminal session %s", result.BytesWritten, result.Record.TerminalSessionID)); err != nil {
			return TerminalSessionWriteOutput{}, err
		}
		return TerminalSessionWriteOutput{TerminalSessionSummary: terminalSessionSummaryFromRecord(result.Record), BytesWritten: result.BytesWritten}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build terminal_session_write tool: %w", err)
	}
	return tool, nil
}

func buildTerminalSessionReadTool(service TerminalService, _ WorkspaceView, _ TerminalSessionContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("terminal_session_read", "Read a bounded range from a terminal/process session log.", func(ctx context.Context, input TerminalSessionReadInput, emit tooling.ToolProgressEmitter) (TerminalSessionReadOutput, error) {
		result, err := service.Read(ctx, terminalsession.ReadRequest{
			TerminalSessionID: input.TerminalSessionID,
			Stream:            terminalsession.LogStream(strings.TrimSpace(input.Stream)),
			Offset:            input.Offset,
			Limit:             input.Limit,
		})
		if err != nil {
			return TerminalSessionReadOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("read %d bytes from terminal session %s %s", result.Bytes, result.Record.TerminalSessionID, result.Stream)); err != nil {
			return TerminalSessionReadOutput{}, err
		}
		return TerminalSessionReadOutput{
			TerminalSessionSummary: terminalSessionSummaryFromRecord(result.Record),
			Stream:                 string(result.Stream),
			ArtifactID:             result.ArtifactID,
			Offset:                 result.Offset,
			Bytes:                  result.Bytes,
			SizeBytes:              result.SizeBytes,
			EOF:                    result.EOF,
			Content:                string(result.Content),
		}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build terminal_session_read tool: %w", err)
	}
	return tool, nil
}

func buildTerminalSessionSignalTool(service TerminalService, _ WorkspaceView, _ TerminalSessionContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("terminal_session_signal", "Send a signal to a running terminal/process session process group.", func(ctx context.Context, input TerminalSessionSignalInput, emit tooling.ToolProgressEmitter) (TerminalSessionSignalOutput, error) {
		signalName := strings.ToUpper(strings.TrimSpace(input.Signal))
		record, err := service.Signal(ctx, terminalsession.SignalRequest{TerminalSessionID: input.TerminalSessionID, Signal: signalName})
		if err != nil {
			return TerminalSessionSignalOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("sent %s to terminal session %s", signalName, record.TerminalSessionID)); err != nil {
			return TerminalSessionSignalOutput{}, err
		}
		return TerminalSessionSignalOutput{TerminalSessionSummary: terminalSessionSummaryFromRecord(record), SignalSent: signalName}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build terminal_session_signal tool: %w", err)
	}
	return tool, nil
}

func buildTerminalSessionCloseTool(service TerminalService, _ WorkspaceView, _ TerminalSessionContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("terminal_session_close", "Close a running terminal/process session by signaling its process group.", func(ctx context.Context, input TerminalSessionCloseInput, emit tooling.ToolProgressEmitter) (TerminalSessionCloseOutput, error) {
		signalName := "TERM"
		if input.Force {
			signalName = "KILL"
		}
		record, err := service.Close(ctx, input.TerminalSessionID, input.Force)
		if err != nil {
			return TerminalSessionCloseOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("sent %s to terminal session %s", signalName, record.TerminalSessionID)); err != nil {
			return TerminalSessionCloseOutput{}, err
		}
		return TerminalSessionCloseOutput{TerminalSessionSummary: terminalSessionSummaryFromRecord(record), SignalSent: signalName}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build terminal_session_close tool: %w", err)
	}
	return tool, nil
}

func buildTerminalSessionListTool(service TerminalService, _ WorkspaceView, bridge TerminalSessionContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("terminal_session_list", "List terminal/process sessions for a run.", func(ctx context.Context, input TerminalSessionListInput, emit tooling.ToolProgressEmitter) (TerminalSessionListOutput, error) {
		runID := strings.TrimSpace(input.RunID)
		if runID == "" {
			runID = strings.TrimSpace(bridge.CurrentRunID(ctx))
		}
		if runID == "" {
			return TerminalSessionListOutput{}, errors.New("terminal_session_list requires run_id or current run context")
		}
		records, err := service.ListByRun(ctx, runID)
		if err != nil {
			return TerminalSessionListOutput{}, err
		}
		items := make([]TerminalSessionSummary, 0, len(records))
		for _, record := range records {
			items = append(items, terminalSessionSummaryFromRecord(record))
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("listed %d terminal sessions", len(items))); err != nil {
			return TerminalSessionListOutput{}, err
		}
		return TerminalSessionListOutput{RunID: runID, Items: items}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build terminal_session_list tool: %w", err)
	}
	return tool, nil
}

func buildProcessStatusTool(service TerminalService, _ WorkspaceView, _ TerminalSessionContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("process_status", "Inspect status for an Acorn-owned terminal/process session.", func(ctx context.Context, input ProcessStatusInput, emit tooling.ToolProgressEmitter) (ProcessStatusOutput, error) {
		record, err := service.Load(ctx, input.TerminalSessionID)
		if err != nil {
			return ProcessStatusOutput{}, err
		}
		if err := emitToolProgress(ctx, emit, fmt.Sprintf("terminal session %s status %s", record.TerminalSessionID, record.Status)); err != nil {
			return ProcessStatusOutput{}, err
		}
		return ProcessStatusOutput{TerminalSessionSummary: terminalSessionSummaryFromRecord(record)}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build process_status tool: %w", err)
	}
	return tool, nil
}

func terminalSessionSummaryFromRecord(record terminalsession.SessionRecord) TerminalSessionSummary {
	return TerminalSessionSummary{
		TerminalSessionID: record.TerminalSessionID,
		RunID:             record.RunID,
		SessionID:         record.SessionID,
		Label:             record.Label,
		Command:           compactCommandJSON(record.CommandJSON),
		Cwd:               record.Cwd,
		Interactive:       record.Interactive,
		PTY:               record.PTY,
		Status:            string(record.Status),
		ProcessID:         copyOptionalInt(record.ProcessID),
		ProcessGroupID:    copyOptionalInt(record.ProcessGroupID),
		ExitCode:          copyOptionalInt(record.ExitCode),
		Signal:            record.Signal,
		StdoutArtifactID:  record.StdoutArtifactID,
		StderrArtifactID:  record.StderrArtifactID,
		PTYArtifactID:     record.PTYArtifactID,
		StartedAt:         formatOptionalTime(record.StartedAt),
		EndedAt:           formatOptionalTime(record.EndedAt),
		CreatedAt:         formatTime(record.CreatedAt),
		UpdatedAt:         formatTime(record.UpdatedAt),
	}
}

func defaultShellCommand() []string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return []string{shell}
	}
	return []string{"/bin/sh"}
}

func compactCommandJSON(raw string) string {
	var command []string
	if err := json.Unmarshal([]byte(raw), &command); err == nil && len(command) > 0 {
		return strings.Join(command, " ")
	}
	return raw
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func copyOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	return new(*value)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTime(*value)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}
