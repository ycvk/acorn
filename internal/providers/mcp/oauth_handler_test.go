package mcpprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
	"golang.org/x/oauth2"
)

// mockTokenStore implements a fake token store for OAuth token persistence
// without requiring a real database. It satisfies port.MCPTokenStore.
type mockTokenStore struct {
	tokens  map[string]*domain.OAuthToken
	getErr  error
	saveErr error
}

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{tokens: make(map[string]*domain.OAuthToken)}
}

func (m *mockTokenStore) GetOAuthToken(_ context.Context, providerName string) (*domain.OAuthToken, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	tok, ok := m.tokens[providerName]
	if !ok {
		return nil, store.ErrOAuthTokenNotFound
	}
	return tok, nil
}

func (m *mockTokenStore) SaveOAuthToken(_ context.Context, token *domain.OAuthToken) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.tokens[token.ProviderName] = token
	return nil
}

func (m *mockTokenStore) DeleteOAuthToken(_ context.Context, providerName string) error {
	delete(m.tokens, providerName)
	return nil
}

// --- TokenSource tests ---

func TestSQLiteOAuthHandler_TokenSource_ReturnsTokenWhenExists(t *testing.T) {
	store := newMockTokenStore()
	providerName := "test-provider"
	futureExpiry := time.Now().Add(1 * time.Hour)

	store.tokens[providerName] = &domain.OAuthToken{
		ProviderName: providerName,
		AccessToken:  "valid-access-token",
		RefreshToken: "valid-refresh-token",
		Expiry:       futureExpiry,
		UpdatedAt:    time.Now().UTC(),
	}

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: providerName,
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
	}

	ts, err := handler.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource returned error: %v", err)
	}
	if ts == nil {
		t.Fatal("TokenSource returned nil TokenSource when token exists")
	}

	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() returned error: %v", err)
	}
	if tok.AccessToken != "valid-access-token" {
		t.Errorf("AccessToken = %q, want %q", tok.AccessToken, "valid-access-token")
	}
	if tok.RefreshToken != "valid-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", tok.RefreshToken, "valid-refresh-token")
	}
}

func TestSQLiteOAuthHandler_TokenSource_ReturnsNilWhenNoToken(t *testing.T) {
	store := newMockTokenStore()
	handler := &persistentOAuthHandler{
		store:        store,
		providerName: "test-provider",
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
	}

	ts, err := handler.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource returned error: %v", err)
	}
	if ts != nil {
		t.Fatal("TokenSource should return nil when no token exists yet")
	}
}

func TestSQLiteOAuthHandler_TokenSource_ReturnsErrorOnStoreFailure(t *testing.T) {
	store := newMockTokenStore()
	store.getErr = errors.New("database is locked")

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: "test-provider",
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
	}

	_, err := handler.TokenSource(context.Background())
	if err == nil {
		t.Fatal("TokenSource should return error when store fails")
	}
	if !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("error = %q, want containing 'database is locked'", err.Error())
	}
}

// --- Authorize tests ---

func TestSQLiteOAuthHandler_Authorize_PersistsTokenAfterSuccess(t *testing.T) {
	store := newMockTokenStore()
	providerName := "oauth-provider"

	mockInner := &mockAuthHandler{
		authorizeFunc: func(ctx context.Context, req *http.Request, resp *http.Response) error {
			return nil // Success
		},
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			Expiry:       time.Now().Add(time.Hour),
		}),
	}

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: providerName,
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
		inner:        mockInner,
	}

	req := httptest.NewRequest("POST", "https://example.com/mcp", nil)
	resp := &http.Response{StatusCode: 401, Header: http.Header{}, Body: http.NoBody}
	resp.Header.Set("WWW-Authenticate", `Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`)

	err := handler.Authorize(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}

	saved, ok := store.tokens[providerName]
	if !ok {
		t.Fatal("token was not persisted to store after successful Authorize")
	}
	if saved.AccessToken != "new-access-token" {
		t.Errorf("persisted AccessToken = %q, want %q", saved.AccessToken, "new-access-token")
	}
	if saved.RefreshToken != "new-refresh-token" {
		t.Errorf("persisted RefreshToken = %q, want %q", saved.RefreshToken, "new-refresh-token")
	}
}

