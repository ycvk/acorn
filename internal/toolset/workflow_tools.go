package toolset

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
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workspace"
)

const (
	verificationStatusPassed   = "passed"
	verificationStatusFailed   = "failed"
	verificationStatusTimedOut = "timed_out"
)

func buildMultiEditTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("multi_edit", "Atomically replace multiple explicit line ranges across workspace files with one mutation checkpoint.", func(ctx context.Context, input MultiEditInput, emit tools.ToolProgressEmitter) (MultiEditOutput, error) {
		return runMultiEdit(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build multi_edit tool: %w", err)
	}
	return tool, nil
}

func runMultiEdit(ctx context.Context, ws WorkspaceView, input MultiEditInput, emit tools.ToolProgressEmitter) (MultiEditOutput, error) {
	plans, appliedEdits, paths, err := prepareMultiEditPlans(ws, input.Edits)
	if err != nil {
		return MultiEditOutput{}, err
	}
	checkpoint, err := ws.CreateMutationCheckpoint(ctx, workspace.ToolMultiEdit, paths)
	if err != nil {
		return MultiEditOutput{}, err
	}
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("checkpoint %s for %s", checkpoint.CheckpointID, strings.Join(paths, ", "))); err != nil {
		return MultiEditOutput{}, err
	}
	if err := applyMultiEditPlans(plans); err != nil {
		return MultiEditOutput{}, err
	}
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("applied %d edit(s) to %d path(s)", len(appliedEdits), len(paths))); err != nil {
		return MultiEditOutput{}, err
	}
	completed, err := ws.CompleteMutationCheckpoint(ctx, checkpoint.CheckpointID)
	if err != nil {
		return MultiEditOutput{}, err
	}
	if err := emitToolProgress(ctx, emit, fmt.Sprintf("completed checkpoint %s", completed.CheckpointID)); err != nil {
		return MultiEditOutput{}, err
	}
	return multiEditOutput(appliedEdits, paths, completed), nil
}

func multiEditOutput(appliedEdits []MultiEditAppliedSpan, paths []string, completed *workspace.WorkspaceMutationCheckpoint) MultiEditOutput {
	return MultiEditOutput{
		Paths:            append([]string(nil), paths...),
		Edits:            appliedEdits,
		Message:          "ok",
		CheckpointID:     completed.CheckpointID,
		CheckpointPaths:  append([]string(nil), completed.Paths...),
		VerifiedDiffStat: strings.TrimSpace(completed.DiffStat),
	}
}

func applyMultiEditPlans(plans []multiEditPlan) error {
	temps, err := writeMultiEditTemps(plans)
	if err != nil {
		return err
	}
	return commitMultiEditTemps(plans, temps)
}

func writeMultiEditTemps(plans []multiEditPlan) ([]multiEditTempFile, error) {
	temps := make([]multiEditTempFile, 0, len(plans))
	for _, plan := range plans {
		tmp, err := os.CreateTemp(filepath.Dir(plan.resolved), ".acorn-multiedit-*")
		if err != nil {
			cleanupTempFiles(temps)
			return nil, fmt.Errorf("create temp file for %s: %w", plan.path, err)
		}
		tmpPath := tmp.Name()
		if err := writeMultiEditTemp(tmp, tmpPath, plan); err != nil {
			_ = os.Remove(tmpPath)
			cleanupTempFiles(temps)
			return nil, err
		}
		temps = append(temps, multiEditTempFile{target: plan.resolved, path: tmpPath, mode: plan.mode})
	}
	return temps, nil
}

