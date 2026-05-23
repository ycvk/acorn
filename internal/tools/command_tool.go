package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/processgroup"
	"github.com/ycvk/acorn/internal/tooling"
)

const runCommandDescription = "Execute a local command as an explicit escape hatch. Set pause_before_exec=true to force an interrupt before execution."

type runCommandTool struct {
	infoSource einotool.BaseTool
	ws         WorkspaceView
}

func buildRunCommandTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := toolutils.InferTool("run_command", runCommandDescription, func(context.Context, RunCommandInput) (RunCommandOutput, error) {
		return RunCommandOutput{}, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build run_command tool: %w", err)
	}
	t := &runCommandTool{
		infoSource: tool,
		ws:         ws,
	}
	return t, nil
}

func (t *runCommandTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.infoSource.Info(ctx)
}

func (t *runCommandTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	return t.InvokableRunWithProgress(ctx, argumentsInJSON, nil, opts...)
}

func (t *runCommandTool) InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit tooling.ToolProgressEmitter, _ ...einotool.Option) (string, error) {
	var input RunCommandInput
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("parse run_command arguments: %w", err)
	}
	output, err := t.run(ctx, input, emit)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("marshal run_command output: %w", err)
	}
	return string(body), nil
}

func (t *runCommandTool) run(ctx context.Context, input RunCommandInput, emit tooling.ToolProgressEmitter) (RunCommandOutput, error) {
	if len(input.Command) == 0 {
		return RunCommandOutput{}, errors.New("command is required")
	}
	commandName := strings.TrimSpace(input.Command[0])
	if commandName == "" {
		return RunCommandOutput{}, errors.New("command name is required")
	}
	cwd := strings.TrimSpace(input.Cwd)
	resolvedCwd, err := t.ws.ResolveCwd(cwd)
	if err != nil {
		return RunCommandOutput{}, err
	}

	if input.PauseBeforeExec {
		wasInterrupted, _, _ := einotool.GetInterruptState[RunCommandInput](ctx)
		if !wasInterrupted {
			info := map[string]any{
				"kind":    "run_command_pause",
				"command": input.Command,
				"cwd":     resolvedCwd,
				"message": "run_command paused before execution; resume this interrupt to continue",
			}
			return RunCommandOutput{}, einotool.StatefulInterrupt(ctx, info, input)
		}
	}

	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = t.ws.RunCommandDefaultTimeout()
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.Command(input.Command[0], input.Command[1:]...)
	processgroup.ConfigureCommand(cmd)
	cmd.Dir = resolvedCwd
	cmd.Env = filterWhitelistedEnv(os.Environ(), t.ws.RunCommandEnvWhitelist())
	stdoutBuf := newRunCommandProgressBuffer(ctx, emit)
	stderrBuf := newRunCommandProgressBuffer(ctx, emit)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		return RunCommandOutput{}, fmt.Errorf("start command %v: %w", input.Command, err)
	}

	waitCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				waitCh <- fmt.Errorf("command wait panic: %v", r)
			}
		}()
		waitCh <- cmd.Wait()
	}()

	select {
	case waitErr := <-waitCh:
		if err := errors.Join(stdoutBuf.Err(), stderrBuf.Err()); err != nil {
			return RunCommandOutput{}, err
		}
		return runCommandResult(input.Command, resolvedCwd, stdoutBuf.String(), stderrBuf.String(), waitErr)
	case <-execCtx.Done():
		killErr := processgroup.KillCommandGroup(cmd)
		waitErr := <-waitCh
		if waitErr != nil {
			exitErr, ok := errors.AsType[*exec.ExitError](waitErr)
			if !ok || exitErr == nil {
				if killErr != nil {
					return RunCommandOutput{}, errors.Join(execCtx.Err(), killErr, waitErr)
				}
				return RunCommandOutput{}, errors.Join(execCtx.Err(), waitErr)
			}
		}
		if killErr != nil {
			return RunCommandOutput{}, errors.Join(execCtx.Err(), killErr)
		}
		return RunCommandOutput{}, execCtx.Err()
	}
}

type runCommandProgressBuffer struct {
	ctx  context.Context
	emit tooling.ToolProgressEmitter
	mu   sync.Mutex
	buf  bytes.Buffer
	err  error
}

func newRunCommandProgressBuffer(ctx context.Context, emit tooling.ToolProgressEmitter) *runCommandProgressBuffer {
	return &runCommandProgressBuffer{ctx: ctx, emit: emit}
}

func (b *runCommandProgressBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, writeErr := b.buf.Write(p)
	if b.emit != nil && len(p) > 0 {
		if err := b.emit(b.ctx, tooling.ToolProgressEvent{Delta: string(p)}); err != nil && b.err == nil {
			b.err = err
		}
	}
	return n, writeErr
}

func (b *runCommandProgressBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *runCommandProgressBuffer) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func filterWhitelistedEnv(all []string, whitelist []string) []string {
	if len(whitelist) == 0 {
		return append([]string(nil), all...)
	}
	allowed := make(map[string]struct{}, len(whitelist))
	for _, name := range whitelist {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	if len(allowed) == 0 {
		return append([]string(nil), all...)
	}
	filtered := make([]string, 0, len(all))
	for _, entry := range all {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, keep := allowed[key]; keep {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func runCommandResult(command []string, cwd, stdout, stderr string, waitErr error) (RunCommandOutput, error) {
	output := RunCommandOutput{
		Command: command,
		Cwd:     cwd,
		Stdout:  stdout,
		Stderr:  stderr,
	}
	if waitErr == nil {
		return output, nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
		output.ExitCode = exitErr.ExitCode()
		return output, nil
	}
	return RunCommandOutput{}, fmt.Errorf("exec command %v: %w", command, waitErr)
}
