// Package memory derives sourced personal records and retrieves them for runs.
package memory

import (
	"context"
	"errors"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/ycvk/acorn/internal/core"
)

type Generation struct {
	Text         string
	InputTokens  int
	OutputTokens int
	Reported     bool
}

type Model interface {
	GenerateMemory(context.Context, string, string, int) (Generation, error)
}

type Config struct {
	Ready            func() error
	SyncSources      func(context.Context) error
	Store            core.MemoryStore
	Model            Model
	ModelName        string
	Embedder         embedding.Embedder
	Index            core.MemoryIndex
	Count            func(context.Context, string) (int, error)
	Clock            func() time.Time
	Location         *time.Location
	DailyTokens      int
	AutonomousTokens int
	BatchTokens      int
	ContextTokens    int
	HistoryTokens    int
}

type Engine struct{ cfg Config }

func New(cfg Config) (*Engine, error) {
	if cfg.Store == nil || cfg.Model == nil || cfg.Embedder == nil || cfg.Count == nil || cfg.Clock == nil || cfg.Location == nil {
		return nil, errors.New("memory requires store, model, embedder, token counter, clock and location")
	}
	if cfg.Index.Model == "" || cfg.Index.Dimensions <= 0 || cfg.BatchTokens < 256 || cfg.ContextTokens < 256 || cfg.HistoryTokens < 256 {
		return nil, errors.New("memory requires model names, embedding dimensions and positive token budgets")
	}
	if cfg.DailyTokens < 0 || cfg.AutonomousTokens < 0 {
		return nil, errors.New("memory daily budgets must be >= 0")
	}
	return &Engine{cfg: cfg}, nil
}

func (e *Engine) Store() core.MemoryStore { return e.cfg.Store }

func (e *Engine) midnight(now time.Time) time.Time {
	local := now.In(e.cfg.Location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, e.cfg.Location)
}

type callScope struct {
	runID, budget string
	jobID         int64
}
type scopeKey struct{}

func WithRun(ctx context.Context, runID string, autonomous bool) context.Context {
	budget := "owner"
	if autonomous {
		budget = "autonomous"
	}
	return context.WithValue(ctx, scopeKey{}, callScope{runID: runID, budget: budget})
}

func scopeFrom(ctx context.Context) callScope {
	if scope, ok := ctx.Value(scopeKey{}).(callScope); ok {
		return scope
	}
	return callScope{budget: "memory"}
}
