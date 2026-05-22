package contextplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/runtimehistory"
)

func (p *defaultContextPlane) Assemble(ctx context.Context, req AssembleRequest) (*AssembleResult, error) {
	if p == nil {
		return nil, errors.New("context plane is not initialized")
	}
	if p.tokenCounter == nil {
		return nil, errors.New("context plane token counter is required")
	}
	budget, err := p.Budget(ctx, BudgetRequest{
		TotalTokens:     p.memorySearchTokenBudget,
		PresentSections: assemblePresentSections(req),
		Hint:            req.Hint,
	})
	if err != nil {
		return nil, err
	}

	var (
		sessionSummary    string
		checkpointSection string
	)

	assembledContext, err := runContextAssembler{
		store:             p.store,
		checkpointService: p.checkpointService,
	}.Assemble(ctx, req)
	if err != nil {
		return nil, err
	}
	if assembledContext != nil {
		checkpointSection = assembledContext.checkpointSection
	}
	if strings.TrimSpace(req.Input) != "" && strings.TrimSpace(req.SessionID) != "" && !isNilInterface(p.sessionSummaryService) {
		summary, summaryErr := p.sessionSummaryService.Get(ctx, req.SessionID)
		if summaryErr != nil {
			return nil, fmt.Errorf("load session summary for %q: %w", req.SessionID, summaryErr)
		}
		sessionSummary = runtimehistory.FormatSessionSummaryForPrompt(summary)
	}

	memoryPacket, err := buildMemoryContextPacket(ctx, p.tokenCounter, p.memoryBudget, sessionSummary, checkpointSection, req.MemoryPrepared)
	if err != nil {
		return nil, err
	}
	memoryMessage := buildMemoryMessageFromPacket(memoryPacket)
	messages, err := budgetedContextMessages(ctx, p.tokenCounter, p.maxContextTokens, filterMessages(
		buildSkillContextMessage(req.SelectedSkill),
		buildSkillCatalogMessage(req.SkillSnapshot),
		memoryMessage,
	))
	if err != nil {
		return nil, err
	}
	lifecycleState := newToolLifecycleState(ctx, req)
	deferredNames := sortedDeferredToolNames(lifecycleState)

	return &AssembleResult{
		Messages:          messages,
		BudgetUsed:        budget,
		LifecycleState:    lifecycleState,
		EagerToolNames:    sortedLoadedToolNames(lifecycleState),
		DeferredToolNames: deferredNames,
		ProcedureActivations: procedureActivationsForMemoryPacket(
			req.MemoryPrepared,
			memoryPacket,
		),
	}, nil
}

func assemblePresentSections(req AssembleRequest) []Section {
	sections := []Section{SectionMemory, SectionToolDef, SectionConversation}
	if req.SelectedSkill != nil {
		sections = append([]Section{SectionSkill}, sections...)
	}
	return sections
}

func filterMessages(messages ...*schema.Message) []*schema.Message {
	result := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg != nil {
			result = append(result, msg)
		}
	}
	return result
}

func budgetedContextMessages(ctx context.Context, counter TokenCounter, maxTokens int, messages []*schema.Message) ([]*schema.Message, error) {
	if counter == nil {
		return nil, errors.New("context message token counter is required")
	}
	if maxTokens <= 0 {
		return cloneMessages(messages), nil
	}
	cloned := cloneMessages(messages)
	adkMessages := make([]adk.Message, 0, len(cloned))
	for _, msg := range cloned {
		if msg != nil {
			adkMessages = append(adkMessages, msg)
		}
	}
	total, err := counter.CountMessages(ctx, adkMessages, nil)
	if err != nil {
		return nil, fmt.Errorf("count context message tokens: %w", err)
	}
	if total > maxTokens {
		return nil, fmt.Errorf("assembled context requires %d tokens over budget %d", total, maxTokens)
	}
	return cloned, nil
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		out = append(out, new(*msg))
	}
	return out
}
