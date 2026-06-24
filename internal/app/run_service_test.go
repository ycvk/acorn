package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
	storecore "github.com/ycvk/acorn/internal/store"
)

func TestProjectRunMapsStatusAndMode(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	run, err := projectRun(domain.RunRecord{
		RunID:     "run_1",
		SessionID: "session_1",
		Status:    domain.RunStatusSucceeded,
		CreatedAt: now,
		FinishedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("projectRun: %v", err)
	}
	if run.ID != "run_1" || run.ThreadID != "session_1" || run.Status != "completed" || run.Mode != "direct" || run.CompletedAt.IsZero() {
		t.Fatalf("run = %#v", run)
	}
}

func TestProjectRunRejectsUnknownStatus(t *testing.T) {
	_, err := projectRun(domain.RunRecord{
		RunID:  "run_bad_status",
		Status: domain.RunStatus(""),
	})
	if !errors.Is(err, ErrClientProjectionFailed) {
		t.Fatalf("status error = %v, want ErrClientProjectionFailed", err)
	}
}

func TestRunServiceInterruptRunDelegatesToSharedController(t *testing.T) {
	controller := runtime.NewRunController()
	service := NewRunService(nil, nil, nil, controller)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	controller.Register("run_1", cancel)

	if err := service.InterruptRun(ctx, "run_1"); err != nil {
		t.Fatalf("interrupt: %v", err)
	}

	select {
	case <-ctx.Done():
		// success: context was cancelled by the controller
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for interrupt to cancel context")
	}
}

func TestRunServiceCreateRunUsesRealExecutorPath(t *testing.T) {
	ctx := context.Background()
	store, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	modelServer := newClientOpenAITestServer(t, "client runtime answer")
	cfg := clientRuntimeTestConfig(t, modelServer.URL+"/v1")
	runnerFactory, err := runtime.NewRunnerFactory(cfg, store, runtime.RunnerFactoryOptions{
		MemoryModule: newClientRuntimeMemoryModule(t, cfg),
	})
	if err != nil {
		t.Fatalf("NewRunnerFactory: %v", err)
	}
	executor, err := runtime.NewExecutorWithRunRuntimeAndController(cfg, store, runnerFactory, nil)
	if err != nil {
		t.Fatalf("NewExecutorWithRunRuntimeAndController: %v", err)
	}

	threads := NewThreadService(store, cfg.WorkspaceRoot())
	threads.newThreadID = func() string { return "thread_runtime" }
	service := NewRunService(store, threads, func(context.Context) (executorHandle, error) {
		return runtimeExecutorHandle{exec: executor}, nil
	}, nil)
	service.newRunID = func() string { return "run_runtime" }

	thread, err := threads.CreateThread(ctx, "runtime")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	message, err := threads.CreateMessage(ctx, thread.ID, "hello")
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}

	run, err := service.CreateRun(ctx, thread.ID, "", "")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.ID != "run_runtime" || run.ThreadID != thread.ID || run.Status != "running" || run.Mode != "direct" {
		t.Fatalf("created run = %#v", run)
	}

	waitForRunStatus(t, store, "run_runtime", domain.RunStatusSucceeded)
	records, err := store.LoadEvents(ctx, "run_runtime")
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("events = %#v, want lifecycle events from executor", records)
	}
	var sawStarted, sawMessage, sawCompleted bool
	for _, record := range records {
		switch record.Kind {
		case "run.started":
			sawStarted = true
		case "agent.message":
			sawMessage = strings.Contains(strings.TrimSpace(toClientTestJSON(record.Payload)), "client runtime answer")
		case "run.completed":
			sawCompleted = true
		}
	}
	if !sawStarted || !sawMessage || !sawCompleted {
		t.Fatalf("missing expected runtime events: started=%v message=%v completed=%v records=%#v", sawStarted, sawMessage, sawCompleted, records)
	}
	items, err := store.ListSessionMessages(ctx, thread.ID, 10)
	if err != nil {
		t.Fatalf("ListSessionMessages: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("session messages = %#v, want user + assistant", items)
	}
	if items[0].ID != mustParseMessageID(t, message.ID) || items[0].RunID != "run_runtime" {
		t.Fatalf("user message was not bound to run: %#v", items[0])
	}
	if items[1].Role != "assistant" || items[1].RunID != "run_runtime" || !strings.Contains(items[1].Content, "client runtime answer") {
		t.Fatalf("assistant message was not synced from run: %#v", items[1])
	}
}

