package wire

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

func buildMemory(ctx context.Context, cfg *config.Config, db *store.Store, owner core.SettingsReader, clock func() time.Time, rebuild bool) (*memory.Engine, error) {
	model, err := runtime.NewMemoryModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	counter, err := runtime.NewTokenCounter()
	if err != nil {
		return nil, err
	}
	embed := cfg.Memory.Embedding
	voyage, err := memory.NewVoyage(memory.VoyageConfig{BaseURL: embed.BaseURL, APIKey: embed.APIKey, Model: embed.Model, Dimensions: embed.Dimensions, Client: &http.Client{Timeout: 60 * time.Second}})
	if err != nil {
		return nil, err
	}
	index, err := db.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: embed.Model, Dimensions: embed.Dimensions}, rebuild)
	if err != nil {
		return nil, err
	}
	if !rebuild && index.State != "ready" {
		return nil, fmt.Errorf("memory index generation %d is %s; finish acorn memory reindex before serve", index.Generation, index.State)
	}
	provider, providerErr := cfg.EnabledProvider()
	ready := func() error {
		if providerErr != nil {
			return providerErr
		}
		if err := cfg.ValidateExecutionReady(); err != nil {
			return err
		}
		budget, err := cfg.InputTokenBudget()
		if err != nil {
			return err
		}
		if memory.BatchTokens >= budget {
			return fmt.Errorf("the model's input budget of %d tokens is too small for a memory batch of %d", budget, memory.BatchTokens)
		}
		return nil
	}
	return memory.New(memory.Config{Ready: ready, Store: db, Model: model, ModelName: provider.Model, Embedder: voyage, Index: index, Count: counter.CountText, Clock: clock, Settings: owner, Owner: db})
}

func (c *Container) MemoryWorker() *memory.Engine { return c.memory }
func (c *Container) MemoryStatus(ctx context.Context) (core.MemoryProcessingStatus, error) {
	status, err := c.store.MemoryProcessingStatus(ctx)
	if err != nil {
		return status, err
	}
	current, err := c.settings.Settings(ctx)
	if err != nil {
		return status, err
	}
	now := c.clock().In(current.Location)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, current.Location)
	status.TokensToday, err = c.store.MemoryUsageSince(ctx, midnight, "memory")
	status.DailyTokenLimit = current.MemoryDailyTokens
	return status, err
}
