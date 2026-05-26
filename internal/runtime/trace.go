package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/workspace"
)

// --- Trace types ---

type Trace struct {
	Run     *events.RunRecord   `json:"run,omitempty"`
	Summary *TraceSummary       `json:"summary,omitempty"`
	Items   []stream.StreamItem `json:"items,omitempty"`
}

type TraceSummary struct {
	ItemCount                  int                   `json:"item_count"`
	LastKind                   stream.StreamItemKind `json:"last_kind,omitempty"`
	AssistantMessageCount      int                   `json:"assistant_message_count,omitempty"`
	AssistantDeltaCount        int                   `json:"assistant_delta_count,omitempty"`
	AssistantDeltaMessageCount int                   `json:"assistant_delta_message_count,omitempty"`
	AssistantDeltaCharCount    int                   `json:"assistant_delta_char_count,omitempty"`
	ToolCallCount              int                   `json:"tool_call_count,omitempty"`
	DecisionEventCount         int                   `json:"decision_event_count,omitempty"`
	SkillEventCount            int                   `json:"skill_event_count,omitempty"`
	PlanEventCount             int                   `json:"plan_event_count,omitempty"`
	DecisionSelected           bool                  `json:"decision_selected,omitempty"`
	DecisionBlocked            bool                  `json:"decision_blocked,omitempty"`
	SkillSelected              bool                  `json:"skill_selected,omitempty"`
	Interrupted                bool                  `json:"interrupted,omitempty"`
	Failed                     bool                  `json:"failed,omitempty"`
	Completed                  bool                  `json:"completed,omitempty"`
}

func BuildTrace(run *events.RunRecord, raw []events.EventRecord) *Trace {
	items := make([]stream.StreamItem, 0, len(raw))
	for _, event := range raw {
		items = append(items, projectEventToStreamItem(event))
	}
	return &Trace{Run: run, Summary: summarizeStreamItems(items), Items: items}
}

func BuildTraceSummary(raw []events.EventRecord) *TraceSummary {
	items := make([]stream.StreamItem, 0, len(raw))
	for _, event := range raw {
		items = append(items, projectEventToStreamItem(event))
	}
	return summarizeStreamItems(items)
}

func LatestRootInterruptContexts(raw []events.EventRecord) ([]stream.StreamInterruptContext, error) {
	for i := len(raw) - 1; i >= 0; i-- {
		item := projectEventToStreamItem(raw[i])
		if item.Kind != stream.StreamKindRunInterrupted {
			continue
		}
		interrupt := item.GetInterrupt()
		if interrupt == nil {
			return nil, errors.New("run.interrupted payload missing interrupt")
		}
		contexts := make([]stream.StreamInterruptContext, 0, len(interrupt.Contexts))
		for _, ctx := range interrupt.Contexts {
			if !ctx.IsRootCause {
				continue
			}
			id := strings.TrimSpace(ctx.ID)
			if id == "" {
				return nil, errors.New("interrupt context id is empty")
			}
			contexts = append(contexts, ctx)
		}
		if len(contexts) == 0 {
			return nil, errors.New("run.interrupted has no root interrupt contexts")
		}
		return contexts, nil
	}
	return nil, errors.New("run has no interrupt event to resume")
}

func LatestRootInterruptIDs(raw []events.EventRecord) ([]string, error) {
	contexts, err := LatestRootInterruptContexts(raw)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(contexts))
	for _, ctx := range contexts {
		ids = append(ids, strings.TrimSpace(ctx.ID))
	}
	return ids, nil
}

func projectEventToStreamItem(event events.EventRecord) stream.StreamItem {
	item := stream.StreamItem{RunID: event.RunID, Sequence: event.Sequence, CreatedAt: event.CreatedAt}

	kind := eventKindToStreamKind(event.Kind)
	item.Kind = kind

	data, err := json.Marshal(event.Payload)
	if err != nil {
		return item
	}

	payload, err := stream.UnmarshalPayload(kind, data)
	if err != nil {
		return item
	}
	item.Payload = payload

	switch p := payload.(type) {
	case *stream.ToolCallStartedPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	case *stream.ToolCallProgressPayload:
		p.ToolCall = extractToolCallProgressFromMergedPayload(event.Payload)
	case *stream.ToolCallSucceededPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	case *stream.ToolCallFailedPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	case *stream.ToolCallInterruptedPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	}

	return item
}

