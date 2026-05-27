package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime/plan"
)

func TestRunnerFactoryNewCleansRunContextOnSetupFailure(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{})

	factory.installRunChatModelBuilderForTest(func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
		return nil, errors.New("model setup failed")
	})

	_, err := factory.New(context.Background(), RunnerBuildRequest{
		RunID:             "run_cleanup",
		SessionID:         "session_cleanup",
		OrchestrationMode: events.ModeDirectResponse,
	})
	if err == nil {
		t.Fatal("expected setup failure")
	}
	if _, ok := factory.registry.Get("run_cleanup"); ok {
		t.Fatal("run context should be cleared after setup failure")
	}
	if got := factory.currentRunIDValue(); got != "" {
		t.Fatalf("currentRunID = %q, want empty", got)
	}
}

func TestWrapModelWithHandlersReturnsWrapError(t *testing.T) {
	wantErr := errors.New("wrap failed")
	_, err := plan.WrapModelWithHandlers(context.Background(), nil, []adk.ChatModelAgentMiddleware{
		&failingModelWrapper{err: wantErr},
	})
	if err == nil {
		t.Fatal("expected wrap error")
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("error = %v, want containing %q", err, wantErr.Error())
	}
}

type failingModelWrapper struct {
	*adk.BaseChatModelAgentMiddleware
	err error
}

func (h *failingModelWrapper) WrapModel(context.Context, einomodel.BaseChatModel, *adk.ModelContext) (einomodel.BaseChatModel, error) {
	return nil, h.err
}
