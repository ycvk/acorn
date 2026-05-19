package contextplane

import (
	"context"
	"errors"
	"fmt"
)

var ErrReactiveCompactEngineRequired = errors.New("reactive compact engine requires compaction engine")

type defaultReactiveCompactEngine struct {
	engine CompactionEngine
}

func newReactiveCompactEngine(engine CompactionEngine) ReactiveCompactEngine {
	if engine == nil {
		return nil
	}
	return &defaultReactiveCompactEngine{engine: engine}
}

func (e *defaultReactiveCompactEngine) Recover(ctx context.Context, req ReactiveCompactRequest) (*ReactiveCompactResult, error) {
	if e == nil || e.engine == nil {
		return nil, ErrReactiveCompactEngineRequired
	}

	aggressivePolicy := req.PreservePolicy
	aggressivePolicy.RecentTurns = max(1, aggressivePolicy.RecentTurns/2)

	result, err := e.engine.Compact(ctx, CompactRequest{
		Trigger:        CompactTriggerReactive,
		Messages:       req.Messages,
		ToolInfos:      req.ToolInfos,
		ToolState:      req.ToolState,
		Pressure:       req.Pressure,
		PreservePolicy: aggressivePolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("reactive compact: %w", err)
	}

	return &ReactiveCompactResult{
		Messages:  result.Messages,
		Recovered: true,
	}, nil
}