func eventKindToStreamKind(eventKind string) stream.StreamItemKind {
	switch eventKind {
	case "run.started":
		return stream.StreamKindRunStarted
	case "run.completed":
		return stream.StreamKindRunCompleted
	case "run.failed":
		return stream.StreamKindRunFailed
	case "run.interrupted":
		return stream.StreamKindRunInterrupted
	case "run.resume_requested":
		return stream.StreamKindRunResumeRequested
	case "decision_selected":
		return stream.StreamKindDecisionSelected
	case "decision_blocked":
		return stream.StreamKindDecisionBlocked
	case "skill.discovered":
		return stream.StreamKindSkillDiscovered
	case "skill.selected":
		return stream.StreamKindSkillSelected
	case "skill.loaded":
		return stream.StreamKindSkillLoaded
	case "skill.failed":
		return stream.StreamKindSkillFailed
	case "skill.lifecycle":
		return stream.StreamKindSkillLifecycle
	case "memory.prepared":
		return stream.StreamKindMemoryPrepared
	case "context.pressure":
		return stream.StreamKindContextPressure
	case "context.compressed":
		return stream.StreamKindContextCompressed
	case "assistant.delta":
		return stream.StreamKindAssistantDelta
	case "stream.heartbeat":
		return stream.StreamKindHeartbeat
	case "agent.message":
		return stream.StreamKindAssistantMessage
	case "tool.call.started":
		return stream.StreamKindToolCallStarted
	case "tool.call.progress":
		return stream.StreamKindToolCallProgress
	case "tool.call.succeeded":
		return stream.StreamKindToolCallSucceeded
	case "tool.call.failed":
		return stream.StreamKindToolCallFailed
	case "tool.call.interrupted":
		return stream.StreamKindToolCallInterrupted
	case "subagent.started":
		return stream.StreamKindSubagentStarted
	case "subagent.completed":
		return stream.StreamKindSubagentCompleted
	case "subagent.failed":
		return stream.StreamKindSubagentFailed
	case "tool.parallel_batch.started":
		return stream.StreamKindToolParallelBatchStarted
	case "tool.parallel_batch.completed":
		return stream.StreamKindToolParallelBatchCompleted
	case "run.archived":
		return stream.StreamKindRunArchived
	case "plan.created":
		return stream.StreamKindPlanCreated
	case "plan.updated":
		return stream.StreamKindPlanUpdated
	case "plan.cleared":
		return stream.StreamKindPlanCleared
	case "step.started":
		return stream.StreamKindStepStarted
	case "step.completed":
		return stream.StreamKindStepCompleted
	case "step.failed":
		return stream.StreamKindStepFailed
	default:
		return stream.StreamItemKind(eventKind)
	}
}

func extractToolCallFromMergedPayload(payload any) *stream.StreamToolCall {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var tool stream.StreamToolCall
	if err := json.Unmarshal(data, &tool); err != nil {
		return nil
	}
	if tool.Name == "" {
		if m, ok := payload.(map[string]any); ok {
			if v, ok := m["tool_name"].(string); ok {
				tool.Name = v
			}
		}
	}
	if tool.Name == "" && tool.Provider == "" && tool.Output == "" && tool.Error == "" {
		return nil
	}
	return &tool
}

func extractToolCallProgressFromMergedPayload(payload any) *stream.StreamToolCallProgress {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var tool stream.StreamToolCallProgress
	if err := json.Unmarshal(data, &tool); err != nil {
		return nil
	}
	if tool.Name == "" {
		if m, ok := payload.(map[string]any); ok {
			if v, ok := m["tool_name"].(string); ok {
				tool.Name = v
			}
		}
	}
	if tool.Name == "" && tool.Provider == "" && tool.Delta == "" {
		return nil
	}
	return &tool
}

