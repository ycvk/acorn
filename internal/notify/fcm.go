// Package notify delivers push notifications to the owner's devices through
// Firebase Cloud Messaging and enforces the hourly cap and quiet hours.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2/jwt"
)

// ErrTokenUnregistered means FCM no longer knows the device token.
var ErrTokenUnregistered = errors.New("push token is no longer registered")

const fcmScope = "https://www.googleapis.com/auth/firebase.messaging"

// Message is one push notification. ThreadID and RunID let the app open the
// conversation when the notification is tapped.
type Message struct {
	Title    string
	Body     string
	ThreadID string
	RunID    string
}

// ServiceAccount is the part of a Firebase service account key FCM needs.
type ServiceAccount struct {
	ProjectID   string
	ClientEmail string
	PrivateKey  string
	TokenURI    string
}

// FCMClient sends messages with the FCM HTTP v1 API.
type FCMClient struct {
	http     *http.Client
	endpoint string
	backoff  func(attempt int) time.Duration
}

// NewFCMClient authenticates with the service account through the OAuth2 JWT
// flow. The returned client caches and refreshes access tokens itself.
func NewFCMClient(account ServiceAccount) (*FCMClient, error) {
	return newAuthedFCMClient(account, fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", account.ProjectID))
}

func newAuthedFCMClient(account ServiceAccount, endpoint string) (*FCMClient, error) {
	if account.ProjectID == "" || account.ClientEmail == "" || account.PrivateKey == "" || account.TokenURI == "" {
		return nil, errors.New("fcm: service account needs project_id, client_email, private_key and token_uri")
	}
	conf := &jwt.Config{
		Email:      account.ClientEmail,
		PrivateKey: []byte(account.PrivateKey),
		TokenURL:   account.TokenURI,
		Scopes:     []string{fcmScope},
	}
	return newFCMClient(conf.Client(context.Background()), endpoint), nil
}

func newFCMClient(client *http.Client, endpoint string) *FCMClient {
	return &FCMClient{
		http:     client,
		endpoint: endpoint,
		backoff:  func(attempt int) time.Duration { return time.Duration(1<<attempt) * 250 * time.Millisecond },
	}
}

const fcmMaxAttempts = 3

// Send delivers msg to one device token. 429 and 5xx responses are retried
// with backoff; the last failure is returned. A token FCM no longer knows
// yields ErrTokenUnregistered.
func (c *FCMClient) Send(ctx context.Context, token string, msg Message) error {
	body, err := json.Marshal(map[string]any{"message": map[string]any{
		"token":        token,
		"notification": map[string]string{"title": msg.Title, "body": msg.Body},
		"data":         map[string]string{"thread_id": msg.ThreadID, "run_id": msg.RunID},
		"android":      map[string]string{"priority": "high"},
	}})
	if err != nil {
		return fmt.Errorf("fcm: encode message: %w", err)
	}
	var lastErr error
	for attempt := range fcmMaxAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(lastErr, ctx.Err())
			case <-time.After(c.backoff(attempt)):
			}
		}
		retry, err := c.post(ctx, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry {
			return err
		}
	}
	return fmt.Errorf("fcm: giving up after %d attempts: %w", fcmMaxAttempts, lastErr)
}

// post sends one request and reports whether a failure is worth retrying.
func (c *FCMClient) post(ctx context.Context, body []byte) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ctx.Err() == nil, fmt.Errorf("fcm: send: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return true, fmt.Errorf("fcm: read response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return false, nil
	case resp.StatusCode == http.StatusNotFound || strings.Contains(string(respBody), "UNREGISTERED"):
		return false, ErrTokenUnregistered
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return true, fmt.Errorf("fcm: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	default:
		return false, fmt.Errorf("fcm: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
}
