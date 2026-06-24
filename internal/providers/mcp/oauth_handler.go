package mcpprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/port"
	"github.com/ycvk/acorn/internal/store"
	"golang.org/x/oauth2"
)


// persistentOAuthHandler implements auth.OAuthHandler by persisting tokens
// through port.MCPTokenStore and delegating authorization to go-sdk's
// AuthorizationCodeHandler.
//
// OAUTH-02 compliance: AuthorizationCodeHandler.Authorize() internally performs
// Protected Resource Metadata discovery (RFC 9728) via oauthex.GetProtectedResourceMetadata
// and Authorization Server Metadata discovery (RFC 8414) via oauthex.GetAuthServerMeta.
// These happen automatically when Authorize is called with a 401/403 response.
//
// OAUTH-05 compliance: AuthorizationCodeHandler.Authorize() detects 403 responses
// with insufficient_scope error and performs step-up re-authorization with expanded
// scope (authorization_code.go line 185-192).
type persistentOAuthHandler struct {
	store               port.MCPTokenStore
	providerName        string
	serverURL           string
	clientID            string
	scopes              []string
	inner               auth.OAuthHandler
	onAuthStatusChanged func(status string)
}

// newPersistentOAuthHandler creates an OAuth handler backed by the given token store.
// The handler delegates PKCE S256 authorization to go-sdk's
// AuthorizationCodeHandler, which performs metadata discovery and step-up
// authorization internally.
func newPersistentOAuthHandler(store port.MCPTokenStore, providerName, serverURL, clientID string, scopes []string, onAuthStatusChanged func(status string)) (*persistentOAuthHandler, error) {
	// Create go-sdk AuthorizationCodeHandler with PKCE S256 support.
	// The AuthorizationCodeFetcher prints the authorization URL to stderr
	// and reads the code from stdin for operator consent.
	fetcher := func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		fmt.Fprintf(os.Stderr, "\nMCP OAuth: Visit this URL to authorize provider %q:\n%s\n\n", providerName, args.URL)
		fmt.Fprintf(os.Stderr, "Enter the authorization code: ")
		var code string
		if _, err := fmt.Scanln(&code); err != nil {
			return nil, fmt.Errorf("read authorization code from stdin: %w", err)
		}
		return &auth.AuthorizationResult{Code: code}, nil
	}

	cfg := &auth.AuthorizationCodeHandlerConfig{
		PreregisteredClient: &oauthex.ClientCredentials{
			ClientID: clientID,
		},
		RedirectURL:              "http://localhost:8080/callback",
		AuthorizationCodeFetcher: auth.AuthorizationCodeFetcher(fetcher),
	}

	inner, err := auth.NewAuthorizationCodeHandler(cfg)
	if err != nil {
		return nil, fmt.Errorf("create OAuth handler for provider %q: %w", providerName, err)
	}

	return &persistentOAuthHandler{
		store:               store,
		providerName:        providerName,
		serverURL:           serverURL,
		clientID:            clientID,
		scopes:              scopes,
		inner:               inner,
		onAuthStatusChanged: onAuthStatusChanged,
	}, nil
}

// TokenSource implements auth.OAuthHandler. It reads the stored token from
// the store and returns a TokenSource that auto-refreshes using the refresh
// token. Returns (nil, nil) when no token exists yet — the transport will
// then call Authorize on the first 401/403.
func (h *persistentOAuthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	token, err := h.store.GetOAuthToken(ctx, h.providerName)
	if err != nil {
		if errors.Is(err, store.ErrOAuthTokenNotFound) {
			// No token yet — transport will trigger Authorize on 401
			return nil, nil
		}
		return nil, fmt.Errorf("read OAuth token for provider %q: %w", h.providerName, err)
	}

	oauth2Token := &oauth2.Token{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}

	// Return a TokenSource that auto-refreshes and persists the refreshed token.
	src := oauth2.ReuseTokenSource(oauth2Token, &refreshTokenSource{
		store:               h.store,
		providerName:        h.providerName,
		inner:               h.inner,
		onAuthStatusChanged: h.onAuthStatusChanged,
	})
	return src, nil
}

// Authorize implements auth.OAuthHandler. It delegates to go-sdk's
// AuthorizationCodeHandler which performs the full PKCE S256 flow, including:
//   - Protected Resource Metadata discovery (RFC 9728 / OAUTH-02)
//   - Authorization Server Metadata discovery (RFC 8414 / OAUTH-02)
//   - Step-up authorization on 403+insufficient_scope (OAUTH-05)
//
// On success, the resulting token is persisted to the store.
func (h *persistentOAuthHandler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	err := h.inner.Authorize(ctx, req, resp)
	if err != nil {
		return err
	}

	// After successful authorization, read the new token and persist it.
	ts, err := h.inner.TokenSource(ctx)
	if err != nil {
		return fmt.Errorf("get token source after authorization for provider %q: %w", h.providerName, err)
	}
	tok, err := ts.Token()
	if err != nil {
		return fmt.Errorf("get token after authorization for provider %q: %w", h.providerName, err)
	}

	saveErr := h.store.SaveOAuthToken(ctx, &domain.OAuthToken{
		ProviderName: h.providerName,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
		UpdatedAt:    time.Now().UTC(),
	})
	if saveErr != nil {
		return fmt.Errorf("persist OAuth token for provider %q: %w", h.providerName, saveErr)
	}

	// Notify auth status change after successful authorization.
	if h.onAuthStatusChanged != nil {
		h.onAuthStatusChanged("authenticated")
	}

	return nil
}

// refreshTokenSource wraps token refresh with store persistence.
// When oauth2.Client retrieves a refreshed token, we persist it.
// When the refresh token is expired or revoked, onAuthStatusChanged
// is called with "expired" to transition the provider auth status.
type refreshTokenSource struct {
	store               port.MCPTokenStore
	providerName        string
	inner               auth.OAuthHandler
	onAuthStatusChanged func(status string)
}

func (r *refreshTokenSource) Token() (*oauth2.Token, error) {
	ts, err := r.inner.TokenSource(context.Background())
	if err != nil {
		return nil, err
	}
	tok, err := ts.Token()
	if err != nil {
		// Detect expired/revoked refresh token: 401 response or invalid_grant
		// indicates the refresh token is no longer valid (RFC 6749 Section 5.2).
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.Response != nil && retrieveErr.Response.StatusCode == 401 {
			if r.onAuthStatusChanged != nil {
				r.onAuthStatusChanged("expired")
			}
		} else if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
			if r.onAuthStatusChanged != nil {
				r.onAuthStatusChanged("expired")
			}
		}
		return nil, err
	}
	if err := r.store.SaveOAuthToken(context.Background(), &domain.OAuthToken{
		ProviderName: r.providerName,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		return nil, fmt.Errorf("persist refreshed OAuth token for provider %q: %w", r.providerName, err)
	}
	return tok, nil
}