func summarizeStreamItems(items []stream.StreamItem) *TraceSummary {
	summary := &TraceSummary{ItemCount: len(items)}
	assistantDeltaMessageIDs := make(map[string]struct{})
	for _, item := range items {
		summary.LastKind = item.Kind
		switch item.Kind {
		case stream.StreamKindAssistantDelta:
			summary.AssistantDeltaCount++
			if delta := item.GetAssistantDelta(); delta != nil {
				summary.AssistantDeltaCharCount += len([]rune(delta.Delta))
				messageID := strings.TrimSpace(delta.MessageID)
				if messageID != "" {
					assistantDeltaMessageIDs[messageID] = struct{}{}
				}
			}
		case stream.StreamKindAssistantMessage:
			summary.AssistantMessageCount++
		case stream.StreamKindToolCallStarted, stream.StreamKindToolCallSucceeded, stream.StreamKindToolCallFailed, stream.StreamKindToolCallInterrupted:
			summary.ToolCallCount++
		case stream.StreamKindDecisionSelected, stream.StreamKindDecisionBlocked:
			summary.DecisionEventCount++
			if item.Kind == stream.StreamKindDecisionSelected {
				summary.DecisionSelected = true
			}
			if item.Kind == stream.StreamKindDecisionBlocked {
				summary.DecisionBlocked = true
			}
		case stream.StreamKindSkillDiscovered, stream.StreamKindSkillSelected, stream.StreamKindSkillLoaded, stream.StreamKindSkillFailed, stream.StreamKindSkillLifecycle:
			summary.SkillEventCount++
			if item.Kind == stream.StreamKindSkillSelected {
				summary.SkillSelected = true
			}
		case stream.StreamKindRunInterrupted:
			summary.Interrupted = true
		case stream.StreamKindRunFailed:
			summary.Failed = true
		case stream.StreamKindRunCompleted:
			summary.Completed = true
		case stream.StreamKindPlanCreated, stream.StreamKindPlanUpdated, stream.StreamKindPlanCleared, stream.StreamKindStepStarted, stream.StreamKindStepCompleted, stream.StreamKindStepFailed:
			summary.PlanEventCount++
		}
	}
	summary.AssistantDeltaMessageCount = len(assistantDeltaMessageIDs)
	return summary
}

func SelectedSkillFromEvents(raw []events.EventRecord) *SelectedSkill {
	for i := len(raw) - 1; i >= 0; i-- {
		item := projectEventToStreamItem(raw[i])
		if (item.Kind != stream.StreamKindSkillLoaded && item.Kind != stream.StreamKindSkillSelected) || item.GetSkill() == nil {
			continue
		}
		skill := item.GetSkill()
		selectedID := strings.TrimSpace(skill.SelectedID)
		if selectedID == "" {
			continue
		}
		return &SelectedSkill{
			Skill: skills.Spec{
				ID:          selectedID,
				Name:        firstNonEmpty(skill.Name, selectedID),
				Summary:     skill.Summary,
				Instruction: skill.Instruction,
				Source:      skill.Source,
				Path:        skill.Path,
				Scripts:     append([]string(nil), skill.Scripts...),
				Requires: skills.Requirements{
					Tools:    append([]string(nil), skill.Requirements.Tools...),
					Toolsets: append([]string(nil), skill.Requirements.Toolsets...),
					Bins:     append([]string(nil), skill.Requirements.Bins...),
					Env:      append([]string(nil), skill.Requirements.Env...),
				},
			},
			Score:        skill.Score,
			MatchedTerms: append([]string(nil), skill.MatchedTerms...),
		}
	}
	return nil
}

type SubagentExecutor struct {
	cfg   *config.Config
	store ExecutorStore
	rf    *RunnerFactory
	ctrl  *RunController

	depthMu sync.Mutex
	depths  map[string]int
}

func NewSubagentExecutor(cfg *config.Config, store ExecutorStore, rf *RunnerFactory, ctrl *RunController) *SubagentExecutor {
	if ctrl == nil {
		ctrl = NewRunController()
	}
	return &SubagentExecutor{
		cfg:    cfg,
		store:  store,
		rf:     rf,
		ctrl:   ctrl,
		depths: make(map[string]int),
	}
}

func (se *SubagentExecutor) currentDepth(parentRunID string) int {
	if se != nil && se.rf != nil && se.rf.registry != nil {
		if rc, ok := se.rf.registry.Get(strings.TrimSpace(parentRunID)); ok {
			return rc.Depth
		}
	}
	se.depthMu.Lock()
	defer se.depthMu.Unlock()
	return se.depths[parentRunID]
}

