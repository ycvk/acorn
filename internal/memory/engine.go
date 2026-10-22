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
	Ready     func() error
	Store     core.MemoryStore
	Model     Model
	ModelName string
	Embedder  embedding.Embedder
	Index     core.MemoryIndex
	Count     func(context.Context, string) (int, error)
	Clock     func() time.Time
	// Settings gives the owner's time zone and the memory and autonomous
	// token budgets at each call.
	Settings core.SettingsReader
	// Owner says how extracted facts refer to the owner; an unnamed owner
	// is "owner".
	Owner OwnerReader
}

// Token limits of memory work.
const (
	// BatchTokens bounds the input of one memory model call.
	BatchTokens = 8192
	// ContextTokens bounds one automatic recall.
	ContextTokens = 8192
	// HistoryTokens bounds the recent thread text and summaries a run loads.
	HistoryTokens = 65536
)

// OwnerReader reads what Acorn calls the owner.
type OwnerReader interface {
	OwnerName(ctx context.Context) (core.OwnerName, error)
}

type Engine struct{ cfg Config }

func New(cfg Config) (*Engine, error) {
	if cfg.Store == nil || cfg.Model == nil || cfg.Embedder == nil || cfg.Count == nil || cfg.Clock == nil || cfg.Settings == nil || cfg.Owner == nil {
		return nil, errors.New("memory requires store, model, embedder, token counter, clock, settings and owner name reader")
	}
	if cfg.Index.Model == "" || cfg.Index.Dimensions <= 0 {
		return nil, errors.New("memory requires model names and embedding dimensions")
	}
	return &Engine{cfg: cfg}, nil
}

func (e *Engine) Store() core.MemoryStore { return e.cfg.Store }

func midnight(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
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