func TestSQLiteOAuthHandler_Authorize_DelegatesToInner(t *testing.T) {
	store := newMockTokenStore()
	authorizeCalled := false

	mockInner := &mockAuthHandler{
		authorizeFunc: func(ctx context.Context, req *http.Request, resp *http.Response) error {
			authorizeCalled = true
			return nil
		},
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  "delegated-token",
			RefreshToken: "delegated-refresh",
			Expiry:       time.Now().Add(time.Hour),
		}),
	}

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: "delegated-provider",
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
		inner:        mockInner,
	}

	req := httptest.NewRequest("POST", "https://example.com/mcp", nil)
	resp := &http.Response{StatusCode: 401, Header: http.Header{}, Body: http.NoBody}

	err := handler.Authorize(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if !authorizeCalled {
		t.Fatal("Authorize did not delegate to inner AuthorizationCodeHandler.Authorize")
	}
}

func TestSQLiteOAuthHandler_Authorize_PropagatesInnerError(t *testing.T) {
	store := newMockTokenStore()

	mockInner := &mockAuthHandler{
		authorizeFunc: func(ctx context.Context, req *http.Request, resp *http.Response) error {
			return errors.New("authorization failed: user denied consent")
		},
	}

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: "denied-provider",
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
		inner:        mockInner,
	}

	req := httptest.NewRequest("POST", "https://example.com/mcp", nil)
	resp := &http.Response{StatusCode: 401, Header: http.Header{}, Body: http.NoBody}

	err := handler.Authorize(context.Background(), req, resp)
	if err == nil {
		t.Fatal("Authorize should propagate inner error")
	}
	if !strings.Contains(err.Error(), "authorization failed") {
		t.Errorf("error = %q, want containing 'authorization failed'", err.Error())
	}
}

// --- Step-up authorization test (OAUTH-05) ---

func TestSQLiteOAuthHandler_Authorize_StepUpOnInsufficientScope(t *testing.T) {
	store := newMockTokenStore()
	providerName := "stepup-provider"
	authorizeCalled := false

	mockInner := &mockAuthHandler{
		authorizeFunc: func(ctx context.Context, req *http.Request, resp *http.Response) error {
			authorizeCalled = true
			return nil
		},
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  "expanded-scope-token",
			RefreshToken: "expanded-scope-refresh",
			Expiry:       time.Now().Add(time.Hour),
		}),
	}

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: providerName,
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
		inner:        mockInner,
	}

	req := httptest.NewRequest("POST", "https://example.com/mcp", nil)
	resp := &http.Response{
		StatusCode: 403,
		Header: http.Header{
			"WWW-Authenticate": []string{`Bearer error="insufficient_scope", scope="read write admin"`},
		},
		Body: http.NoBody,
	}

	err := handler.Authorize(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("Authorize with 403+insufficient_scope returned error: %v", err)
	}
	if !authorizeCalled {
		t.Fatal("Authorize did not call inner handler for step-up re-authorization")
	}

	saved, ok := store.tokens[providerName]
	if !ok {
		t.Fatal("token was not persisted after step-up authorization")
	}
	if saved.AccessToken != "expanded-scope-token" {
		t.Errorf("persisted AccessToken = %q, want %q", saved.AccessToken, "expanded-scope-token")
	}
}

// --- Metadata discovery delegation test (OAUTH-02) ---