func (se *SubagentExecutor) incrementDepth(parentRunID string) int {
	se.depthMu.Lock()
	defer se.depthMu.Unlock()
	se.depths[parentRunID]++
	return se.depths[parentRunID]
}

func (se *SubagentExecutor) decrementDepth(parentRunID string) {
	se.depthMu.Lock()
	defer se.depthMu.Unlock()
	if se.depths[parentRunID] > 0 {
		se.depths[parentRunID]--
	}
}

func (se *SubagentExecutor) Execute(ctx context.Context, req orchestration.ChildAgentRequest) (*orchestration.ChildAgentResult, error) {
	req = normalizeChildAgentRequest(req)
	task := strings.TrimSpace(req.Task)
	parentRunID := strings.TrimSpace(req.ParentRunID)
	if task == "" {
		return nil, errors.New("task is required for subagent execution")
	}
	if parentRunID == "" {
		return nil, errors.New("parent run ID is required for subagent execution")
	}

	newDepth := se.incrementDepth(parentRunID)
	defer se.decrementDepth(parentRunID)

	if se.store == nil {
		return nil, errors.New("subagent executor store is not initialized")
	}

	sink := stream.StreamSinkFromContext(ctx)

	subRunID := newRunID()
	childSessionID := "delegate_" + subRunID
	childRunMode := orchestration.NormalizeChildRunMode(req.ChildRunMode)
	requestedMode := events.OrchestrationMode(req.RequestedMode).Normalize()
	if requestedMode == "" {
		requestedMode = events.ModeSingleAgent
	}
	workspaceMode := orchestration.NormalizeChildWorkspaceMode(req.WorkspaceMode)
	childRunnerFactory := se.rf
	worktreePath := ""
	if workspaceMode == orchestration.ChildWorkspaceModeWorktree {
		childWorkspace, err := se.createChildWorktreeWorkspace(ctx, subRunID)
		if err != nil {
			if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
				return nil, errors.Join(err, emitErr)
			}
			return nil, err
		}
		worktreePath = childWorkspace.Root()
		childRunnerFactory = se.rf.cloneForWorkspace(childWorkspace)
	}
	// Delegated child sessions always start fresh, so bootstrap the session and
	// first user turn in one tight path rather than walking the general chat
	// preparation flow.
	turnIndex, err := se.store.CreateFreshSessionTurn(childCtxOrBackground(ctx), childSessionID, truncateTaskTitle(task), task)
	if err != nil {
		return nil, fmt.Errorf("prepare child session turn: %w", err)
	}

	if _, err := stream.AppendStreamItem(ctx, se.store, sink, stream.StreamItem{
		RunID:     parentRunID,
		Kind:      stream.StreamKindSubagentStarted,
		CreatedAt: time.Now().UTC(),
		Payload: &stream.SubagentStartedPayload{
			SubRunID:          subRunID,
			ParentID:          parentRunID,
			SessionID:         childSessionID,
			Depth:             newDepth,
			Task:              task,
			ChildRunMode:      string(childRunMode),
			WorkspaceMode:     string(workspaceMode),
			WorktreePath:      worktreePath,
			ContextMessages:   len(req.ContextMessages),
			OrchestrationMode: string(requestedMode),
			ParentStepID:      req.ParentStepID,
		},
	}); err != nil {
		return nil, fmt.Errorf("emit subagent.started: %w", err)
	}

	childCtx := childCtxOrBackground(ctx)

	exec, err := NewExecutorWithRunnerFactoryAndController(se.cfg, se.store, childRunnerFactory, se.ctrl)
	if err != nil {
		if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
			return nil, errors.Join(fmt.Errorf("create subagent executor: %w", err), emitErr)
		}
		return nil, fmt.Errorf("create subagent executor: %w", err)
	}

	childMessages := append([]*schema.Message(nil), req.ContextMessages...)
	childMessages = append(childMessages, schema.UserMessage(task))
	result, err := exec.ExecuteMessages(childCtx, ExecuteRequest{
		RunID:             subRunID,
		SessionID:         childSessionID,
		TurnIndex:         turnIndex,
		Input:             task,
		Messages:          childMessages,
		AllowedToolNames:  req.AllowedToolNames,
		OrchestrationMode: requestedMode,
		ParentRunID:       parentRunID,
		Depth:             se.currentDepth(parentRunID) + 1,
	}, nil)
	if err != nil {
		if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
			return nil, errors.Join(err, emitErr)
		}
		return nil, err
	}

	planRecord, err := se.store.LoadPlanBySession(childCtxOrBackground(ctx), childSessionID)
	if err != nil && !errors.Is(err, store.ErrPlanNotFound) {
		if emitErr := se.emitFailed(ctx, parentRunID, subRunID, childSessionID, req.ParentStepID, childRunMode, workspaceMode, worktreePath, requestedMode, err.Error(), sink); emitErr != nil {
			return nil, errors.Join(fmt.Errorf("load child plan: %w", err), emitErr)
		}
		return nil, fmt.Errorf("load child plan: %w", err)
	}
	if errors.Is(err, store.ErrPlanNotFound) {
		planRecord = nil
	}

	planFailureReasons := delegationPlanFailureReasons(planRecord)
	finalStatus := string(result.Status)
	if len(planFailureReasons) > 0 {
		finalStatus = string(events.RunStatusFailed)
	}
	delegated := &orchestration.ChildAgentResult{
		ChildRunID:         result.RunID,
		ChildSessionID:     childSessionID,
		ChildRunMode:       childRunMode,
		WorkspaceMode:      workspaceMode,
		WorktreePath:       worktreePath,
		FinalStatus:        finalStatus,
		OutputSummary:      strings.TrimSpace(result.Output),
		EvidenceSummaries:  delegationEvidenceSummaries(planRecord),
		EvidenceRefs:       delegationEvidenceRefs(planRecord),
		EffectiveToolNames: append([]string(nil), req.AllowedToolNames...),
	}
	delegated.Acceptance = evaluateDelegationAcceptance(req, events.RunStatus(finalStatus), delegated.OutputSummary, delegated.EvidenceSummaries, planFailureReasons)

	if _, err := stream.AppendStreamItem(ctx, se.store, sink, stream.StreamItem{
		RunID:     parentRunID,
		Kind:      stream.StreamKindSubagentCompleted,
		CreatedAt: time.Now().UTC(),
		Payload: &stream.SubagentCompletedPayload{
			SubRunID:          subRunID,
			ParentID:          parentRunID,
			SessionID:         childSessionID,
			Summary:           delegated.OutputSummary,
			FinalStatus:       delegated.FinalStatus,
			AcceptanceStatus:  delegated.Acceptance.Status,
			AcceptanceReasons: append([]string(nil), delegated.Acceptance.Reasons...),
			ChildRunMode:      string(childRunMode),
			WorkspaceMode:     string(workspaceMode),
			WorktreePath:      worktreePath,
			EvidenceRefs:      append([]string(nil), delegated.EvidenceRefs...),
			OrchestrationMode: string(requestedMode),
			ParentStepID:      req.ParentStepID,
		},
	}); err != nil {
		return delegated, fmt.Errorf("emit subagent.completed: %w", err)
	}
	return delegated, nil
}

