package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/crystallization"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestrationmode"
	"github.com/ycvk/acorn/internal/runtimehistory"
)

var (
	ErrRunNotInterrupted = errors.New("run not interrupted")
	ErrExecutionNotReady = errors.New("execution not ready")
)

type StreamSink func(item StreamItem) error

type Result struct {
	RunID        string           `json:"run_id"`
	Status       events.RunStatus `json:"status"`
	Output       string           `json:"output,omitempty"`
	Error        string           `json:"error,omitempty"`
	Interrupted  map[string]any   `json:"interrupted,omitempty"`
	TraceSummary *TraceSummary    `json:"trace_summary,omitempty"`
}

type ExecuteRequest struct {
	RunID             string
	SessionID         string
	TurnIndex         int
	Input             string
	SkillID           string
	AllowedToolNames  []string
	Messages          []adk.Message
	OrchestrationMode orchestrationmode.Mode
	ParentRunID       string
	Depth             int
}

type Executor struct {
	store             executorStore
	runnerFactory     *RunnerFactory
	controller        *RunController
	newChatModel      func(ctx context.Context) (einomodel.BaseChatModel, error)
	archiveRunFunc    func(ctx context.Context, runID string, runStatus events.RunStatus) error
	sessionSummarySvc *runtimehistory.SessionSummaryService
	crystallizer      crystallization.Service
}

func NewExecutorWithRunnerFactoryAndController(cfg *config.Config, store executorStore, runnerFactory *RunnerFactory, controller *RunController) (*Executor, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if store == nil {
		return nil, errors.New("store is required")
	}
	if runnerFactory == nil {
		return nil, errors.New("runner factory is required")
	}
	if controller == nil {
		controller = NewRunController()
	}
	if err := cfg.ValidateExecutionReady(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExecutionNotReady, err)
	}
	exec := &Executor{
		store:             store,
		runnerFactory:     runnerFactory,
		controller:        controller,
		sessionSummarySvc: runnerFactory.sessionSummarySvc,
		newChatModel:      runnerFactory.newChatModel,
	}
	exec.crystallizer = runnerFactory.Crystallizer()
	exec.archiveRunFunc = exec.archiveRun
	return exec, nil
}

func (e *Executor) SetCrystallizer(svc crystallization.Service) {
	if e != nil {
		e.crystallizer = svc
	}
}

func archiveSignalsFromEvents(records []events.EventRecord) ([]string, []string) {
	pathSet := make(map[string]struct{})
	toolSet := make(map[string]struct{})
	for _, record := range records {
		payload, ok := record.Payload.(map[string]any)
		if !ok {
			continue
		}
		toolName := strings.TrimSpace(extractString(payload["tool_name"]))
		if toolName == "" && strings.HasPrefix(record.Kind, "tool.call") {
			toolName = strings.TrimSpace(extractString(payload["name"]))
		}
		if toolName != "" {
			toolSet[toolName] = struct{}{}
		}
		if arguments := strings.TrimSpace(extractString(payload["arguments_json"])); arguments != "" {
			for _, path := range extractTouchedPaths(arguments) {
				pathSet[path] = struct{}{}
			}
		}
	}
	return sortedKeys(pathSet), sortedKeys(toolSet)
}

func extractTouchedPaths(argumentsJSON string) []string {
	if strings.TrimSpace(argumentsJSON) == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return nil
	}
	paths := make([]string, 0, 2)
	for _, key := range []string{"path", "file_path", "target", "root_dir", "work_dir"} {
		value := strings.TrimSpace(extractString(payload[key]))
		if value != "" {
			paths = append(paths, value)
		}
	}
	return paths
}

func sortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactArchiveText(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 280 {
		return trimmed
	}
	return trimmed[:280] + "..."
}

func (e *Executor) traceSummary(ctx context.Context, runID string) (*TraceSummary, error) {
	if e == nil || e.store == nil {
		return nil, errors.New("executor store is nil")
	}
	items, err := e.store.LoadEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("load trace summary events: %w", err)
	}
	return BuildTraceSummary(items), nil
}

func (e *Executor) failRunSetup(ctx context.Context, runID string, setupErr error, sink StreamSink) error {
	if e == nil || e.store == nil || strings.TrimSpace(runID) == "" || setupErr == nil {
		return setupErr
	}
	durableCtx := durableContext(ctx)
	if err := e.emitRunFailed(durableCtx, runID, sink, setupErr.Error()); err != nil {
		return err
	}
	return e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, "", setupErr.Error())
}

func (e *Executor) recordFinalizationFailure(ctx context.Context, runID, output string, finalizationErr error, sink StreamSink) error {
	if e == nil || e.store == nil {
		return finalizationErr
	}
	durableCtx := durableContext(ctx)
	message := fmt.Sprintf("run finalization failed: %v", finalizationErr)
	var errs []error
	if err := e.store.FinishRunContext(durableCtx, runID, events.RunStatusFailed, output, message); err != nil {
		errs = append(errs, fmt.Errorf("mark run failed after finalization failure: %w", err))
	}
	if err := e.emitRunFailed(durableCtx, runID, sink, message); err != nil {
		errs = append(errs, fmt.Errorf("append finalization failure event: %w", err))
	}
	if _, err := e.traceSummary(durableCtx, runID); err != nil {
		errs = append(errs, err)
	}
	errs = append([]error{finalizationErr}, errs...)
	return errors.Join(errs...)
}

func (e *Executor) verifyAndRecordSkill(ctx context.Context, runID string, selected *SelectedSkill, status events.RunStatus, output string, sink StreamSink) error {
	_ = ctx
	if e == nil || e.store == nil || selected == nil || strings.TrimSpace(runID) == "" || status != events.RunStatusFailed {
		return nil
	}
	_, err := appendStreamItem(ctx, e.store, sink, StreamItem{
		RunID: runID,
		Kind:  StreamKindSkillFailed,
		Payload: &SkillFailedPayload{Skill: &StreamSkill{
			SelectedID:    selected.Skill.ID,
			Name:          selected.Skill.Name,
			Source:        selected.Skill.Source,
			Path:          selected.Skill.Path,
			Summary:       selected.Skill.Summary,
			Requirements:  streamSkillRequirementsFromDomain(selected.Skill.Requires),
			FailureReason: failureReasonForStatus(status, output),
		}},
	})
	if err != nil {
		return err
	}
	return nil
}

func failureReasonForStatus(status events.RunStatus, output string) string {
	if status != events.RunStatusFailed {
		return ""
	}
	if strings.TrimSpace(output) == "" {
		return "run_failed"
	}
	return "run_failed:with_output"
}
