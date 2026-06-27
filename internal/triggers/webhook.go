package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
)

// WebhookConfig configures a single webhook trigger.
type WebhookConfig struct {
	ID           string `yaml:"id"`
	Secret       string `yaml:"secret"`         // HMAC-SHA256 shared secret for signature verification
	Prompt       string `yaml:"prompt"`         // template; {{payload}} is replaced with the body
	MaxBodyBytes int64  `yaml:"max_body_bytes"` // default 1MB; rejects oversized payloads
}

// WebhookTrigger is a Trigger that fires when an authenticated HTTP POST
// arrives at its endpoint. It does not own the HTTP server — the api layer
// routes POST /v1/triggers/{id} to HandleWebhook.
type WebhookTrigger struct {
	cfg     WebhookConfig
	handler FireFunc
	stopCh  chan struct{}
}

// NewWebhookTrigger constructs a webhook trigger from config.
func NewWebhookTrigger(cfg WebhookConfig) (*WebhookTrigger, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return nil, errors.New("webhook trigger id is required")
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = 1 << 20 // 1MB default
	}
	cfg.MaxBodyBytes = maxBody
	return &WebhookTrigger{cfg: cfg, stopCh: make(chan struct{})}, nil
}

// ID returns the trigger identifier used in the URL path.
func (w *WebhookTrigger) ID() string { return w.cfg.ID }

// Start records the fire handler. The actual HTTP listening is done by the
// api layer which routes to HandleWebhook.
func (w *WebhookTrigger) Start(_ context.Context, handler FireFunc) error {
	if handler == nil {
		return errors.New("webhook trigger requires a non-nil handler")
	}
	w.handler = handler
	return nil
}

// Stop signals the trigger to stop accepting webhooks.
func (w *WebhookTrigger) Stop() {
	select {
	case <-w.stopCh:
	default:
		close(w.stopCh)
	}
}

// HandleWebhook processes an incoming HTTP request. It verifies the HMAC
// signature, reads the body, applies the prompt template, and fires the
// trigger.
func (w *WebhookTrigger) HandleWebhook(ctx context.Context, r *http.Request) error {
	if w.handler == nil {
		return errors.New("webhook trigger not started")
	}

	body, err := w.readAndVerify(r)
	if err != nil {
		return err
	}

	input := w.renderPrompt(body)
	w.handler(ctx, w.cfg.ID, input)
	return nil
}

// readAndVerify reads the body, checks size, and verifies the HMAC-SHA256
// signature if a secret is configured. MaxBytesReader is given nil for the
// ResponseWriter because triggers has no http.ResponseWriter; oversized bodies
// produce a read error from io.ReadAll instead of an HTTP-level abort.
func (w *WebhookTrigger) readAndVerify(r *http.Request) (string, error) {
	if w.cfg.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(nil, r.Body, w.cfg.MaxBodyBytes)
	}
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", errors.New("webhook body read failed")
	}
	bodyStr := string(body)

	if w.cfg.Secret != "" {
		sig := r.Header.Get("X-Acorn-Signature")
		if !w.verifySignature(body, sig) {
			return "", errors.New("webhook signature verification failed")
		}
	}

	return bodyStr, nil
}

// verifySignature checks the HMAC-SHA256 signature of the body against the
// configured secret. Constant-time comparison is done by hmac.Equal.
func (w *WebhookTrigger) verifySignature(body []byte, sig string) bool {
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(w.cfg.Secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

// renderPrompt produces the run input from the webhook body. If a prompt
// template is configured, {{payload}} is replaced; otherwise the raw body is
// used.
func (w *WebhookTrigger) renderPrompt(body string) string {
	if strings.TrimSpace(w.cfg.Prompt) == "" {
		return body
	}
	return strings.ReplaceAll(w.cfg.Prompt, "{{payload}}", body)
}
