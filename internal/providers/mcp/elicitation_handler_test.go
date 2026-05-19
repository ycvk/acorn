package mcpprovider

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/store/sqlite"
)

func (h *ElicitationHandler) setTimeoutForTest(d time.Duration) {
	h.timeout = d
}

func TestHandleElicitationCreatesPendingAction(t *testing.T) {
	store, cleanup := openTestStore(t)
	defer cleanup()

	ctx := context.Background()
	runID := "run_test_elicitation_001"
	if err := store.CreateRun(ctx, runID, "test elicitation", ""); err != nil {
		t.Fatalf("create run: %v", err)
	}

	handler := newElicitationHandler(store, nil)
	handler.setActiveRunID(runID)

	req := &mcp.ElicitRequest{
		Params: &mcp.ElicitParams{
			Message: "Please confirm this action",
		},
	}

	// Start handler in background so we can inspect the PendingAction
	resultCh := make(chan *mcp.ElicitResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := handler.HandleElicitation(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	// Wait briefly for the PendingAction to be created
	time.Sleep(200 * time.Millisecond)

	// List pending actions and check one was created with Kind="elicitation"
	actions, err := store.ListPendingActions(ctx, 10)
	if err != nil {
		t.Fatalf("list pending actions: %v", err)
	}

	var foundAction *events.PendingActionRecord
	for i := range actions {
		if actions[i].Kind == events.PendingActionKindElicitation {
			foundAction = &actions[i]
			break
		}
	}
	if foundAction == nil {
		t.Fatal("no pending action with Kind=\"elicitation\" found")
	}
	if foundAction.Subject != "elicitation" {
		t.Errorf("Subject = %q, want %q", foundAction.Subject, "elicitation")
	}
	if foundAction.RunID != runID {
		t.Errorf("RunID = %q, want %q", foundAction.RunID, runID)
	}

	// Decide the action to unblock the handler
	_, err = store.DecidePendingAction(ctx, foundAction.ActionID, events.PendingActionStatusApproved, events.PendingActionModeDeferred, `{"action":"accept"}`)
	if err != nil {
		t.Fatalf("decide pending action: %v", err)
	}

	// Check the handler returned accept
	select {
	case result := <-resultCh:
		if result.Action != "accept" {
			t.Errorf("Action = %q, want %q", result.Action, "accept")
		}
	case err := <-errCh:
		t.Fatalf("handler error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return within timeout")
	}
}

func TestHandleElicitationTimeoutReturnsDecline(t *testing.T) {
	store, cleanup := openTestStore(t)
	defer cleanup()

	ctx := context.Background()
	runID := "run_test_elicitation_timeout_001"
	if err := store.CreateRun(ctx, runID, "test timeout", ""); err != nil {
		t.Fatalf("create run: %v", err)
	}

	handler := newElicitationHandler(store, nil)
	handler.setActiveRunID(runID)
	handler.setTimeoutForTest(100 * time.Millisecond) // short timeout for test

	req := &mcp.ElicitRequest{
		Params: &mcp.ElicitParams{
			Message: "Will this timeout?",
		},
	}

	result, err := handler.HandleElicitation(ctx, req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.Action != "decline" {
		t.Errorf("Action = %q, want %q on timeout", result.Action, "decline")
	}
}

func TestHandleElicitationLoadFailureReturnsError(t *testing.T) {
	store, cleanup := openTestStore(t)
	defer cleanup()

	ctx := context.Background()
	runID := "run_test_elicitation_load_error_001"
	if err := store.CreateRun(ctx, runID, "test load error", ""); err != nil {
		t.Fatalf("create run: %v", err)
	}

	handler := newElicitationHandler(failingPendingActionStore{
		PendingActionStore: store,
		loadErr:            errors.New("sqlite is locked"),
	}, nil)
	handler.setActiveRunID(runID)
	handler.setTimeoutForTest(50 * time.Millisecond)

	req := &mcp.ElicitRequest{
		Params: &mcp.ElicitParams{
			Message: "Will this fail loudly?",
		},
	}

	_, err := handler.HandleElicitation(ctx, req)
	if err == nil {
		t.Fatal("expected load pending action failure")
	}
	if !containsAll(err.Error(), "load elicitation pending action", "sqlite is locked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleElicitationNoActiveRunReturnsDecline(t *testing.T) {
	store, cleanup := openTestStore(t)
	defer cleanup()

	handler := newElicitationHandler(store, nil)
	// No active run ID set

	req := &mcp.ElicitRequest{
		Params: &mcp.ElicitParams{
			Message: "No run context",
		},
	}

	result, err := handler.HandleElicitation(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.Action != "decline" {
		t.Errorf("Action = %q, want %q when no active run", result.Action, "decline")
	}
}

func TestHandleElicitationEmitsStreamItems(t *testing.T) {
	store, cleanup := openTestStore(t)
	defer cleanup()

	ctx := context.Background()
	runID := "run_test_elicitation_stream_001"
	if err := store.CreateRun(ctx, runID, "test stream", ""); err != nil {
		t.Fatalf("create run: %v", err)
	}

	var emitted []ProviderEvent
	var mu sync.Mutex
	onEvent := func(event ProviderEvent) {
		mu.Lock()
		emitted = append(emitted, event)
		mu.Unlock()
	}

	handler := newElicitationHandler(store, onEvent)
	handler.setActiveRunID(runID)
	handler.setTimeoutForTest(100 * time.Millisecond)

	req := &mcp.ElicitRequest{
		Params: &mcp.ElicitParams{
			Message: "Check stream emission",
		},
	}

	_, _ = handler.HandleElicitation(ctx, req)

	// Verify that pending and decided stream events were emitted via events table
	events_list, err := store.LoadEvents(ctx, runID)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}

	var foundPending, foundDecided bool
	for _, e := range events_list {
		if e.Kind == "elicitation.pending" {
			foundPending = true
		}
		if e.Kind == "elicitation.decided" {
			foundDecided = true
		}
	}
	if !foundPending {
		t.Error("expected elicitation.pending event not found")
	}
	if !foundDecided {
		t.Error("expected elicitation.decided event not found")
	}
}

// openTestStore creates a temporary SQLite store for testing.
func openTestStore(t *testing.T) (*sqlite.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(dir)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	return store, func() { store.Close() }
}

type failingPendingActionStore struct {
	PendingActionStore
	loadErr error
}

func (s failingPendingActionStore) LoadPendingAction(ctx context.Context, actionID string) (*events.PendingActionRecord, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return s.PendingActionStore.LoadPendingAction(ctx, actionID)
}

func containsAll(input string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(input, want) {
			return false
		}
	}
	return true
}

// TestBuildElicitationHandlerReturnsValidHandler tests that buildElicitationHandler
// on Manager produces a working handler.
func TestBuildElicitationHandlerReturnsValidHandler(t *testing.T) {
	store, cleanup := openTestStore(t)
	defer cleanup()

	ctx := context.Background()
	runID := "run_test_build_handler_001"
	if err := store.CreateRun(ctx, runID, "test build handler", ""); err != nil {
		t.Fatalf("create run: %v", err)
	}

	mgr := &Manager{
		store:       store,
		onEvent:     func(event ProviderEvent) {},
		elicitation: newElicitationHandler(store, func(event ProviderEvent) {}),
	}
	mgr.SetActiveRunID(runID)

	handler := mgr.buildElicitationHandler()
	if handler == nil {
		t.Fatal("buildElicitationHandler returned nil")
	}

	req := &mcp.ElicitRequest{
		Params: &mcp.ElicitParams{
			Message: "Via manager",
		},
	}

	// Start handler, then decide
	resultCh := make(chan *mcp.ElicitResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := handler(ctx, req)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()

	time.Sleep(200 * time.Millisecond)

	actions, _ := store.ListPendingActions(ctx, 10)
	for _, a := range actions {
		if a.Kind == events.PendingActionKindElicitation && a.Status == events.PendingActionStatusPending {
			store.DecidePendingAction(ctx, a.ActionID, events.PendingActionStatusRejected, events.PendingActionModeDeferred, `{"action":"decline"}`)
			break
		}
	}

	select {
	case result := <-resultCh:
		if result.Action != "decline" {
			t.Errorf("Action = %q, want %q", result.Action, "decline")
		}
	case err := <-errCh:
		t.Fatalf("handler error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return within timeout")
	}
}

// TestBuildCreateMessageHandlerReturnsNilWithoutStore tests that buildCreateMessageHandler
// returns nil when the sampling handler is not configured (no store).
func TestBuildCreateMessageHandlerReturnsNilWithoutStore(t *testing.T) {
	mgr := &Manager{}
	handler := mgr.buildCreateMessageHandler()
	if handler != nil {
		t.Fatal("expected nil handler when sampling is not configured")
	}
}

// TestBuildCreateMessageHandlerReturnsHandlerWithStore tests that buildCreateMessageHandler
// returns a handler when the sampling handler is configured.
func TestBuildCreateMessageHandlerReturnsHandlerWithStore(t *testing.T) {
	mgr := &Manager{}
	mgr.sampling = newSamplingHandler(mgr)
	handler := mgr.buildCreateMessageHandler()
	if handler == nil {
		t.Fatal("expected non-nil handler when sampling is configured")
	}
}

// TestManagerSamplingDepthField tests that the Manager struct has a samplingDepth field.
func TestManagerSamplingDepthField(t *testing.T) {
	mgr := &Manager{}
	if mgr.samplingDepth != 0 {
		t.Errorf("samplingDepth = %d, want 0", mgr.samplingDepth)
	}
	mgr.samplingDepth = 3
	if mgr.samplingDepth != 3 {
		t.Errorf("samplingDepth = %d, want 3", mgr.samplingDepth)
	}
}
