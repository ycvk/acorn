package wire

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/news"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/webaccess"
)

// buildNews builds the processor that sorts news items into stories, with
// the model background memory work uses and the pages of issues read under
// the outbound policy.
func buildNews(ctx context.Context, cfg *config.Config, db *store.Store, owner core.SettingsReader, fetcher *webaccess.FetchService, clock func() time.Time) (*news.Processor, error) {
	model, err := runtime.NewMemoryModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	counter, err := runtime.NewTokenCounter()
	if err != nil {
		return nil, err
	}
	provider, providerErr := cfg.EnabledProvider()
	ready := func() error {
		if providerErr != nil {
			return providerErr
		}
		return cfg.ValidateExecutionReady()
	}
	return news.New(news.Config{
		Store: db, Model: newsModel{model: model}, ModelName: provider.Model, Pages: newsPages{fetcher: fetcher},
		Count: counter.CountText, Clock: clock, Settings: owner, Ready: ready,
	})
}

// newsModel calls the model background memory work uses.
type newsModel struct{ model memory.Model }

func (m newsModel) GenerateNews(ctx context.Context, instruction, input string, maxOutput int) (news.Generation, error) {
	g, err := m.model.GenerateMemory(ctx, instruction, input, maxOutput)
	return news.Generation{Text: g.Text, InputTokens: g.InputTokens, OutputTokens: g.OutputTokens, Reported: g.Reported}, err
}

func (c *Container) NewsWorker() *news.Processor { return c.news }
