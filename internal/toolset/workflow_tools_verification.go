package toolset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/toolkit"
)

type verificationCommandResult struct {
	stdout   string
	stderr   string
	exitCode int
	status   string
	timedOut bool
}

func normalizeVerificationKind(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "test", "lint", "build", "format_check", "custom":
		return strings.TrimSpace(value), nil
	default:
		return "", fmt.Errorf("verification kind %q is invalid", strings.TrimSpace(value))
	}
}

func normalizeCommand(command []string) ([]string, error) {
	if len(command) == 0 {
		return nil, errors.New("command is required")
	}
	normalized := make([]string, 0, len(command))
	for index, item := range command {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			return nil, errors.New("command arguments must not be empty")
		}
		if index == 0 {
			normalized = append(normalized, trimmed)
			continue
		}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func normalizeVerificationPaths(ws WorkspaceView, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	return normalizePatchPaths(ws, values)
}

func verificationSummary(kind string, status string, exitCode int) string {
	return fmt.Sprintf("%s verification %s with exit code %d", kind, status, exitCode)
}

func writeWorkflowArtifact(ctx context.Context, service ArtifactService, bridge domain.ToolCallContextBridge, kind store.ArtifactKind, title string, mimeType string, content string) (store.ArtifactRecord, error) {
	runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
	if runID == "" {
		return store.ArtifactRecord{}, errors.New("workflow artifact write requires current run context")
	}
	callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
	if callID == "" {
		return store.ArtifactRecord{}, errors.New("workflow artifact write requires current tool call context")
	}
	return service.Write(ctx, store.ArtifactWriteRequest{
		RunID:               runID,
		SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
		SourceToolResultRef: "tool_result:" + runID + ":" + callID,
		Kind:                kind,
		Title:               title,
		MIMEType:            mimeType,
		Content:             []byte(content),
	})
}

func executeVerificationCommand(ctx context.Context, ws WorkspaceView, command []string, cwd string, timeoutSeconds int, emit toolkit.ToolProgressEmitter) (verificationCommandResult, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = ws.RunCommandDefaultTimeout()
	}
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	cmd, stdoutBuf, stderrBuf := buildVerificationCommand(ws, command, cwd, ctx, emit)
	if err := cmd.Start(); err != nil {
		return verificationCommandResult{}, fmt.Errorf("start verification command %v: %w", command, err)
	}
	return waitVerificationCommand(execCtx, cmd, command, stdoutBuf, stderrBuf)
}

func buildVerificationCommand(ws WorkspaceView, command []string, cwd string, ctx context.Context, emit toolkit.ToolProgressEmitter) (*exec.Cmd, *runCommandProgressBuffer, *runCommandProgressBuffer) {
	cmd := exec.Command(command[0], command[1:]...)
	ConfigureCommand(cmd)
	cmd.Dir = cwd
	cmd.Env = filterWhitelistedEnv(os.Environ(), ws.RunCommandEnvWhitelist())
	stdoutBuf := newRunCommandProgressBuffer(ctx, emit)
	stderrBuf := newRunCommandProgressBuffer(ctx, emit)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	return cmd, stdoutBuf, stderrBuf
}

func waitVerificationCommand(execCtx context.Context, cmd *exec.Cmd, command []string, stdoutBuf, stderrBuf *runCommandProgressBuffer) (verificationCommandResult, error) {
	waitCh := spawnVerificationWait(cmd)
	select {
	case waitErr := <-waitCh:
		if err := errors.Join(stdoutBuf.Err(), stderrBuf.Err()); err != nil {
			return verificationCommandResult{}, err
		}
		return verificationCommandResultFromWait(command, stdoutBuf.String(), stderrBuf.String(), waitErr)
	case <-execCtx.Done():
		return verificationResultOnTimeout(execCtx, cmd, waitCh, command, stdoutBuf, stderrBuf)
	}
}

func spawnVerificationWait(cmd *exec.Cmd) chan error {
	waitCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				waitCh <- fmt.Errorf("verification command wait panic: %v", r)
			}
		}()
		waitCh <- cmd.Wait()
	}()
	return waitCh
}

func verificationResultOnTimeout(ctx context.Context, cmd *exec.Cmd, waitCh chan error, command []string, stdoutBuf, stderrBuf *runCommandProgressBuffer) (verificationCommandResult, error) {
	killErr := KillCommandGroup(cmd)
	waitErr := <-waitCh
	if err := errors.Join(stdoutBuf.Err(), stderrBuf.Err()); err != nil {
		return verificationCommandResult{}, err
	}
	if killErr != nil {
		return verificationCommandResult{}, errors.Join(ctx.Err(), killErr)
	}
	if waitErr != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); !ok || exitErr == nil {
			return verificationCommandResult{}, errors.Join(ctx.Err(), waitErr)
		}
	}
	return verificationCommandResult{
		stdout:   stdoutBuf.String(),
		stderr:   stderrBuf.String(),
		exitCode: -1,
		status:   verificationStatusTimedOut,
		timedOut: true,
	}, nil
}

func verificationCommandResultFromWait(command []string, stdout string, stderr string, waitErr error) (verificationCommandResult, error) {
	result := verificationCommandResult{
		stdout: stdout,
		stderr: stderr,
		status: verificationStatusPassed,
	}
	if waitErr == nil {
		return result, nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
		result.exitCode = exitErr.ExitCode()
		result.status = verificationStatusFailed
		return result, nil
	}
	return verificationCommandResult{}, fmt.Errorf("exec verification command %v: %w", command, waitErr)
}

func appendGitPathspec(args []string, scopedPath string) []string {
	args = append(args, "--")
	if strings.TrimSpace(scopedPath) != "" {
		args = append(args, filepath.ToSlash(strings.TrimSpace(scopedPath)))
	}
	return args
}

func gitSummaryDiffStat(ctx context.Context, ws WorkspaceView, scopedPath string, cached bool) (string, error) {
	args := []string{"diff", "--stat"}
	if cached {
		args = append(args, "--cached")
	}
	args = appendGitPathspec(args, scopedPath)
	worktreeStat, err := runGitCommand(ctx, ws.Root(), args...)
	if err != nil || cached {
		return worktreeStat, err
	}
	cachedArgs := appendGitPathspec([]string{"diff", "--cached", "--stat"}, scopedPath)
	cachedStat, err := runGitCommand(ctx, ws.Root(), cachedArgs...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.Join(trimmedNonEmptyStrings([]string{worktreeStat, cachedStat}), "\n")), nil
}

func gitSummaryDiff(ctx context.Context, ws WorkspaceView, scopedPath string, cached bool, contextLines int) (string, error) {
	args := []string{"diff", "--no-ext-diff", fmt.Sprintf("--unified=%d", contextLines)}
	if cached {
		args = append(args, "--cached")
	}
	args = appendGitPathspec(args, scopedPath)
	worktreeDiff, err := runGitCommand(ctx, ws.Root(), args...)
	if err != nil || cached {
		return worktreeDiff, err
	}
	cachedArgs := appendGitPathspec([]string{"diff", "--no-ext-diff", fmt.Sprintf("--unified=%d", contextLines), "--cached"}, scopedPath)
	cachedDiff, err := runGitCommand(ctx, ws.Root(), cachedArgs...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.Join(trimmedNonEmptyStrings([]string{worktreeDiff, cachedDiff}), "\n")), nil
}

func trimmedNonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	}
	return out
}