func (se *SubagentExecutor) emitFailed(ctx context.Context, parentRunID, subRunID, childSessionID, parentStepID string, childRunMode orchestration.ChildRunMode, workspaceMode orchestration.ChildWorkspaceMode, worktreePath string, mode events.OrchestrationMode, errMsg string, sink stream.StreamSink) error {
	if se == nil || se.store == nil {
		return errors.New("emit subagent.failed: store is not initialized")
	}
	if _, err := stream.AppendStreamItem(ctx, se.store, sink, stream.StreamItem{
		RunID:     parentRunID,
		Kind:      stream.StreamKindSubagentFailed,
		CreatedAt: time.Now().UTC(),
		Payload: &stream.SubagentFailedPayload{
			SubRunID:          subRunID,
			ParentID:          parentRunID,
			SessionID:         childSessionID,
			Error:             errMsg,
			AcceptanceStatus:  "failed",
			AcceptanceReasons: []string{errMsg},
			ChildRunMode:      string(childRunMode),
			WorkspaceMode:     string(workspaceMode),
			WorktreePath:      worktreePath,
			OrchestrationMode: string(mode),
			ParentStepID:      parentStepID,
		},
	}); err != nil {
		return fmt.Errorf("emit subagent.failed: %w", err)
	}
	return nil
}

