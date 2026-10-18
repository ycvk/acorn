package workstation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// workDir holds the directories the agent works on concerns in, one per
// concern id, under the workstation's home.
const workDir = "work"

var concernID = regexp.MustCompile(`^m_[0-9a-f]{32}$`)

// WorkDirs lists the ids of the concerns that have a work directory.
type WorkDirs struct {
	IDs []string `json:"ids"`
}

func (s *Server) workDirs(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(filepath.Join(s.home, workDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	out := WorkDirs{IDs: []string{}}
	for _, e := range entries {
		if e.IsDir() && concernID.MatchString(e.Name()) {
			out.IDs = append(out.IDs, e.Name())
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteWorkDir(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !concernID.MatchString(id) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid concern id %q", id))
		return
	}
	if err := os.RemoveAll(filepath.Join(s.home, workDir, id)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// WorkDirs reports the ids of the concerns that have a work directory.
func (c *Client) WorkDirs(ctx context.Context) ([]string, error) {
	var out WorkDirs
	if err := c.call(ctx, http.MethodGet, "/v1/work", nil, &out); err != nil {
		return nil, err
	}
	return out.IDs, nil
}

// DeleteWorkDir removes a concern's work directory with everything in it.
func (c *Client) DeleteWorkDir(ctx context.Context, id string) error {
	var out struct{}
	return c.call(ctx, http.MethodDelete, "/v1/work/"+id, nil, &out)
}
