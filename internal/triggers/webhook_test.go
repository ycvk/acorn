package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebhookTriggerHandleFiresHandler(t *testing.T) {
	wt, err := NewWebhookTrigger(WebhookConfig{
		ID:     "deploy",
		Prompt: "Webhook received: {{payload}}",
	})
	if err != nil {
		t.Fatalf("NewWebhookTrigger: %v", err)
	}

	var fired atomic.Int32
	var gotInput string
	wt.Start(context.Background(), func(_ context.Context, _, input string) {
		fired.Add(1)
		gotInput = input
	})

	body := `{"event":"deploy_failed","repo":"acorn"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/triggers/deploy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if err := wt.HandleWebhook(context.Background(), req); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	deadline := make(chan struct{})
	go func() { time.Sleep(100 * time.Millisecond); close(deadline) }()
	for fired.Load() == 0 {
		select {
		case <-deadline:
			t.Fatalf("handler not fired, calls=%d", fired.Load())
		default:
		}
	}

	if !strings.Contains(gotInput, "Webhook received:") {
		t.Fatalf("input = %q, want prompt template applied", gotInput)
	}
	if !strings.Contains(gotInput, "deploy_failed") {
		t.Fatalf("input = %q, want body content", gotInput)
	}
}

func TestWebhookTriggerRejectsBadSignature(t *testing.T) {
	wt, err := NewWebhookTrigger(WebhookConfig{
		ID:     "secret_hook",
		Secret: "supersecret",
	})
	if err != nil {
		t.Fatalf("NewWebhookTrigger: %v", err)
	}

	wt.Start(context.Background(), func(context.Context, string, string) {})

	body := `{"event":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/triggers/secret_hook", strings.NewReader(body))
	req.Header.Set("X-Acorn-Signature", "invalid")

	err = wt.HandleWebhook(context.Background(), req)
	if err == nil {
		t.Fatal("expected signature verification error, got nil")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want signature error", err)
	}
}

func TestWebhookTriggerAcceptsValidSignature(t *testing.T) {
	wt, err := NewWebhookTrigger(WebhookConfig{
		ID:     "secret_hook",
		Secret: "supersecret",
	})
	if err != nil {
		t.Fatalf("NewWebhookTrigger: %v", err)
	}

	var fired atomic.Int32
	wt.Start(context.Background(), func(context.Context, string, string) { fired.Add(1) })

	body := `{"event":"test"}`
	mac := hmac.New(sha256.New, []byte("supersecret"))
	mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/v1/triggers/secret_hook", strings.NewReader(body))
	req.Header.Set("X-Acorn-Signature", sig)

	if err := wt.HandleWebhook(context.Background(), req); err != nil {
		t.Fatalf("HandleWebhook: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if fired.Load() != 1 {
		t.Fatalf("fired = %d, want 1", fired.Load())
	}
}

func TestWebhookTriggerRawBodyWhenNoPrompt(t *testing.T) {
	wt, err := NewWebhookTrigger(WebhookConfig{ID: "raw"})
	if err != nil {
		t.Fatalf("NewWebhookTrigger: %v", err)
	}

	var got string
	wt.Start(context.Background(), func(_ context.Context, _, input string) { got = input })

	body := "plain text body"
	req := httptest.NewRequest(http.MethodPost, "/v1/triggers/raw", strings.NewReader(body))

	wt.HandleWebhook(context.Background(), req)
	time.Sleep(50 * time.Millisecond)

	if got != "plain text body" {
		t.Fatalf("input = %q, want raw body", got)
	}
}

func TestNewWebhookTriggerRejectsEmptyID(t *testing.T) {
	_, err := NewWebhookTrigger(WebhookConfig{})
	if err == nil {
		t.Fatal("expected error for empty ID, got nil")
	}
}