func TestSQLiteOAuthHandler_Authorize_DelegatesMetadataDiscovery(t *testing.T) {
	store := newMockTokenStore()
	var capturedResp *http.Response

	mockInner := &mockAuthHandler{
		authorizeFunc: func(ctx context.Context, req *http.Request, resp *http.Response) error {
			capturedResp = resp
			return nil
		},
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  "discovered-token",
			RefreshToken: "discovered-refresh",
			Expiry:       time.Now().Add(time.Hour),
		}),
	}

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: "discovery-provider",
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
		inner:        mockInner,
	}

	req := httptest.NewRequest("POST", "https://example.com/mcp", nil)
	resp := &http.Response{
		StatusCode: 401,
		Header: http.Header{
			"WWW-Authenticate": []string{`Bearer resource_metadata="https://example.com/.well-known/oauth-protected-resource"`},
		},
		Body: http.NoBody,
	}

	err := handler.Authorize(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}

	if capturedResp == nil {
		t.Fatal("inner Authorize was not called -- metadata discovery cannot be triggered")
	}
	if capturedResp.StatusCode != 401 {
		t.Errorf("inner received status %d, want 401", capturedResp.StatusCode)
	}
}

// --- NewTransport with OAuth tests ---

func TestNewTransport_WithOAuth_SetsHandlerOnStreamableHTTP(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "oauth-http",
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   "http://localhost:8080/mcp",
		StartupTimeoutSeconds: 10,
		Auth: AuthConfig{
			Type:     "oauth",
			ClientID: "test-client-id",
			Scopes:   []string{"read", "write"},
		},
	}

	store := newMockTokenStore()
	transport, cleanup, metadata, err := NewTransportWithStore(cfg, store)
	if err != nil {
		t.Fatalf("NewTransportWithStore with OAuth: %v", err)
	}
	defer cleanup()

	sHttp, ok := transport.(*mcp.StreamableClientTransport)
	if !ok {
		t.Fatalf("expected *mcp.StreamableClientTransport, got %T", transport)
	}
	if sHttp.OAuthHandler == nil {
		t.Fatal("StreamableClientTransport.OAuthHandler is nil, expected OAuth handler to be set")
	}
	if got, want := metadata.Kind, "streamable_http"; got != want {
		t.Fatalf("metadata.Kind = %q, want %q", got, want)
	}
}

func TestNewTransport_WithOAuth_RejectsSSE(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "oauth-sse",
		Enabled:               true,
		Transport:             "sse",
		URL:                   "http://localhost:8080/sse",
		StartupTimeoutSeconds: 10,
		Auth: AuthConfig{
			Type:     "oauth",
			ClientID: "test-client-id",
		},
	}

	store := newMockTokenStore()
	_, _, _, err := NewTransportWithStore(cfg, store)
	if err == nil {
		t.Fatal("expected OAuth on SSE to be rejected")
	}
	if !strings.Contains(err.Error(), "oauth") || !strings.Contains(err.Error(), "sse") {
		t.Errorf("error = %q, want containing 'oauth' and 'sse'", err.Error())
	}
}

func TestNewTransport_WithOAuth_RejectsStdio(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "oauth-stdio",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "echo",
		StartupTimeoutSeconds: 10,
		Auth: AuthConfig{
			Type:     "oauth",
			ClientID: "test-client-id",
		},
	}

	store := newMockTokenStore()
	_, _, _, err := NewTransportWithStore(cfg, store)
	if err == nil {
		t.Fatal("expected OAuth on stdio to be rejected")
	}
}

func TestNewTransport_WithOAuth_MissingClientID(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "oauth-no-clientid",
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   "http://localhost:8080/mcp",
		StartupTimeoutSeconds: 10,
		Auth: AuthConfig{
			Type:     "oauth",
			ClientID: "",
		},
	}

	store := newMockTokenStore()
	_, _, _, err := NewTransportWithStore(cfg, store)
	if err == nil {
		t.Fatal("expected OAuth without ClientID to be rejected")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Errorf("error = %q, want containing 'client_id'", err.Error())
	}
}

func TestNewTransport_WithOAuth_MissingURL(t *testing.T) {
	cfg := ProviderConfig{
		Name:                  "oauth-no-url",
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   "",
		StartupTimeoutSeconds: 10,
		Auth: AuthConfig{
			Type:     "oauth",
			ClientID: "test-client-id",
		},
	}

	store := newMockTokenStore()
	_, _, _, err := NewTransportWithStore(cfg, store)
	if err == nil {
		t.Fatal("expected OAuth without URL to be rejected")
	}
}

