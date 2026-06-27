package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/triggers"
)

// handleWebhookTrigger receives an external webhook and routes it to the
// trigger scheduler. This endpoint is NOT behind device auth — webhook
// authentication is via HMAC signature (X-Acorn-Signature header), not
// device bearer token. See ADR-0001.
func (s *Server) handleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	if s.triggerSched == nil {
		s.respondJSON(w, r, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]any{"code": "triggers_not_configured", "message": "trigger scheduler is not available"},
		})
		return
	}

	triggerID := chi.URLParam(r, "trigger_id")
	if triggerID == "" {
		s.respondJSON(w, r, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"code": "missing_trigger_id", "message": "trigger_id is required"},
		})
		return
	}

	if err := s.triggerSched.HandleWebhook(r.Context(), triggerID, r); err != nil {
		var nfe *triggers.TriggerNotFoundError
		if errors.As(err, &nfe) {
			s.respondJSON(w, r, http.StatusNotFound, map[string]any{
				"error": map[string]any{"code": "trigger_not_found", "message": err.Error()},
			})
			return
		}
		s.respondJSON(w, r, http.StatusUnauthorized, map[string]any{
			"error": map[string]any{"code": "webhook_verification_failed", "message": err.Error()},
		})
		return
	}

	s.respondJSON(w, r, http.StatusAccepted, map[string]any{"status": "accepted"})
}