func (se *SubagentExecutor) createChildWorktreeWorkspace(ctx context.Context, subRunID string) (*workspace.Workspace, error) {
	if se == nil || se.rf == nil || se.rf.deps.Workspace == nil {
		return nil, errors.New("child worktree requires an initialized workspace")
	}
	worktree, err := se.rf.deps.Workspace.CreateChildWorktree(childCtxOrBackground(ctx), subRunID)
	if err != nil {
		return nil, fmt.Errorf("create child worktree: %w", err)
	}
	childWorkspace, err := se.rf.deps.Workspace.OpenWorktree(worktree)
	if err != nil {
		return nil, fmt.Errorf("open child worktree: %w", err)
	}
	return childWorkspace, nil
}

type subagentExecutorAdapter struct {
	exec orchestration.ChildAgentExecutor
}

func (a subagentExecutorAdapter) ExecuteMessages(ctx context.Context, messages []*schema.Message) (string, error) {
	parentRunID := getRunID(ctx)
	if strings.TrimSpace(parentRunID) == "" {
		parentRunID = "sampling_parent"
	}

	task := "MCP sampling request"
	if len(messages) > 0 {
		task = messages[len(messages)-1].Content
	}

	result, err := a.exec.Execute(ctx, orchestration.ChildAgentRequest{
		ParentRunID:        parentRunID,
		Task:               task,
		ContextMessages:    append([]*schema.Message(nil), messages...),
		AcceptanceCriteria: []string{"sampling request completed"},
		Origin:             orchestration.ChildAgentOriginMCPSampling,
	})
	if err != nil {
		return "", err
	}
	return result.OutputSummary, nil
}

func normalizeChildAgentRequest(req orchestration.ChildAgentRequest) orchestration.ChildAgentRequest {
	req.ParentRunID = strings.TrimSpace(req.ParentRunID)
	req.ParentSessionID = strings.TrimSpace(req.ParentSessionID)
	req.ParentStepID = strings.TrimSpace(req.ParentStepID)
	req.Task = strings.TrimSpace(req.Task)
	req.AllowedToolNames = normalizeToolNames(req.AllowedToolNames)
	req.AcceptanceCriteria = normalizeToolNames(req.AcceptanceCriteria)
	req.ExpectedEvidence = normalizeToolNames(req.ExpectedEvidence)
	req.ChildRunMode = orchestration.NormalizeChildRunMode(req.ChildRunMode)
	req.WorkspaceMode = orchestration.NormalizeChildWorkspaceMode(req.WorkspaceMode)
	req.RequestedMode = events.OrchestrationMode(req.RequestedMode).Normalize()
	return req
}

func evaluateDelegationAcceptance(
	req orchestration.ChildAgentRequest,
	runStatus events.RunStatus,
	outputSummary string,
	evidenceSummaries []string,
	planFailureReasons []string,
) orchestration.ChildAgentAcceptance {
	reasons := make([]string, 0, len(req.AcceptanceCriteria)+len(req.ExpectedEvidence)+len(planFailureReasons)+1)
	switch runStatus {
	case events.RunStatusSucceeded:
	default:
		reasons = append(reasons, fmt.Sprintf("child run finished with status %s", runStatus))
	}
	reasons = append(reasons, planFailureReasons...)

	for _, expected := range req.ExpectedEvidence {
		if !summaryContains(evidenceSummaries, expected) {
			reasons = append(reasons, fmt.Sprintf("missing expected evidence: %s", expected))
		}
	}
	for _, criterion := range req.AcceptanceCriteria {
		if acceptanceCriterionSatisfied(criterion, outputSummary, evidenceSummaries) {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("acceptance criterion not satisfied: %s", criterion))
	}
	if len(reasons) == 0 {
		return orchestration.ChildAgentAcceptance{Status: "passed"}
	}
	return orchestration.ChildAgentAcceptance{Status: "failed", Reasons: reasons}
}

func delegationPlanFailureReasons(planRecord *store.PlanRecord) []string {
	if planRecord == nil {
		return nil
	}
	reasons := make([]string, 0)
	for _, step := range planRecord.Steps {
		if strings.TrimSpace(string(step.Status)) != string(PlanStepFailed) {
			continue
		}
		reason := strings.TrimSpace(latestStoreEvidenceError(step.Evidence))
		if reason == "" {
			action := strings.TrimSpace(step.Action)
			if action == "" {
				action = strings.TrimSpace(step.ID)
			}
			reason = fmt.Sprintf("child plan step %s failed", action)
		}
		reasons = append(reasons, reason)
	}
	return reasons
}