func writeMultiEditTemp(tmp *os.File, tmpPath string, plan multiEditPlan) error {
	if _, err := tmp.Write(plan.after); err != nil {
		return fmt.Errorf("write temp file for %s: %w", plan.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", plan.path, err)
	}
	if err := os.Chmod(tmpPath, plan.mode); err != nil {
		return fmt.Errorf("chmod temp file for %s: %w", plan.path, err)
	}
	return nil
}

func commitMultiEditTemps(plans []multiEditPlan, temps []multiEditTempFile) error {
	applied := make([]multiEditPlan, 0, len(plans))
	for i, plan := range plans {
		if err := os.Rename(temps[i].path, temps[i].target); err != nil {
			return rollbackMultiEditCommit(applied, temps[i:], plan, err)
		}
		applied = append(applied, plan)
	}
	return nil
}

func rollbackMultiEditCommit(applied []multiEditPlan, remainingTemps []multiEditTempFile, plan multiEditPlan, applyErr error) error {
	restoreErr := restoreMultiEditPlans(applied)
	cleanupTempFiles(remainingTemps)
	if restoreErr != nil {
		return errors.Join(fmt.Errorf("apply multi_edit to %s: %w", plan.path, applyErr), restoreErr)
	}
	return fmt.Errorf("apply multi_edit to %s: %w", plan.path, applyErr)
}

func buildRunVerificationTool(ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge) (einotool.BaseTool, error) {
	if service == nil {
		return nil, errors.New("artifact service is required")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required")
	}
	tool, err := inferProgressTool("run_verification", "Run a verification command and persist stdout/stderr as artifacts with normalized status.", func(ctx context.Context, input RunVerificationInput, emit tools.ToolProgressEmitter) (RunVerificationOutput, error) {
		return runVerification(ctx, ws, service, bridge, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build run_verification tool: %w", err)
	}
	return tool, nil
}

func runVerification(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, input RunVerificationInput, emit tools.ToolProgressEmitter) (RunVerificationOutput, error) {
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

func buildVerificationOutput(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, emit tools.ToolProgressEmitter, input RunVerificationInput, result verificationCommandResult, kind string, command []string, cwd string, paths []string, started time.Time) (RunVerificationOutput, error) {
	duration := time.Since(started)
	stdoutArtifact, err := writeWorkflowArtifact(ctx, service, bridge, store.ArtifactKindLog, fmt.Sprintf("%s verification stdout", kind), "text/plain", result.stdout)
	if err != nil {
		return RunVerificationOutput{}, err
	}
	stderrArtifact, err := writeWorkflowArtifact(ctx, service, bridge, store.ArtifactKindLog, fmt.Sprintf("%s verification stderr", kind), "text/plain", result.stderr)
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
	tool, err := inferProgressTool("git_summary", "Summarize workspace git status, diffstat, changed paths, and optionally persist a scoped diff artifact.", func(ctx context.Context, input GitSummaryInput, emit tools.ToolProgressEmitter) (GitSummaryOutput, error) {
		return runGitSummary(ctx, ws, service, bridge, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build git_summary tool: %w", err)
	}
	return tool, nil
}

func runGitSummary(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, input GitSummaryInput, emit tools.ToolProgressEmitter) (GitSummaryOutput, error) {
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

func applyGitSummaryDiff(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge domain.ToolCallContextBridge, emit tools.ToolProgressEmitter, input GitSummaryInput, output *GitSummaryOutput, scopedPath string) error {
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
	record, err := writeWorkflowArtifact(ctx, service, bridge, store.ArtifactKindDiff, "git summary diff", "text/x-diff", diff)
	if err != nil {
		return err
	}
	output.DiffArtifactID = record.ArtifactID
	diffSummary := artifactSummaryFromRecord(record)
	output.DiffArtifact = &diffSummary
	return emitToolProgress(ctx, emit, fmt.Sprintf("wrote diff artifact %s", record.ArtifactID))
}

type multiEditPlan struct {
	path     string
	resolved string
	before   []byte
	after    []byte
	mode     os.FileMode
	edits    []multiEditPreparedSpan
}

type multiEditPreparedSpan struct {
	path        string
	startLine   int
	endLine     int
	replacement string
}

type multiEditTempFile struct {
	target string
	path   string
	mode   os.FileMode
}

func prepareMultiEditPlans(ws WorkspaceView, spans []MultiEditSpan) ([]multiEditPlan, []MultiEditAppliedSpan, []string, error) {
	if len(spans) == 0 {
		return nil, nil, nil, errors.New("edits are required")
	}
	plansByPath, err := collectMultiEditPlans(ws, spans)
	if err != nil {
		return nil, nil, nil, err
	}
	paths := sortedMultiEditPaths(plansByPath)
	return finalizeMultiEditPlans(plansByPath, paths, spans)
}

func collectMultiEditPlans(ws WorkspaceView, spans []MultiEditSpan) (map[string]*multiEditPlan, error) {
	plansByPath := make(map[string]*multiEditPlan, len(spans))
	for index, span := range spans {
		plan, rel, err := resolveMultiEditSpanPlan(ws, plansByPath, span, index)
		if err != nil {
			return nil, err
		}
		if err := appendMultiEditSpan(plan, rel, span, index); err != nil {
			return nil, err
		}
		plansByPath[rel] = plan
	}
	return plansByPath, nil
}

func resolveMultiEditSpanPlan(ws WorkspaceView, plansByPath map[string]*multiEditPlan, span MultiEditSpan, index int) (*multiEditPlan, string, error) {
	if strings.TrimSpace(span.Path) == "" {
		return nil, "", fmt.Errorf("edits[%d].path is required", index)
	}
	if span.StartLine <= 0 || span.EndLine <= 0 {
		return nil, "", fmt.Errorf("edits[%d] start_line and end_line must be > 0", index)
	}
	resolved, err := ws.ResolveWritePath(span.Path)
	if err != nil {
		return nil, "", err
	}
	rel, err := ws.RelativePath(resolved)
	if err != nil {
		return nil, "", err
	}
	rel = filepath.ToSlash(rel)
	plan := plansByPath[rel]
	if plan == nil {
		var err error
		plan, err = loadMultiEditPlanBody(rel, resolved)
		if err != nil {
			return nil, "", err
		}
	}
	return plan, rel, nil
}

func loadMultiEditPlanBody(rel, resolved string) (*multiEditPlan, error) {
	body, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", resolved, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat file %s: %w", resolved, err)
	}
	return &multiEditPlan{
		path:     rel,
		resolved: resolved,
		before:   body,
		mode:     info.Mode().Perm(),
	}, nil
}

func appendMultiEditSpan(plan *multiEditPlan, rel string, span MultiEditSpan, index int) error {
	offsets := lineStartOffsets(plan.before)
	startLine, endLine, err := normalizeLineRange(len(offsets), span.StartLine, span.EndLine)
	if err != nil {
		return fmt.Errorf("edits[%d] %s: %w", index, rel, err)
	}
	plan.edits = append(plan.edits, multiEditPreparedSpan{
		path:        rel,
		startLine:   startLine,
		endLine:     endLine,
		replacement: span.Replacement,
	})
	return nil
}

func sortedMultiEditPaths(plansByPath map[string]*multiEditPlan) []string {
	paths := make([]string, 0, len(plansByPath))
	for path := range plansByPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func finalizeMultiEditPlans(plansByPath map[string]*multiEditPlan, paths []string, spans []MultiEditSpan) ([]multiEditPlan, []MultiEditAppliedSpan, []string, error) {
	plans := make([]multiEditPlan, 0, len(paths))
	applied := make([]MultiEditAppliedSpan, 0, len(spans))
	for _, path := range paths {
		plan := plansByPath[path]
		if err := sortMultiEditPlanEdits(path, plan); err != nil {
			return nil, nil, nil, err
		}
		plan.after = applyPreparedSpans(plan.before, plan.edits)
		for _, edit := range plan.edits {
			applied = append(applied, appliedSpanFromEdit(edit))
		}
		plans = append(plans, *plan)
	}
	return plans, applied, paths, nil
}

func sortMultiEditPlanEdits(path string, plan *multiEditPlan) error {
	slices.SortFunc(plan.edits, func(left, right multiEditPreparedSpan) int {
		if left.startLine != right.startLine {
			return left.startLine - right.startLine
		}
		return left.endLine - right.endLine
	})
	for i := 1; i < len(plan.edits); i++ {
		previous := plan.edits[i-1]
		current := plan.edits[i]
		if current.startLine <= previous.endLine {
			return fmt.Errorf("multi_edit spans overlap in %s: %d-%d and %d-%d", path, previous.startLine, previous.endLine, current.startLine, current.endLine)
		}
	}
	return nil
}

func appliedSpanFromEdit(edit multiEditPreparedSpan) MultiEditAppliedSpan {
	return MultiEditAppliedSpan{
		Path:             edit.path,
		StartLine:        edit.startLine,
		EndLine:          edit.endLine,
		ReplacementBytes: len(edit.replacement),
	}
}

func applyPreparedSpans(body []byte, edits []multiEditPreparedSpan) []byte {
	result := append([]byte(nil), body...)
	offsets := lineStartOffsets(body)
	totalLines := len(offsets)
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		startByte := offsets[edit.startLine-1]
		endByte := len(body)
		if edit.endLine < totalLines {
			endByte = offsets[edit.endLine]
		}
		replaced := append([]byte(nil), result[:startByte]...)
		replaced = append(replaced, []byte(edit.replacement)...)
		replaced = append(replaced, result[endByte:]...)
		result = replaced
	}
	return result
}

func cleanupTempFiles(items []multiEditTempFile) {
	for _, item := range items {
		_ = os.Remove(item.path)
	}
}

func restoreMultiEditPlans(plans []multiEditPlan) error {
	var joined error
	for _, plan := range plans {
		if err := os.WriteFile(plan.resolved, plan.before, plan.mode); err != nil {
			joined = errors.Join(joined, fmt.Errorf("restore %s after failed multi_edit: %w", plan.path, err))
		}
	}
	return joined
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

func executeVerificationCommand(ctx context.Context, ws WorkspaceView, command []string, cwd string, timeoutSeconds int, emit tools.ToolProgressEmitter) (verificationCommandResult, error) {
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

func buildVerificationCommand(ws WorkspaceView, command []string, cwd string, ctx context.Context, emit tools.ToolProgressEmitter) (*exec.Cmd, *runCommandProgressBuffer, *runCommandProgressBuffer) {
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
