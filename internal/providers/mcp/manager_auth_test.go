package mcpprovider

import (
	"context"
	"testing"
)

func TestAuthStatusTransitionOnOAuthConnect(t *testing.T) {
	binary := buildFixtureServer(t)

	var events []ProviderEvent
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "provider",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}}, WithEventCallback(func(ev ProviderEvent) {
		events = append(events, ev)
	}))
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

	found := false
	for _, ev := range events {
		if ev.Kind == "auth_status_changed" && ev.Provider == "provider" && ev.AuthStatus == "authenticated" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auth_status_changed event with AuthStatus=authenticated, got events: %v", events)
	}
}

func TestAuthStatusExpiredOnReconnectWithExpiredToken(t *testing.T) {
	binary := buildFixtureServer(t)

	var events []ProviderEvent
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "provider",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}}, WithEventCallback(func(ev ProviderEvent) {
		events = append(events, ev)
	}))
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

	found := false
	for _, ev := range events {
		if ev.Kind == "auth_status_changed" && ev.AuthStatus == "expired" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auth_status_changed event with AuthStatus=expired, got events: %v", events)
	}
}

func TestAuthStatusNoDuplicateTransition(t *testing.T) {
	binary := buildFixtureServer(t)

	var authEvents []ProviderEvent
	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}}, WithEventCallback(func(ev ProviderEvent) {
		if ev.Kind == "auth_status_changed" {
			authEvents = append(authEvents, ev)
		}
	}))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	mgr.updateProviderAuthStatus("fixture", "authenticated")
	if got, want := len(authEvents), 1; got != want {
		t.Fatalf("expected %d auth_status_changed events, got %d", want, got)
	}

	mgr.updateProviderAuthStatus("fixture", "authenticated")
	if got, want := len(authEvents), 1; got != want {
		t.Fatalf("expected no duplicate auth_status_changed event, got %d events", len(authEvents))
	}

	mgr.updateProviderAuthStatus("fixture", "expired")
	if got, want := len(authEvents), 2; got != want {
		t.Fatalf("expected %d auth_status_changed events after status change, got %d", want, got)
	}
}