// --- WithTokenStore ManagerOption test ---

func TestWithTokenStore_SetsStoreInManagerOptions(t *testing.T) {
	store := newMockTokenStore()
	opt := WithTokenStore(store)

	var opts managerOptions
	opt(&opts)

	if opts.tokenStore == nil {
		t.Fatal("WithTokenStore did not set tokenStore in managerOptions")
	}
}

func TestWithTokenStore_NilStore(t *testing.T) {
	opt := WithTokenStore(nil)

	var opts managerOptions
	opt(&opts)

	if opts.tokenStore != nil {
		t.Fatal("WithTokenStore(nil) should result in nil tokenStore")
	}
}

// --- Compile-time interface check ---

func TestSQLiteOAuthHandler_ImplementsOAuthHandler(t *testing.T) {
	var _ auth.OAuthHandler = (*persistentOAuthHandler)(nil)
}

// --- TokenSource errors.Is test (replaces strings.Contains) ---

func TestSQLiteOAuthHandler_TokenSource_UsesErrorsIsForNotFound(t *testing.T) {
	mockStore := newMockTokenStore()
	// getErr is a wrapped error that contains "not found" in its message
	// but should NOT be treated as ErrOAuthTokenNotFound when using errors.Is.
	wrappedErr := fmt.Errorf("wrapped: %w", store.ErrOAuthTokenNotFound)

	// Verify errors.Is works with the wrapped error
	if !errors.Is(wrappedErr, store.ErrOAuthTokenNotFound) {
		t.Fatal("errors.Is should detect ErrOAuthTokenNotFound through wrapping")
	}

	// Verify a random error with "not found" in its message is NOT matched by errors.Is
	randomErr := errors.New("token not found in cache")
	if errors.Is(randomErr, store.ErrOAuthTokenNotFound) {
		t.Fatal("errors.Is should NOT match a random error that happens to contain 'not found'")
	}

	// Now verify the handler correctly returns (nil, nil) for the sentinel error
	mockStore.getErr = store.ErrOAuthTokenNotFound
	handler := &persistentOAuthHandler{
		store:        mockStore,
		providerName: "test-provider",
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
	}

	ts, err := handler.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource returned error for ErrOAuthTokenNotFound: %v", err)
	}
	if ts != nil {
		t.Fatal("TokenSource should return nil TokenSource when token not found")
	}
}

// --- Authorize onAuthStatusChanged callback test ---

func TestSQLiteOAuthHandler_Authorize_CallsOnAuthStatusChanged(t *testing.T) {
	store := newMockTokenStore()
	providerName := "callback-provider"
	var capturedStatus string

	mockInner := &mockAuthHandler{
		authorizeFunc: func(ctx context.Context, req *http.Request, resp *http.Response) error {
			return nil
		},
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  "cb-token",
			RefreshToken: "cb-refresh",
			Expiry:       time.Now().Add(time.Hour),
		}),
	}

	handler := &persistentOAuthHandler{
		store:        store,
		providerName: providerName,
		serverURL:    "https://example.com/mcp",
		clientID:     "test-client-id",
		inner:        mockInner,
		onAuthStatusChanged: func(status string) {
			capturedStatus = status
		},
	}

	req := httptest.NewRequest("POST", "https://example.com/mcp", nil)
	resp := &http.Response{StatusCode: 401, Header: http.Header{}, Body: http.NoBody}

	err := handler.Authorize(context.Background(), req, resp)
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}

	if got, want := capturedStatus, "authenticated"; got != want {
		t.Fatalf("onAuthStatusChanged called with %q, want %q", got, want)
	}
}

// --- refreshTokenSource expired token detection tests ---

