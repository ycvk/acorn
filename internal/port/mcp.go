package port

import (
	"context"
	"time"
)

// MCPTokenStore is the OAuth token storage port for MCP providers.
type MCPTokenStore interface {
	SaveOAuthToken(ctx context.Context, providerName, accessToken, refreshToken string, expiry time.Time) error
	LoadOAuthToken(ctx context.Context, providerName string) (accessToken, refreshToken string, expiry time.Time, err error)
}

// MCPPendingActionStore is the pending-action port for MCP elicitation.
type MCPPendingActionStore interface {
	CreatePendingAction(ctx context.Context, input PendingActionInput) error
}

// PendingActionInput is the MCP-side pending action creation input.
type PendingActionInput struct {
	ActionID    string
	RunID       string
	InterruptID string
	Kind        string
	Subject     string
	PayloadJSON string
}