func TestRunServiceCreateRunReturnsExecutionNotReady(t *testing.T) {
	ctx := context.Background()
	store, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	threads := NewThreadService(store, "/repo")
	threads.newThreadID = func() string { return "thread_not_ready" }
	service := NewRunService(store, threads, func(context.Context) (executorHandle, error) {
		return nil, domain.ErrExecutionNotReady
	}, nil)
	service.newRunID = func() string { return "run_not_ready" }

	thread, err := threads.CreateThread(ctx, "not ready")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if _, err := threads.CreateMessage(ctx, thread.ID, "hello"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	_, err = service.CreateRun(ctx, thread.ID, "", "")
	if !errors.Is(err, domain.ErrExecutionNotReady) {
		t.Fatalf("CreateRun error = %v, want ErrExecutionNotReady", err)
	}
	if _, loadErr := store.LoadRun(ctx, "run_not_ready"); !errors.Is(loadErr, storecore.ErrRunNotFound) {
		t.Fatalf("LoadRun after execution-not-ready = %v, want ErrRunNotFound", loadErr)
	}
}

func TestRunServiceCreateRunReportsPostStartPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = db.Close()
		}
	})

	exec := &postStartFailingExecutor{
		store:   db,
		release: make(chan struct{}),
	}
	threads := NewThreadService(db, "/repo")
	threads.newThreadID = func() string { return "thread_post_start_failure" }
	service := NewRunService(db, threads, func(context.Context) (executorHandle, error) {
		return exec, nil
	}, nil)
	service.newRunID = func() string { return "run_post_start_failure" }
	reported := make(chan error, 1)
	service.reportError = func(_ context.Context, runID string, err error) {
		if runID != "run_post_start_failure" {
			t.Errorf("reported run id = %q", runID)
		}
		reported <- err
	}

	thread, err := threads.CreateThread(ctx, "post-start failure")
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if _, err := threads.CreateMessage(ctx, thread.ID, "hello"); err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	run, err := service.CreateRun(ctx, thread.ID, "", "")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.ID != "run_post_start_failure" || run.Status != "running" {
		t.Fatalf("created run = %#v", run)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	closed = true
	close(exec.release)

	select {
	case err := <-reported:
		if err == nil || !strings.Contains(err.Error(), "record started client run failure") {
			t.Fatalf("reported error = %v, want persistence failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for background failure report")
	}
}

type postStartFailingExecutor struct {
	store   *store.Store
	release chan struct{}
}

func (e *postStartFailingExecutor) ExecuteMessages(ctx context.Context, req domain.ExecuteRequest, observer runStartObserver) error {
	if err := e.store.CreateRun(ctx, domain.RunCreateParams{
		RunID:     req.RunID,
		SessionID: req.SessionID,
		TurnIndex: req.TurnIndex,
		Input:     req.Input,
	}); err != nil {
		return err
	}
	if _, err := e.store.AppendEvent(ctx, req.RunID, "run.started", map[string]any{"input": req.Input}); err != nil {
		return err
	}
	if observer != nil {
		observer.RunStarted()
	}
	<-e.release
	return errors.New("executor failed after start")
}

func (e *postStartFailingExecutor) ResumeWithTargets(context.Context, string, map[string]any) (*executorRunResult, error) {
	return nil, errors.New("unexpected ResumeWithTargets call")
}

type openaiTestRequest struct {
	Stream bool   `json:"stream"`
	Model  string `json:"model"`
}

type openaiTestResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openaiTestChoice `json:"choices"`
	Usage   openaiTestUsage    `json:"usage"`
}

type openaiTestChoice struct {
	Index        int               `json:"index"`
	Message      openaiTestMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type openaiTestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiTestStreamResponse struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []openaiTestStreamChoice `json:"choices"`
}

type openaiTestStreamChoice struct {
	Index        int                    `json:"index"`
	Delta        openaiTestMessageDelta `json:"delta"`
	FinishReason string                 `json:"finish_reason"`
}

type openaiTestMessageDelta struct {
	Content string `json:"content"`
}

type openaiTestUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func newClientOpenAITestServer(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var req openaiTestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk := openaiTestStreamResponse{
				ID:      "chatcmpl_client_v1",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []openaiTestStreamChoice{{
					Index: 0,
					Delta: openaiTestMessageDelta{
						Content: answer,
					},
					FinishReason: "stop",
				}},
			}
			body, err := json.Marshal(chunk)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", body); err != nil {
				t.Errorf("write stream chunk: %v", err)
				return
			}
			if _, err := fmt.Fprint(w, "event: done\ndata: [DONE]\n\n"); err != nil {
				t.Errorf("write stream done: %v", err)
				return
			}
			return
		}
		response := openaiTestResponse{
			ID:      "chatcmpl_client_v1",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []openaiTestChoice{{
				Index: 0,
				Message: openaiTestMessage{
					Role:    "assistant",
					Content: answer,
				},
				FinishReason: "stop",
			}},
			Usage: openaiTestUsage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode chat completion response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func clientRuntimeTestConfig(t *testing.T, baseURL string) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Providers[0].Model = "gpt-test"
	cfg.Providers[0].BaseURL = baseURL
	cfg.Providers[0].APIKey = "test-key"
	cfg.Providers[0].MaxCompletionTokens = 32
	cfg.Providers[0].TimeoutSeconds = 5
	// Semantic is OFF by default now (no embedding model/base_url defaults); this
	// test wires a semantic runtime, so set them explicitly.
	cfg.Memory.Semantic.Embedding.Model = "text-embedding-test"
	cfg.Memory.Semantic.Embedding.BaseURL = baseURL
	cfg.Memory.Semantic.Embedding.APIKey = "test-key"
	cfg.Runtime.StorageDir = filepath.Join(root, "state")
	cfg.Runtime.RunTimeoutSeconds = 5
	cfg.Tools.Workspace.RootDir = root
	cfg.Tools.Mutation.RootDir = root
	cfg.Tools.Mutation.Disabled = true
	cfg.Tools.RunCommand.WorkDir = root
	cfg.Tools.RunCommand.Disabled = true
	cfg.Agent.MaxIterations = 2
	return cfg
}

func newClientRuntimeMemoryModule(t *testing.T, cfg *config.Config) memory.Service {
	t.Helper()
	service, err := memory.NewLocalService(memory.Config{Root: filepath.Join(cfg.Runtime.StorageDir, "memory")})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	if err := service.EnsureLayout(t.Context()); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	if err := service.BuildIndex(t.Context()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if err := service.SetSemanticRuntime(memory.SemanticRuntimeOptions{
		VectorStore: &clientRuntimeSemanticIndex{},
		Embedder:    clientRuntimeEmbedder{dimensions: cfg.Memory.Semantic.Embedding.Dimensions, model: cfg.Memory.Semantic.Embedding.Model},
		Model:       cfg.Memory.Semantic.Embedding.Model,
		Dimensions:  cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:   cfg.Memory.Semantic.Embedding.BatchSize,
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	return service
}

type clientRuntimeSemanticIndex struct{}

func (i *clientRuntimeSemanticIndex) Store(_ context.Context, _ string, _ memory.Kind, _ string, _ []float32, _ string, _ int) error {
	return nil
}

func (i *clientRuntimeSemanticIndex) Search(_ context.Context, _ []float32, limit int) ([]memory.VectorSearchResult, error) {
	return make([]memory.VectorSearchResult, 0, limit), nil
}

func (i *clientRuntimeSemanticIndex) Delete(_ context.Context, _ string) error {
	return nil
}

type clientRuntimeEmbedder struct {
	dimensions int
	model      string
}

func (e clientRuntimeEmbedder) Embed(_ context.Context, req memory.EmbedRequest) (*memory.EmbedResult, error) {
	vectors := make([]memory.EmbeddingVector, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		vectors = append(vectors, memory.EmbeddingVector{
			Ref:    input.Ref,
			Values: make([]float32, e.dimensions),
		})
	}
	return &memory.EmbedResult{Model: e.model, Dimensions: e.dimensions, Vectors: vectors}, nil
}

func waitForRunStatus(t *testing.T, store *store.Store, runID string, want domain.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.LoadRun(context.Background(), runID)
		if err == nil && run.Status == want {
			return
		}
		if err != nil && !errors.Is(err, storecore.ErrRunNotFound) {
			t.Fatalf("LoadRun: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := store.LoadRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("LoadRun after wait: %v", err)
	}
	t.Fatalf("run status = %q, want %q, error=%q, output=%q", run.Status, want, run.Error, run.Output)
}
