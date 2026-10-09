package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
)

func TestNativeModelProtocolPreservesReasoningAndTools(t *testing.T) {
	for _, tc := range []struct{ api, path, model, reply, signature string }{
		{"responses", "/responses", "gpt-6-astra", `{"id":"resp_1","object":"response","status":"completed","model":"gpt-6-astra","output":[{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"checking"}],"encrypted_content":"opaque-openai"},{"type":"function_call","call_id":"call_1","name":"recall","arguments":"{}","id":"fc_1","status":"completed"}],"usage":{"input_tokens":12,"output_tokens":8,"total_tokens":20}}`, "opaque-openai"},
		{"anthropic", "/v1/messages", "claude-opus-5-5", `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"checking","signature":"opaque-claude"},{"type":"tool_use","id":"call_1","name":"recall","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":12,"output_tokens":8}}`, "opaque-claude"},
		{"chat_completions", "/chat/completions", "custom", `{"id":"chat_1","object":"chat.completion","model":"custom","choices":[{"index":0,"message":{"role":"assistant","reasoning_content":"checking","tool_calls":[{"id":"call_1","type":"function","function":{"name":"recall","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`, "checking"},
	} {
		t.Run(tc.api, func(t *testing.T) {
			var requests []map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("path=%s, want %s", r.URL.Path, tc.path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				requests = append(requests, body)
				if tc.api == "anthropic" {
					if body["stream"] != true {
						t.Error("large Anthropic generation must stream")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + tc.reply + "}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.reply))
			}))
			defer server.Close()
			cfg := config.ProviderConfig{API: tc.api, Model: tc.model, BaseURL: server.URL, APIKey: "test", MaxOutputTokens: new(128000), ReasoningEffort: "high", ExtraFields: map[string]any{"metadata": map[string]any{"test": "protocol"}}}
			model, err := newProviderModel(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			input := []*schema.AgenticMessage{schema.UserAgenticMessage("look up the previous discussion")}
			reply, err := model.Generate(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			public := StreamMessageFromSchema(reply, "")
			publicJSON, _ := json.Marshal(public)
			if strings.Contains(string(publicJSON), "opaque-") {
				t.Fatal("private reasoning signature reached client projection")
			}
			if len(public.ToolCalls) != 1 || public.ToolCalls[0].ID != "call_1" || public.ToolCalls[0].Name != "recall" {
				t.Fatalf("tool call lost: %+v", public)
			}
			if reply.ResponseMeta == nil || reply.ResponseMeta.TokenUsage == nil || reply.ResponseMeta.TokenUsage.CompletionTokens != 8 {
				t.Fatalf("usage lost: %+v", reply.ResponseMeta)
			}
			input = append(input, reply, toolResultMessage("found", "call_1", "recall"))
			if _, err = model.Generate(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(requests[1])
			if !strings.Contains(string(encoded), tc.signature) {
				t.Fatalf("reasoning state missing: %s", encoded)
			}
			if _, ok := requests[0]["temperature"]; ok {
				t.Fatal("omitted temperature was sent")
			}
			if tc.api == "responses" && requests[0]["store"] != false {
				t.Fatal("responses must keep state in local checkpoints")
			}
			if len(cfg.ExtraFields) != 1 {
				t.Fatal("provider fields mutated")
			}
		})
	}
}
