package mcp

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/core"
)

func TestReconcileProvidersAddsNewProvider(t *testing.T) {
	binary := buildFixtureServer(t)

	cfg := ProviderConfig{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}

	mgr, err := NewManager(context.Background(), []ProviderConfig{cfg})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	statuses := mgr.Statuses()
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("initial status count = %d, want %d", got, want)
	}

	err = mgr.ReconcileProviders(context.Background(), []ProviderConfig{
		cfg,
		{
			Name:                  "beta",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
	})
	if err != nil {
		t.Fatalf("ReconcileProviders: %v", err)
	}

	statuses = mgr.Statuses()
	if got, want := len(statuses), 2; got != want {
		t.Fatalf("status count after add = %d, want %d", got, want)
	}

	var foundBeta bool
	for _, s := range statuses {
		if s.Name == "beta" {
			foundBeta = true
			if s.StartupStatus != "healthy" {
				t.Fatalf("added provider beta status = %q, want healthy", s.StartupStatus)
			}
		}
	}
	if !foundBeta {
		t.Fatal("beta provider not found in statuses after add")
	}

}

func TestReconcileProvidersRemovesProvider(t *testing.T) {
	binary := buildFixtureServer(t)

	mgr, err := NewManager(context.Background(), []ProviderConfig{
		{
			Name:                  "alpha",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "beta",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	err = mgr.ReconcileProviders(context.Background(), []ProviderConfig{{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("ReconcileProviders: %v", err)
	}

	statuses := mgr.Statuses()
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("status count after remove = %d, want %d", got, want)
	}
	if statuses[0].Name != "alpha" {
		t.Fatalf("remaining provider = %q, want alpha", statuses[0].Name)
	}

}

func TestReconcileProvidersRestartsChangedProvider(t *testing.T) {
	binary := buildFixtureServer(t)

	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	initialTools := mgr.Tools()
	if got := len(initialTools); got == 0 {
		t.Fatal("expected tools from initial provider")
	}

	err = mgr.ReconcileProviders(context.Background(), []ProviderConfig{{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 15,
	}})
	if err != nil {
		t.Fatalf("ReconcileProviders: %v", err)
	}

	statuses := mgr.Statuses()
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("status count after restart = %d, want %d", got, want)
	}
	if statuses[0].StartupStatus != "healthy" {
		t.Fatalf("restarted provider status = %q, want healthy", statuses[0].StartupStatus)
	}

	restartedTools := mgr.Tools()
	if got := len(restartedTools); got == 0 {
		t.Fatal("expected tools after provider restart")
	}
}

func TestReconcileProvidersPreservesUnchangedSlots(t *testing.T) {
	binary := buildFixtureServer(t)

	origFunc := connectProviderFunc
	t.Cleanup(func() { connectProviderFunc = origFunc })

	var connectCalls atomic.Int32
	connectProviderFunc = func(ctx context.Context, cfg ProviderConfig, opts *mcp.ClientOptions, store core.ArtifactStore, onAuthStatusChanged func(status string)) (*provider, error) {
		connectCalls.Add(1)
		return connectProvider(ctx, cfg, opts, store, onAuthStatusChanged)
	}

	mgr, err := NewManager(context.Background(), []ProviderConfig{
		{
			Name:                  "alpha",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "beta",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	initialConnectCalls := connectCalls.Load()

	err = mgr.ReconcileProviders(context.Background(), []ProviderConfig{
		{
			Name:                  "alpha",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "beta",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
	})
	if err != nil {
		t.Fatalf("ReconcileProviders: %v", err)
	}

	if connectCalls.Load() != initialConnectCalls {
		t.Fatalf("expected no new connect calls for unchanged providers, got %d additional calls", connectCalls.Load()-initialConnectCalls)
	}

	statuses := mgr.Statuses()
	if got, want := len(statuses), 2; got != want {
		t.Fatalf("status count after no-change reconcile = %d, want %d", got, want)
	}
	for _, s := range statuses {
		if s.StartupStatus != "healthy" {
			t.Fatalf("provider %s status = %q, want healthy", s.Name, s.StartupStatus)
		}
	}
}

func TestReconcileProvidersDeterministicOrder(t *testing.T) {
	binary := buildFixtureServer(t)

	mgr, err := NewManager(context.Background(), []ProviderConfig{
		{
			Name:                  "alpha",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "beta",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	err = mgr.ReconcileProviders(context.Background(), []ProviderConfig{
		{
			Name:                  "alpha",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "beta",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
		{
			Name:                  "gamma",
			Enabled:               true,
			Transport:             "stdio",
			Command:               binary,
			StartupTimeoutSeconds: 10,
		},
	})
	if err != nil {
		t.Fatalf("ReconcileProviders: %v", err)
	}

	statuses := mgr.Statuses()
	if got, want := len(statuses), 3; got != want {
		t.Fatalf("status count = %d, want %d", got, want)
	}

	wantOrder := []string{"alpha", "beta", "gamma"}
	for i, want := range wantOrder {
		if statuses[i].Name != want {
			t.Fatalf("status[%d].Name = %q, want %q", i, statuses[i].Name, want)
		}
	}
}

func TestReconcileProvidersRestartFailureReturnsError(t *testing.T) {
	binary := buildFixtureServer(t)

	origFunc := connectProviderFunc
	t.Cleanup(func() { connectProviderFunc = origFunc })

	var connectCalls atomic.Int32
	connectProviderFunc = func(ctx context.Context, cfg ProviderConfig, opts *mcp.ClientOptions, store core.ArtifactStore, onAuthStatusChanged func(status string)) (*provider, error) {
		connectCalls.Add(1)
		if connectCalls.Load() == 2 {
			return nil, errors.New("restart connection failed")
		}
		return connectProvider(ctx, cfg, opts, store, onAuthStatusChanged)
	}

	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	err = mgr.ReconcileProviders(context.Background(), []ProviderConfig{{
		Name:                  "alpha",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 20,
	}})
	if err == nil {
		t.Fatal("expected ReconcileProviders to return error on restart failure")
	}
	if !strings.Contains(err.Error(), "restart connection failed") {
		t.Fatalf("expected restart failure in error, got %v", err)
	}

	statuses := mgr.Statuses()
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("status count = %d, want %d", got, want)
	}
	if statuses[0].StartupStatus != "failed" {
		t.Fatalf("restarted provider status = %q, want failed", statuses[0].StartupStatus)
	}
	if statuses[0].Error == "" {
		t.Fatal("expected error to be recorded on failed restart slot")
	}

}
