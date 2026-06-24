package port

import (
	"context"

	"github.com/ycvk/acorn/internal/domain"
)

// MCPTokenStore is the OAuth token storage port for MCP providers.
type MCPTokenStore interface {
	GetOAuthToken(ctx context.Context, providerName string) (*domain.OAuthToken, error)
	SaveOAuthToken(ctx context.Context, token domain.OAuthToken) error
}

// MCPPendingActionStore is the pending-action port for MCP elicitation.
// It combines pending-action lifecycle (create/load/decide) with event
// appending so the elicitation handler can emit stream events through the
// same store it polls for decisions.
type MCPPendingActionStore interface {
	CreatePendingAction(ctx context.Context, input domain.PendingActionInput) (*domain.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*domain.PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status domain.PendingActionStatus, decisionJSON string) (*domain.PendingActionRecord, error)
	AppendEvent(ctx context.Context, runID, kind string, payload any) (domain.EventRecord, error)
}