func TestRefreshTokenSource_DetectsExpiredTokenWith401(t *testing.T) {
	store := newMockTokenStore()
	var capturedStatus string

	// Create a mock inner that returns an error with 401 status
	mockInner := &mockAuthHandler{
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{}),
	}

	// We need to make the inner.TokenSource return a TokenSource that fails with 401
	mockInner.tokenSource = &failingTokenSource{
		err: &oauth2.RetrieveError{
			Response: &http.Response{StatusCode: 401},
		},
	}

	rts := &refreshTokenSource{
		store:        store,
		providerName: "expired-provider",
		inner:        mockInner,
		onAuthStatusChanged: func(status string) {
			capturedStatus = status
		},
	}

	_, err := rts.Token()
	if err == nil {
		t.Fatal("expected error from expired token refresh")
	}

	if got, want := capturedStatus, "expired"; got != want {
		t.Fatalf("onAuthStatusChanged called with %q, want %q", got, want)
	}
}

func TestRefreshTokenSource_DetectsInvalidGrant(t *testing.T) {
	store := newMockTokenStore()
	var capturedStatus string

	mockInner := &mockAuthHandler{
		tokenSource: &failingTokenSource{
			err: &oauth2.RetrieveError{ErrorCode: "invalid_grant"},
		},
	}

	rts := &refreshTokenSource{
		store:        store,
		providerName: "invalid-grant-provider",
		inner:        mockInner,
		onAuthStatusChanged: func(status string) {
			capturedStatus = status
		},
	}

	_, err := rts.Token()
	if err == nil {
		t.Fatal("expected error from invalid_grant refresh")
	}

	if got, want := capturedStatus, "expired"; got != want {
		t.Fatalf("onAuthStatusChanged called with %q, want %q", got, want)
	}
}

func TestRefreshTokenSource_NoCallbackOnOtherErrors(t *testing.T) {
	store := newMockTokenStore()
	var capturedStatus string

	mockInner := &mockAuthHandler{
		tokenSource: &failingTokenSource{
			err: errors.New("network timeout"),
		},
	}

	rts := &refreshTokenSource{
		store:        store,
		providerName: "network-provider",
		inner:        mockInner,
		onAuthStatusChanged: func(status string) {
			capturedStatus = status
		},
	}

	_, err := rts.Token()
	if err == nil {
		t.Fatal("expected error from network timeout")
	}

	if capturedStatus != "" {
		t.Fatalf("onAuthStatusChanged should not be called on network errors, got %q", capturedStatus)
	}
}

func TestRefreshTokenSource_ReturnsErrorWhenPersistFails(t *testing.T) {
	store := newMockTokenStore()
	store.saveErr = errors.New("database locked")
	mockInner := &mockAuthHandler{
		tokenSource: oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  "new-token",
			RefreshToken: "new-refresh",
			Expiry:       time.Now().Add(time.Hour),
		}),
	}
	rts := &refreshTokenSource{
		store:        store,
		providerName: "persist-failure-provider",
		inner:        mockInner,
	}

	_, err := rts.Token()
	if err == nil {
		t.Fatal("expected refreshed token persistence failure to return error")
	}
	if !strings.Contains(err.Error(), "database locked") {
		t.Fatalf("expected persistence error, got %v", err)
	}
}

// --- failingTokenSource for testing error paths ---

type failingTokenSource struct {
	err error
}

func (f *failingTokenSource) Token() (*oauth2.Token, error) {
	return nil, f.err
}

// --- mockAuthHandler for testing ---

type mockAuthHandler struct {
	authorizeFunc func(ctx context.Context, req *http.Request, resp *http.Response) error
	tokenSource   oauth2.TokenSource
}

func (m *mockAuthHandler) TokenSource(_ context.Context) (oauth2.TokenSource, error) {
	return m.tokenSource, nil
}

func (m *mockAuthHandler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	if m.authorizeFunc != nil {
		return m.authorizeFunc(ctx, req, resp)
	}
	return fmt.Errorf("mockAuthHandler: no AuthorizeFunc configured")
}
