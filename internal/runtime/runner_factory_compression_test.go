package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"

	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/providers"
)

func TestRunnerFactoryBuildsCompressionStackPerRun(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)

	chatModel, err := providers.NewOpenAIChatModel(context.Background(), cfg.Providers[0])
	if err != nil {
		t.Fatalf("NewOpenAIChatModel: %v", err)
	}

	staticHandler := &adk.BaseChatModelAgentMiddleware{}
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		Handlers: []adk.ChatModelAgentMiddleware{staticHandler},
	})

	first, err := buildRunnerAgentHandlers(context.Background(), factory.deps.Config, factory.deps.ContextPlane, factory.deps.Handlers, factory.deps.Store, chatModel, contextplane.NewCompressionState())
	if err != nil {
		t.Fatalf("buildRunnerAgentHandlers(first): %v", err)
	}
	second, err := buildRunnerAgentHandlers(context.Background(), factory.deps.Config, factory.deps.ContextPlane, factory.deps.Handlers, factory.deps.Store, chatModel, contextplane.NewCompressionState())
	if err != nil {
		t.Fatalf("buildRunnerAgentHandlers(second): %v", err)
	}
	if got, want := len(first), 4; got != want {
		t.Fatalf("handler count = %d, want %d", got, want)
	}
	for i, want := range []string{"patchtoolcalls", "toolLifecycleMiddleware", "pipelineCompressionMiddleware", "BaseChatModelAgentMiddleware"} {
		if got := reflect.TypeOf(first[i]).String(); !strings.Contains(got, want) {
			t.Fatalf("first handler[%d] type = %q, want substring %q", i, got, want)
		}
	}
	for i := 0; i < 3; i++ {
		if reflect.ValueOf(first[i]).Pointer() == reflect.ValueOf(second[i]).Pointer() {
			t.Fatalf("compression handler %d should be rebuilt per run", i)
		}
	}
	if reflect.ValueOf(first[3]).Pointer() != reflect.ValueOf(second[3]).Pointer() {
		t.Fatalf("static custom handler should be reused across runs")
	}
}

func TestCompressionAlwaysOnBuildsMiddlewareStack(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	// Compression is always on in V2; Enabled field removed

	chatModel, err := providers.NewOpenAIChatModel(context.Background(), cfg.Providers[0])
	if err != nil {
		t.Fatalf("NewOpenAIChatModel: %v", err)
	}

	staticHandler := &adk.BaseChatModelAgentMiddleware{}
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		Handlers: []adk.ChatModelAgentMiddleware{staticHandler},
	})
	handlers, err := buildRunnerAgentHandlers(context.Background(), factory.deps.Config, factory.deps.ContextPlane, factory.deps.Handlers, factory.deps.Store, chatModel, contextplane.NewCompressionState())
	if err != nil {
		t.Fatalf("buildRunnerAgentHandlers: %v", err)
	}
	if got, want := len(handlers), 4; got != want {
		t.Fatalf("handler count = %d, want %d (compression always on)", got, want)
	}
	for i, want := range []string{"patchtoolcalls", "toolLifecycleMiddleware", "pipelineCompressionMiddleware", "BaseChatModelAgentMiddleware"} {
		if got := reflect.TypeOf(handlers[i]).String(); !strings.Contains(got, want) {
			t.Fatalf("handler[%d] type = %q, want substring %q", i, got, want)
		}
	}
}
