package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
)

const (
	defaultLimit      = 100
	maxJSONBodySize   = 1 << 20
	internalErrorBody = `{"error":{"code":"internal_error","message":"internal server error"}}`
)

func (s *Server) respondJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		s.logInternalError(r, "response_marshal_failed", err, slog.Int("status", status))
		s.writeInternalError(w, r)
		return
	}
	if err := writeResponseBody(w, status, "application/json", body); err != nil {
		s.logInternalError(r, "response_write_failed", err, slog.Int("status", status))
	}
}

func (s *Server) respondError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	s.respondJSON(w, r, status, ErrorResponse{
		Error: ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

func (s *Server) respondBadRequest(w http.ResponseWriter, r *http.Request, message string) {
	s.respondError(w, r, http.StatusBadRequest, "invalid_request", message)
}

func (s *Server) respondNotFound(w http.ResponseWriter, r *http.Request, code, message string) {
	s.respondError(w, r, http.StatusNotFound, code, message)
}

func (s *Server) respondConflict(w http.ResponseWriter, r *http.Request, code, message string) {
	s.respondError(w, r, http.StatusConflict, code, message)
}

func (s *Server) respondInternalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logInternalError(r, "internal_handler_error", err, slog.Int("status", http.StatusInternalServerError))
	s.writeInternalError(w, r)
}

func (s *Server) writeInternalError(w http.ResponseWriter, r *http.Request) {
	if err := writeResponseBody(w, http.StatusInternalServerError, "application/json", []byte(internalErrorBody)); err != nil {
		s.logInternalError(r, "internal_error_response_write_failed", err, slog.Int("status", http.StatusInternalServerError))
	}
}

func writeResponseBody(w http.ResponseWriter, status int, contentType string, body []byte) error {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, err := w.Write(body)
	return err
}

func (s *Server) logInternalError(r *http.Request, source string, err error, attrs ...slog.Attr) {
	if s == nil || s.logger == nil || err == nil || r == nil {
		return
	}

	ctx := r.Context()
	logAttrs := []slog.Attr{
		slog.String("source", source),
		slog.Any("error", err),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	}
	if requestID := middleware.GetReqID(ctx); requestID != "" {
		logAttrs = append(logAttrs, slog.String("request_id", requestID))
	}
	logAttrs = append(logAttrs, attrs...)

	s.logger.LogAttrs(ctx, slog.LevelError, "web internal error", logAttrs...)
}

func parseLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultLimit, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("limit must be a positive integer")
	}
	return value, nil
}

func parseOffset(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("offset must be a non-negative integer")
	}
	return value, nil
}

func parseClientAfterSeq(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("after_seq must be a non-negative integer")
	}
	return value, nil
}

func parseClientFollow(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("follow must be a boolean")
	}
	return value, nil
}

func decodeJSONBody(r *http.Request, out any) error {
	if r == nil || r.Body == nil {
		return fmt.Errorf("request body is required")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxJSONBodySize))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("request body must contain a single JSON object")
	}
	return fmt.Errorf("request body must contain a single JSON object")
}

func (s *Server) respondClientKnownError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrSessionNotFound):
		s.respondError(w, r, http.StatusNotFound, "thread_not_found", err.Error())
	case errors.Is(err, store.ErrRunNotFound):
		s.respondError(w, r, http.StatusNotFound, "run_not_found", err.Error())
	case errors.Is(err, app.ErrClientNoPendingMessage):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrPendingActionDecisionInvalid):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, app.ErrClientProjectionFailed):
		s.respondError(w, r, http.StatusInternalServerError, "run_event_projection_failed", err.Error())
	case errors.Is(err, domain.ErrExecutionNotReady):
		s.respondError(w, r, http.StatusServiceUnavailable, "execution_not_ready", err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}

func clientWorkspaceRoot(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.WorkspaceRoot()
}

func bearerToken(header string) (string, error) {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return "", app.ErrUnauthenticated
	}
	parts := strings.Fields(trimmed)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", app.ErrUnauthenticated
	}
	return parts[1], nil
}
