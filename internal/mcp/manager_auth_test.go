package mcp

import (
	"context"
	"testing"
)

func TestAuthStatusTransitionOnOAuthConnect(t *testing.T) {
	binary := buildFixtureServer(t)

	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "provider",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	mgr.updateProviderAuthStatus("provider", "authenticated")

	statuses := mgr.Statuses()
	if got, want := len(statuses), 1; got != want {
		t.Fatalf("status count = %d, want %d", got, want)
	}
	if got, want := statuses[0].AuthStatus, "authenticated"; got != want {
		t.Fatalf("AuthStatus = %q, want %q", got, want)
	}

}

func TestAuthStatusExpiredOnReconnectWithExpiredToken(t *testing.T) {
	binary := buildFixtureServer(t)

	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "provider",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	mgr.updateProviderAuthStatus("provider", "authenticated")
	statuses := mgr.Statuses()
	if got, want := statuses[0].AuthStatus, "authenticated"; got != want {
		t.Fatalf("initial AuthStatus = %q, want %q", got, want)
	}

	mgr.updateProviderAuthStatus("provider", "expired")

	statuses = mgr.Statuses()
	if got, want := statuses[0].AuthStatus, "expired"; got != want {
		t.Fatalf("AuthStatus after expiry = %q, want %q", got, want)
	}

}

func TestAuthStatusNoDuplicateTransition(t *testing.T) {
	binary := buildFixtureServer(t)

	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	mgr.updateProviderAuthStatus("fixture", "authenticated")
	statuses := mgr.Statuses()
	if got, want := statuses[0].AuthStatus, "authenticated"; got != want {
		t.Fatalf("AuthStatus = %q, want %q", got, want)
	}

	mgr.updateProviderAuthStatus("fixture", "authenticated")
	statuses = mgr.Statuses()
	if got, want := statuses[0].AuthStatus, "authenticated"; got != want {
		t.Fatalf("AuthStatus after duplicate update = %q, want %q", got, want)
	}

	mgr.updateProviderAuthStatus("fixture", "expired")
	statuses = mgr.Statuses()
	if got, want := statuses[0].AuthStatus, "expired"; got != want {
		t.Fatalf("AuthStatus after status change = %q, want %q", got, want)
	}
}