func latestStoreEvidenceError(items []store.PlanEvidence) string {
	for i := len(items) - 1; i >= 0; i-- {
		if strings.TrimSpace(items[i].Status) != string(EvidenceStatusFailed) {
			continue
		}
		if errText := strings.TrimSpace(items[i].Error); errText != "" {
			return errText
		}
		if summary := strings.TrimSpace(items[i].Summary); summary != "" {
			return summary
		}
	}
	return ""
}

func delegationEvidenceSummaries(planRecord *store.PlanRecord) []string {
	if planRecord == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(planRecord.Steps))
	for _, step := range planRecord.Steps {
		for _, evidence := range step.Evidence {
			summary := strings.TrimSpace(evidence.Summary)
			if summary == "" {
				continue
			}
			if _, ok := seen[summary]; ok {
				continue
			}
			seen[summary] = struct{}{}
			result = append(result, summary)
		}
	}
	return result
}

func delegationEvidenceRefs(planRecord *store.PlanRecord) []string {
	if planRecord == nil {
		return nil
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, step := range planRecord.Steps {
		for _, evidence := range step.Evidence {
			for _, ref := range childEvidenceRefs(evidence) {
				if _, ok := seen[ref]; ok {
					continue
				}
				seen[ref] = struct{}{}
				result = append(result, ref)
			}
		}
	}
	return result
}

func childEvidenceRefs(evidence store.PlanEvidence) []string {
	refs := make([]string, 0, 3)
	if ref := strings.TrimSpace(evidence.ToolResultRef); ref != "" {
		refs = append(refs, ref)
	}
	if ref := strings.TrimSpace(evidence.ChildRunID); ref != "" {
		refs = append(refs, "run:"+ref)
	}
	if ref := strings.TrimSpace(evidence.ID); ref != "" {
		refs = append(refs, "evidence:"+ref)
	}
	return refs
}

func acceptanceCriterionSatisfied(criterion string, outputSummary string, evidenceSummaries []string) bool {
	expected := strings.TrimSpace(criterion)
	if expected == "" {
		return true
	}
	if strings.Contains(strings.ToLower(outputSummary), strings.ToLower(expected)) {
		return true
	}
	return summaryContains(evidenceSummaries, expected)
}

func summaryContains(summaries []string, expected string) bool {
	target := strings.ToLower(strings.TrimSpace(expected))
	if target == "" {
		return true
	}
	for _, summary := range summaries {
		if strings.Contains(strings.ToLower(strings.TrimSpace(summary)), target) {
			return true
		}
	}
	return false
}

func normalizeToolNames(requested []string) []string {
	seen := make(map[string]struct{}, len(requested))
	result := make([]string, 0, len(requested))
	for _, name := range requested {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func truncateTaskTitle(task string) string {
	title := strings.TrimSpace(task)
	if len(title) <= 80 {
		return title
	}
	return title[:77] + "..."
}

func childCtxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

type PendingResumeStore interface {
	FindLatestInterruptedRun(ctx context.Context) (*events.RunRecord, error)
}

type PendingResumeInfo struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Input     string    `json:"input"`
	CreatedAt time.Time `json:"created_at"`
}

func FindPendingResume(ctx context.Context, store PendingResumeStore) (*PendingResumeInfo, error) {
	run, err := store.FindLatestInterruptedRun(ctx)
	if err != nil {
		return nil, fmt.Errorf("find latest interrupted run: %w", err)
	}
	if run == nil {
		return nil, nil
	}
	return &PendingResumeInfo{
		RunID:     run.RunID,
		SessionID: run.SessionID,
		Input:     run.Input,
		CreatedAt: run.CreatedAt,
	}, nil
}

func resolveRootOrchestrationMode(req ExecuteRequest) events.OrchestrationMode {
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if req.OrchestrationMode != "" {
		return mode
	}
	if strings.TrimSpace(req.ParentRunID) != "" {
		return events.ModeSingleAgent
	}
	if strings.TrimSpace(req.SkillID) != "" {
		return events.ModePlanExecute
	}
	return events.ModeDirectResponse
}
