package clientevents

import (
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/stream"
)

func BuildTraceSummary(raw []events.EventRecord) (*TraceSummary, error) {
	summary, err := stream.BuildTraceSummary(raw)
	if err != nil {
		return nil, err
	}
	return TraceSummaryFromStream(summary), nil
}

func TraceSummaryFromStream(summary *stream.TraceSummary) *TraceSummary {
	if summary == nil {
		return nil
	}
	return &TraceSummary{
		ItemCount:                  summary.ItemCount,
		LastKind:                   string(summary.LastKind),
		AssistantMessageCount:      summary.AssistantMessageCount,
		AssistantDeltaCount:        summary.AssistantDeltaCount,
		AssistantDeltaMessageCount: summary.AssistantDeltaMessageCount,
		AssistantDeltaCharCount:    summary.AssistantDeltaCharCount,
		ToolCallCount:              summary.ToolCallCount,
		DecisionEventCount:         summary.DecisionEventCount,
		SkillEventCount:            summary.SkillEventCount,
		PlanEventCount:             summary.PlanEventCount,
		DecisionSelected:           summary.DecisionSelected,
		DecisionBlocked:            summary.DecisionBlocked,
		SkillSelected:              summary.SkillSelected,
		Interrupted:                summary.Interrupted,
		Failed:                     summary.Failed,
		Completed:                  summary.Completed,
	}
}
