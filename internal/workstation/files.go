package workstation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	// uploadsDir holds copies of the files the owner sent, one directory per
	// file id, under the workstation's home.
	uploadsDir = "uploads"
	// maxFileBytes bounds one file sent either way.
	maxFileBytes = 1 << 30
)

var uploadID = regexp.MustCompile(`^file_[0-9a-f]{32}$`)

// Uploads lists the upload directory and the ids of the files in it.
type Uploads struct {
	Dir string   `json:"dir"`
	IDs []string `json:"ids"`
}

// rawFile streams a file of the workstation.
func (s *Server) rawFile(w http.ResponseWriter, r *http.Request) {
	path, err := s.requirePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	switch {
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
		return
	case !info.Mode().IsRegular():
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s is not a regular file", path))
		return
	case info.Size() > maxFileBytes:
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("%s is larger than %d bytes", path, maxFileBytes))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		slog.Error("workstation raw file write failed", "path", path, "error", err)
	}
}

func (s *Server) uploads(w http.ResponseWriter, _ *http.Request) {
	dir := filepath.Join(s.home, uploadsDir)
	out := Uploads{Dir: dir, IDs: []string{}}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, e := range entries {
		if e.IsDir() && uploadID.MatchString(e.Name()) {
			out.IDs = append(out.IDs, e.Name())
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// putUpload writes a file as uploads/<id>/<name>. The directory is filled
// beside its final place and moved there, so the agent never sees half a
// file.
func (s *Server) putUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileBytes)
	id, name := r.PathValue("id"), r.URL.Query().Get("name")
	if !uploadID.MatchString(id) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid upload id %q", id))
		return
	}
	if clean, err := core.ThreadFileName(name); err != nil || clean != name {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid upload name %q", name))
		return
	}
	dir := filepath.Join(s.home, uploadsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	incoming := filepath.Join(dir, ".incoming-"+id+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	defer os.RemoveAll(incoming)
	if err := writeUpload(filepath.Join(incoming, name), r.Body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	target := filepath.Join(dir, id)
	if err := os.RemoveAll(target); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.Rename(incoming, target); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func writeUpload(path string, content io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (s *Server) deleteUpload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !uploadID.MatchString(id) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid upload id %q", id))
		return
	}
	if err := os.RemoveAll(filepath.Join(s.home, uploadsDir, id)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// OpenFile streams a file of the workstation and reports its size. The
// caller closes the body.
func (c *Client) OpenFile(ctx context.Context, path string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/files/raw?"+url.Values{"path": {path}}.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("workstation /v1/files/raw: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if err != nil {
			return nil, 0, fmt.Errorf("workstation /v1/files/raw: read error: %w", err)
		}
		return nil, 0, responseError("/v1/files/raw", resp.StatusCode, data)
	}
	return resp.Body, resp.ContentLength, nil
}

// Uploads reports the upload directory and the ids of the files in it.
func (c *Client) Uploads(ctx context.Context) (string, []string, error) {
	var out Uploads
	if err := c.call(ctx, http.MethodGet, "/v1/uploads", nil, &out); err != nil {
		return "", nil, err
	}
	return out.Dir, out.IDs, nil
}

// PutUpload copies a file the owner sent to uploads/<id>/<name>.
func (c *Client) PutUpload(ctx context.Context, id, name string, content io.Reader, size int64) error {
	path := "/v1/uploads/" + id
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+path+"?"+url.Values{"name": {name}}.Encode(), content)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.stream.Do(req)
	if err != nil {
		return fmt.Errorf("workstation %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("workstation %s: read response: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return responseError(path, resp.StatusCode, data)
	}
	return nil
}

// DeleteUpload removes the copy of a file the owner sent.
func (c *Client) DeleteUpload(ctx context.Context, id string) error {
	var out struct{}
	return c.call(ctx, http.MethodDelete, "/v1/uploads/"+id, nil, &out)
}
