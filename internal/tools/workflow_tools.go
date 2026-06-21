package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/workspace"
)

const (
	verificationStatusPassed   = "passed"
	verificationStatusFailed   = "failed"
	verificationStatusTimedOut = "timed_out"
)

func buildMultiEditTool(ws WorkspaceView) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("multi_edit", "Atomically replace multiple explicit line ranges across workspace files with one mutation checkpoint.", func(ctx context.Context, input MultiEditInput, emit tooling.ToolProgressEmitter) (MultiEditOutput, error) {
		return runMultiEdit(ctx, ws, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build multi_edit tool: %w", err)
	}
	return tool, nil
}

func runMultiEdit(ctx context.Context, ws WorkspaceView, input MultiEditInput, emit tooling.ToolProgressEmitter) (MultiEditOutput, error) {
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

func buildRunVerificationTool(ws WorkspaceView, service ArtifactService, bridge ArtifactContext) (einotool.BaseTool, error) {
	if service == nil {
		return nil, errors.New("artifact service is required")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required")
	}
	tool, err := inferProgressTool("run_verification", "Run a verification command and persist stdout/stderr as artifacts with normalized status.", func(ctx context.Context, input RunVerificationInput, emit tooling.ToolProgressEmitter) (RunVerificationOutput, error) {
		return runVerification(ctx, ws, service, bridge, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build run_verification tool: %w", err)
	}
	return tool, nil
}

func runVerification(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge ArtifactContext, input RunVerificationInput, emit tooling.ToolProgressEmitter) (RunVerificationOutput, error) {
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

func buildVerificationOutput(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge ArtifactContext, emit tooling.ToolProgressEmitter, input RunVerificationInput, result verificationCommandResult, kind string, command []string, cwd string, paths []string, started time.Time) (RunVerificationOutput, error) {
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

func buildGitSummaryTool(ws WorkspaceView, service ArtifactService, bridge ArtifactContext) (einotool.BaseTool, error) {
	tool, err := inferProgressTool("git_summary", "Summarize workspace git status, diffstat, changed paths, and optionally persist a scoped diff artifact.", func(ctx context.Context, input GitSummaryInput, emit tooling.ToolProgressEmitter) (GitSummaryOutput, error) {
		return runGitSummary(ctx, ws, service, bridge, input, emit)
	})
	if err != nil {
		return nil, fmt.Errorf("build git_summary tool: %w", err)
	}
	return tool, nil
}

func runGitSummary(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge ArtifactContext, input GitSummaryInput, emit tooling.ToolProgressEmitter) (GitSummaryOutput, error) {
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

func applyGitSummaryDiff(ctx context.Context, ws WorkspaceView, service ArtifactService, bridge ArtifactContext, emit tooling.ToolProgressEmitter, input GitSummaryInput, output *GitSummaryOutput, scopedPath string) error {
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
