package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/contextplane"
)

func emitContextCompressedEvent(
	ctx context.Context,
	store eventAppender,
	outcome contextplane.CompressionOutcome,
) error {
	if store == nil {
		return nil
	}
	runID := strings.TrimSpace(CurrentRunID(ctx))
	if runID == "" {
		return nil
	}
	_, err := appendStreamItem(ctx, store, CurrentStreamSink(ctx), StreamItem{
		RunID:     runID,
		Kind:      StreamKindContextCompressed,
		CreatedAt: time.Now().UTC(),
		Payload: &ContextCompressedPayload{ContextCompressed: &StreamContextCompressed{
			BoundaryID:     outcome.BoundaryID,
			FirstIndex:     outcome.FirstIndex,
			LastIndex:      outcome.LastIndex,
			TokensBefore:   outcome.TokensBefore,
			TokensAfter:    outcome.TokensAfter,
			SummarySnippet: outcome.SummarySnippet,
		}},
	})
	return err
}

func emitContextPressureEvent(
	ctx context.Context,
	store eventAppender,
	pressure contextplane.BudgetPressure,
) error {
	if store == nil {
		return nil
	}
	runID := strings.TrimSpace(CurrentRunID(ctx))
	if runID == "" {
		return nil
	}
	_, err := appendStreamItem(ctx, store, CurrentStreamSink(ctx), StreamItem{
		RunID:     runID,
		Kind:      StreamKindContextPressure,
		CreatedAt: time.Now().UTC(),
		Payload: &ContextPressurePayload{ContextPressure: &StreamContextPressure{
			State:                      string(pressure.State),
			EstimatedInputTokens:       pressure.EstimatedInputTokens,
			EffectiveWindowTokens:      pressure.EffectiveWindowTokens,
			WarningThresholdTokens:     pressure.WarningThresholdTokens,
			AutoCompactThresholdTokens: pressure.AutoCompactThresholdTokens,
			BlockingThresholdTokens:    pressure.BlockingThresholdTokens,
			PercentUsed:                pressure.PercentUsed,
		}},
	})
	return err
}
