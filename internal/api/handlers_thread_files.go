package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

// multipartOverheadBytes leaves room for the multipart framing around a file.
const multipartOverheadBytes = 1 << 20

// handleUploadThreadFile stores the file part of a multipart body as it
// streams in, without buffering it in memory.
func (s *Server) handleUploadThreadFile(w http.ResponseWriter, r *http.Request) {
	limit := s.cfg.Files.MaxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, limit+multipartOverheadBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		s.respondBadRequest(w, r, fmt.Sprintf("send multipart/form-data with a file part: %v", err))
		return
	}
	part, err := reader.NextPart()
	switch {
	case errors.Is(err, io.EOF):
		s.respondBadRequest(w, r, "the file part is required")
		return
	case err != nil:
		s.respondThreadFileError(w, r, err)
		return
	case part.FormName() != "file":
		s.respondBadRequest(w, r, fmt.Sprintf("unexpected part %q; send the file as the part named file", part.FormName()))
		return
	}
	defer func() { _ = part.Close() }()
	file, err := s.threads.UploadFile(r.Context(), chi.URLParam(r, "thread_id"), part.FileName(), part, limit)
	if err != nil {
		s.respondThreadFileError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusCreated, DefaultConverter.threadFileDTOFromDomain(file))
}

func (s *Server) respondThreadFileError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		err = fmt.Errorf("%w: larger than %d MiB", core.ErrThreadFileTooLarge, s.cfg.Files.MaxUploadMiB)
	}
	s.respondClientKnownError(w, r, err)
}

// handleGetThreadFile serves a file of a thread. File content never changes,
// so clients may cache it indefinitely.
func (s *Server) handleGetThreadFile(w http.ResponseWriter, r *http.Request) {
	file, content, err := s.threads.OpenFile(r.Context(), chi.URLParam(r, "thread_id"), chi.URLParam(r, "file_id"))
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	defer func() { _ = content.Close() }()
	w.Header().Set("Content-Type", file.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, content); err != nil {
		s.logInternalError(r, "thread_file_write_failed", err)
	}
}
