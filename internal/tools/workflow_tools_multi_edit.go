package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

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
