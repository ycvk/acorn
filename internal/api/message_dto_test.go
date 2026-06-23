package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/app"
)

func TestMessageDTOFromDomainEmitsResultArrays(t *testing.T) {
	message := messageDTOFromDomain(app.Message{
		ID:       "msg_1",
		ThreadID: "thread_1",
		Role:     "assistant",
		Content: app.MessageContent{
			Type: "text",
			Text: "Task completed.",
			Parts: []app.MessagePart{{
				Kind:  "result",
				Title: "Task completed",
			}},
		},
		CreatedAt: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		RunID:     "run_1",
	})

	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	for _, want := range []string{`"changed":[]`, `"verified":[]`, `"risks":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("message JSON should contain %s, got %s", want, string(raw))
		}
	}
}

func TestMessageDTOFromDomainEmitsReasoningPart(t *testing.T) {
	message := messageDTOFromDomain(app.Message{
		ID:       "msg_2",
		ThreadID: "thread_1",
		Role:     "assistant",
		Content: app.MessageContent{
			Type: "text",
			Text: "Task completed.",
			Parts: []app.MessagePart{{
				Kind:      "reasoning",
				Reasoning: "inspected the final run archive before answering",
			}},
		},
		CreatedAt: time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC),
		RunID:     "run_1",
	})

	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	if !strings.Contains(string(raw), `"kind":"reasoning"`) || !strings.Contains(string(raw), `"reasoning":"inspected the final run archive before answering"`) {
		t.Fatalf("message JSON should contain reasoning part, got %s", string(raw))
	}
}

func TestMessagePartDTOEmitsDisclosureItemsField(t *testing.T) {
	raw, err := json.Marshal(MessagePartDTO{
		Kind:  "disclosure",
		Items: []DisclosureItemDTO{},
	})
	if err != nil {
		t.Fatalf("marshal disclosure part: %v", err)
	}
	if !strings.Contains(string(raw), `"items":[]`) {
		t.Fatalf("disclosure part should always emit items, got %s", string(raw))
	}
}

func TestMessagePartDTOEmitsTechnicalDetailLinkRequiredFields(t *testing.T) {
	raw, err := json.Marshal(MessagePartDTO{
		Kind:        "technical_detail_link",
		DetailRunID: "detail_run_1",
	})
	if err != nil {
		t.Fatalf("marshal technical detail link: %v", err)
	}
	for _, want := range []string{`"detail_run_id":"detail_run_1"`, `"run_id":""`, `"label":""`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("technical_detail_link JSON should contain %s, got %s", want, string(raw))
		}
	}
	if strings.Contains(string(raw), "tool_name") {
		t.Fatalf("technical_detail_link JSON should not contain tool_name, got %s", string(raw))
	}
}
