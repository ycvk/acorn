package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

type clientSSEWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	started bool
}

func newClientSSEWriter(w http.ResponseWriter) (*clientSSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	return &clientSSEWriter{writer: w, flusher: flusher}, nil
}

func (s *clientSSEWriter) Start() {
	if !s.started {
		s.writer.Header().Set("Content-Type", "text/event-stream")
		s.writer.Header().Set("Cache-Control", "no-cache")
		s.writer.Header().Set("Connection", "keep-alive")
		s.writer.WriteHeader(http.StatusOK)
		s.started = true
	}
}

func (s *clientSSEWriter) Sink(event core.RunEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := validateClientSSEField("event_id", event.EventID); err != nil {
		return err
	}
	if err := validateClientSSEField("event.type", event.Type); err != nil {
		return err
	}
	s.Start()
	if _, err := fmt.Fprintf(s.writer, "id: %s\n", event.EventID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.writer, "event: %s\n", event.Type); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.writer, "data: %s\n\n", body); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func validateClientSSEField(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be a single line", name)
	}
	return nil
}
