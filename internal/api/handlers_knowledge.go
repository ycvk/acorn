package api

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

func (s *Server) handleCreateCapture(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCaptureBodyBytes)
	if err := r.ParseMultipartForm(maxCaptureBodyBytes); err != nil {
		s.respondCaptureError(w, r, captureBodyError(err))
		return
	}
	defer func() {
		if err := r.MultipartForm.RemoveAll(); err != nil {
			s.logInternalError(r, "capture_multipart_cleanup_failed", err)
		}
	}()
	image, err := readCaptureImage(r)
	if err != nil {
		s.respondCaptureError(w, r, err)
		return
	}
	accepted, err := s.captures.Capture(r.Context(), CaptureInput{
		Text:    r.FormValue("text"),
		Subject: r.FormValue("subject"),
		Image:   image,
	})
	if err != nil {
		s.respondCaptureError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusAccepted, accepted)
}

func readCaptureImage(r *http.Request) ([]byte, error) {
	file, _, err := r.FormFile("image")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: image: %v", ErrInvalidCapture, err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxCaptureImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxCaptureImageBytes {
		return nil, fmt.Errorf("%w: image is larger than 10 MiB", ErrCaptureTooLarge)
	}
	return data, nil
}

func captureBodyError(err error) error {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return fmt.Errorf("%w: request body is larger than %d bytes", ErrCaptureTooLarge, maxCaptureBodyBytes)
	case errors.Is(err, http.ErrNotMultipart), errors.Is(err, multipart.ErrMessageTooLarge):
		return fmt.Errorf("%w: send multipart/form-data: %v", ErrInvalidCapture, err)
	default:
		return fmt.Errorf("%w: %v", ErrInvalidCapture, err)
	}
}

func (s *Server) respondCaptureError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrCaptureTooLarge):
		s.respondError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", err.Error())
	case errors.Is(err, ErrInvalidCapture):
		s.respondBadRequest(w, r, err.Error())
	default:
		s.respondClientKnownError(w, r, err)
	}
}

func (s *Server) handleListKnowledgeNotes(w http.ResponseWriter, r *http.Request) {
	limit := defaultKnowledgeListLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > maxKnowledgeListLimit {
			s.respondBadRequest(w, r, fmt.Sprintf("limit must be between 1 and %d", maxKnowledgeListLimit))
			return
		}
		limit = value
	}
	query := r.URL.Query()
	notes, err := s.knowledge.ListNotes(r.Context(), query.Get("q"), query.Get("prefix"), limit)
	if err != nil {
		s.respondKnowledgeError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, notes)
}

func (s *Server) handleGetKnowledgeNote(w http.ResponseWriter, r *http.Request) {
	note, err := s.knowledge.GetNote(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		s.respondKnowledgeError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, note)
}

func (s *Server) respondKnowledgeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, knowledge.ErrInvalidPath):
		s.respondBadRequest(w, r, err.Error())
	case errors.Is(err, core.ErrKnowledgeNoteNotFound):
		s.respondNotFound(w, r, "knowledge_note_not_found", err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}
