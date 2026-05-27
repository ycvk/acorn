package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/webaccess"
)

func TestOpenFailsWhenExecutableIsNotConfigured(t *testing.T) {
	service, err := NewService(Config{
		Timeout: time.Second,
		Policy:  webaccess.URLPolicy{},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = service.Open(context.Background(), "http://93.184.216.34/")
	if err == nil || !strings.Contains(err.Error(), "browser.executable_path is not configured") || !strings.Contains(err.Error(), "install Chrome/Chromium") {
		t.Fatalf("Open error = %v, want actionable missing executable_path", err)
	}
}

func TestOpenFailsWhenExecutablePathIsNotAccessible(t *testing.T) {
	service, err := NewService(Config{
		ExecutablePath: "/not-a-real-acorn-browser",
		Timeout:        time.Second,
		Policy:         webaccess.URLPolicy{},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = service.Open(context.Background(), "http://93.184.216.34/")
	if err == nil || !strings.Contains(err.Error(), "browser.executable_path") || !strings.Contains(err.Error(), "not accessible") || !strings.Contains(err.Error(), "install Chrome/Chromium") {
		t.Fatalf("Open error = %v, want actionable inaccessible executable_path", err)
	}
}

func TestResolveSelectorRequiresFreshSnapshotRef(t *testing.T) {
	service, err := NewService(Config{Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.snapshotGeneration = 2
	service.snapshotRefs["e1"] = snapshotRef{Selector: "#name", Generation: 2}

	got, err := service.resolveSelector("e1", "")
	if err != nil {
		t.Fatalf("resolveSelector fresh ref: %v", err)
	}
	if got != "#name" {
		t.Fatalf("selector = %q, want #name", got)
	}

	service.snapshotGeneration = 3
	_, err = service.resolveSelector("e1", "")
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("resolveSelector expired error = %v, want expired", err)
	}
}

func TestResolveSelectorRejectsAmbiguousSelectorSource(t *testing.T) {
	service, err := NewService(Config{Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_, err = service.resolveSelector("e1", "#name")
	if err == nil || !strings.Contains(err.Error(), "ref or selector") {
		t.Fatalf("resolveSelector error = %v, want ambiguity error", err)
	}
}

func TestConsoleAndNetworkBuffersAreExplicitlyEnabled(t *testing.T) {
	service, err := NewService(Config{Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.handleEvent(nil)
	if got := service.ConsoleList(); got.Enabled || len(got.Entries) != 0 {
		t.Fatalf("console list before start = %+v, want disabled empty", got)
	}
	if got := service.NetworkList(); got.Enabled || len(got.Entries) != 0 {
		t.Fatalf("network list before start = %+v, want disabled empty", got)
	}
}

func TestPausedRequestAppliesURLPolicy(t *testing.T) {
	service, err := NewService(Config{
		Timeout: time.Second,
		Policy:  webaccess.URLPolicy{},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if service.handlePausedRequestForTest(context.Background(), "http://127.0.0.1/admin") {
		t.Fatal("loopback request should be blocked")
	}
	if got := service.takePolicyError(); !strings.Contains(got, "loopback_address") {
		t.Fatalf("policy error = %q, want loopback address", got)
	}
	if !service.handlePausedRequestForTest(context.Background(), "data:image/png;base64,AAAA") {
		t.Fatal("same-page browser data resource should be allowed")
	}
}
