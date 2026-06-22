package eventstream

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestActiveProviderName(t *testing.T) {
	// chatModel with ActiveProvider
	modelWithProvider := &mockChatModel{provider: "openai"}
	if got := activeProviderName(modelWithProvider); got != "openai" {
		t.Fatalf("activeProviderName = %q, want openai", got)
	}

	// chatModel without ActiveProvider
	modelWithout := &mockChatModel{}
	if got := activeProviderName(modelWithout); got != "" {
		t.Fatalf("activeProviderName = %q, want empty", got)
	}

	// nil chatModel
	if got := activeProviderName(nil); got != "" {
		t.Fatalf("activeProviderName(nil) = %q, want empty", got)
	}
}

func TestStreamItemsFromAgentEvent(t *testing.T) {
	cases := []struct {
		name     string
		event    *adk.AgentEvent
		wantKind StreamItemKind
		wantLen  int
	}{
		{
			name: "message_output",
			event: &adk.AgentEvent{
				Output: &adk.AgentOutput{
					MessageOutput: &adk.MessageVariant{
						Message: &schema.Message{Role: schema.Assistant, Content: "hi"},
					},
				},
			},
			wantKind: StreamKindAssistantMessage,
			wantLen:  1,
		},
		{
			name: "interrupted",
			event: &adk.AgentEvent{
				Action: &adk.AgentAction{
					Interrupted: &adk.InterruptInfo{
						InterruptContexts: []*adk.InterruptCtx{
							{ID: "ctx1", Address: adk.Address{{ID: "addr1", Type: adk.AddressSegmentAgent}}, IsRootCause: true},
						},
					},
				},
			},
			wantKind: StreamKindRunInterrupted,
			wantLen:  1,
		},
		{
			name:     "error",
			event:    &adk.AgentEvent{Err: errors.New("boom")},
			wantKind: StreamKindRunFailed,
			wantLen:  1,
		},
		{
			name: "message_and_error",
			event: &adk.AgentEvent{
				Output: &adk.AgentOutput{
					MessageOutput: &adk.MessageVariant{
						Message: &schema.Message{Role: schema.Assistant, Content: "hi"},
					},
				},
				Err: errors.New("boom"),
			},
			wantLen: 2,
		},
		{
			name:    "empty",
			event:   &adk.AgentEvent{},
			wantLen: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := StreamItemsFromAgentEvent(tc.event, nil)
			if len(items) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(items), tc.wantLen)
			}
			if tc.wantLen > 0 && tc.wantKind != "" {
				found := false
				for _, item := range items {
					if item.Kind == tc.wantKind {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("kind %q not found in items", tc.wantKind)
				}
			}
		})
	}
}

func TestStreamInterruptFromInfo(t *testing.T) {
	info := &adk.InterruptInfo{
		InterruptContexts: []*adk.InterruptCtx{
			{ID: "ctx1", Address: adk.Address{{ID: "addr1", Type: adk.AddressSegmentAgent}}, IsRootCause: true, Info: map[string]any{"kind": "action"}},
			{ID: "ctx2", Address: adk.Address{{ID: "addr2", Type: adk.AddressSegmentAgent}}, IsRootCause: false},
		},
	}

	interrupt := streamInterruptFromInfo(info)
	if interrupt == nil {
		t.Fatal("expected non-nil")
	}
	if interrupt.ContextCount != 2 {
		t.Fatalf("context_count = %d", interrupt.ContextCount)
	}
	if len(interrupt.Contexts) != 2 {
		t.Fatalf("len(contexts) = %d", len(interrupt.Contexts))
	}
	if interrupt.Contexts[0].ID != "ctx1" {
		t.Fatalf("id = %q", interrupt.Contexts[0].ID)
	}
	if !interrupt.Contexts[0].IsRootCause {
		t.Fatal("expected IsRootCause=true")
	}

	// nil info
	if streamInterruptFromInfo(nil) != nil {
		t.Fatal("nil info should return nil")
	}
}

// mockChatModel implements einomodel.BaseChatModel with optional ActiveProvider.
type mockChatModel struct {
	provider string
}

func (m *mockChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	return nil, nil
}

func (m *mockChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *mockChatModel) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

func (m *mockChatModel) ActiveProvider() string {
	return m.provider
}
