package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/workspace"
)

func buildRunVerificationTool(ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge) (einotool.BaseTool, error) {
	if service == nil {
		return nil, errors.New("artifact service is required")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required")
	}
	tool, err := inferProgressTool("run_verification", "Run a verification command and persist stdout/stderr as artifacts with normalized status.", func(ctx context.Context, input RunVerificationInput, emit ToolProgressEmitter) (RunVerificationOutput, error) {
		return runVerification(ctx, ws, service, bridge, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build run_verification tool: %w", err)
	}
	return tool, nil
}

func runVerification(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, input RunVerificationInput, emit ToolProgressEmitter) (RunVerificationOutput, error) {
	kind, err := normalizeVerificationKind(input.Kind)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	command, err := normalizeCommand(input.Command)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	cwd, err := ws.ResolveCwd(input.Cwd)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	paths, err := normalizeVerificationPaths(ws, input.Paths)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("running %s verification: %s", kind, strings.Join(command, " "))); err != nil {
		return RunVerificationOutput{}, err
	}
	started := time.Now()
	result, err := executeVerificationCommand(ctx, ws, command, cwd, input.TimeoutSeconds, emit)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	return buildVerificationOutput(ctx, ws, service, bridge, emit, input, result, kind, command, cwd, paths, started)
}

func buildVerificationOutput(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, emit ToolProgressEmitter, input RunVerificationInput, result verificationCommandResult, kind string, command []string, cwd string, paths []string, started time.Time) (RunVerificationOutput, error) {
	duration := time.Since(started)
	stdoutArtifact, err := writeWorkflowArtifact(ctx, service, bridge, "log", fmt.Sprintf("%s verification stdout", kind), "text/plain", result.stdout)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	stderrArtifact, err := writeWorkflowArtifact(ctx, service, bridge, "log", fmt.Sprintf("%s verification stderr", kind), "text/plain", result.stderr)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	summary := verificationSummary(kind, result.status, result.exitCode)
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("%s; stdout=%s stderr=%s", summary, stdoutArtifact.ArtifactID, stderrArtifact.ArtifactID)); err != nil {
		return RunVerificationOutput{}, err
	}
	return RunVerificationOutput{
		Kind:             kind,
		Status:           result.status,
		Command:          command,
		Cwd:              cwd,
		ExitCode:         result.exitCode,
		TimedOut:         result.timedOut,
		DurationMS:       duration.Milliseconds(),
		Paths:            paths,
		Summary:          summary,
		StdoutArtifactID: stdoutArtifact.ArtifactID,
		StderrArtifactID: stderrArtifact.ArtifactID,
		StdoutArtifact:   artifactSummaryFromRecord(stdoutArtifact),
		StderrArtifact:   artifactSummaryFromRecord(stderrArtifact),
	}, nil
}

func buildGitSummaryTool(ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("git_summary", "Summarize workspace git status, diffstat, changed paths, and optionally persist a scoped diff artifact.", func(ctx context.Context, input GitSummaryInput, emit ToolProgressEmitter) (GitSummaryOutput, error) {
		return runGitSummary(ctx, ws, service, bridge, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build git_summary tool: %w", err)
	}
	return tool, nil
}

func runGitSummary(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, input GitSummaryInput, emit ToolProgressEmitter) (GitSummaryOutput, error) {
	if input.ContextLines < 0 {
		return GitSummaryOutput{}, errors.New("context_lines must be >= 0")
	}
	scopedPath, err := resolveGitSummaryScopedPath(ws, input.Path)
	if err != nil {
		return GitSummaryOutput{}, err
	}
	status, err := ws.InspectGitStatus(ctx, scopedPath)
	if err != nil {
		return GitSummaryOutput{}, err
	}
	diffStat, err := gitSummaryDiffStat(ctx, ws, scopedPath, input.Cached)
	if err != nil {
		return GitSummaryOutput{}, err
	}
	entries, changedPaths := gitSummaryEntries(status.Entries)
	output := gitSummaryOutput(scopedPath, status, diffStat, entries, changedPaths)
	if err := applyGitSummaryDiff(ctx, ws, service, bridge, emit, input, &output, scopedPath); err != nil {
		return GitSummaryOutput{}, err
	}
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("git summary: %d changed path(s)", len(changedPaths))); err != nil {
		return GitSummaryOutput{}, err
	}
	return output, nil
}

func gitSummaryOutput(scopedPath string, status *workspace.WorkspaceGitStatus, diffStat string, entries []GitStatusEntry, changedPaths []string) GitSummaryOutput {
	return GitSummaryOutput{
		RootPath:     status.WorkspaceRoot,
		Path:         scopedPath,
		Branch:       status.Branch,
		Clean:        status.Clean,
		Entries:      entries,
		ChangedPaths: changedPaths,
		DiffStat:     strings.TrimSpace(diffStat),
	}
}
func resolveGitSummaryScopedPath(ws WorkspaceView, path string) (string, error) {
	scopedPath := strings.TrimSpace(path)
	if scopedPath == "" {
		return "", nil
	}
	return normalizeScopedRelativePath(ws, scopedPath)
}

func gitSummaryEntries(entries []workspace.GitStatusEntry) ([]GitStatusEntry, []string) {
	out := make([]GitStatusEntry, 0, len(entries))
	changedPaths := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out = append(out, GitStatusEntry{
			Path:           entry.Path,
			IndexStatus:    entry.IndexStatus,
			WorktreeStatus: entry.WorktreeStatus,
		})
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		changedPaths = append(changedPaths, path)
	}
	slices.Sort(changedPaths)
	return out, changedPaths
}

func applyGitSummaryDiff(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, emit ToolProgressEmitter, input GitSummaryInput, output *GitSummaryOutput, scopedPath string) error {
	if !input.IncludeDiff {
		return nil
	}
	if service == nil || bridge == nil {
		return errors.New("git_summary include_diff requires artifact service")
	}
	diff, err := gitSummaryDiff(ctx, ws, scopedPath, input.Cached, input.ContextLines)
	if err != nil {
		return err
	}
	record, err := writeWorkflowArtifact(ctx, service, bridge, "diff", "git summary diff", "text/x-diff", diff)
	if err != nil {
		return err
	}
	output.DiffArtifactID = record.ArtifactID
	diffSummary := artifactSummaryFromRecord(record)
	output.DiffArtifact = &diffSummary
	return emitToolProgress(ctx, emit, fmt.Sprintf("wrote diff artifact %s", record.ArtifactID))
}

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

func writeWorkflowArtifact(ctx context.Context, service ArtifactService, bridge domain.ToolCallContextBridge, kind string, title string, mimeType string, content string) (domain.ArtifactRecord, error) {
	runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
	if runID == "" {
		return domain.ArtifactRecord{}, errors.New("workflow artifact write requires current run context")
	}
	callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
	if callID == "" {
		return domain.ArtifactRecord{}, errors.New("workflow artifact write requires current tool call context")
	}
	return service.WriteArtifact(ctx, domain.ArtifactWriteRequest{
		RunID:               runID,
		SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
		SourceToolResultRef: "tool_result:" + runID + ":" + callID,
		Kind:                kind,
		Title:               title,
		MIMEType:            mimeType,
		Content:             []byte(content),
	})
}

func executeVerificationCommand(ctx context.Context, ws WorkspaceView, command []string, cwd string, timeoutSeconds int, emit ToolProgressEmitter) (verificationCommandResult, error) {
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

func buildVerificationCommand(ws WorkspaceView, command []string, cwd string, ctx context.Context, emit ToolProgressEmitter) (*exec.Cmd, *runCommandProgressBuffer, *runCommandProgressBuffer) {
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
