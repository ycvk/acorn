package workstation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/skills"
)

// TriggerRequest runs the command of trigger Trigger of the package the
// workstation holds under Name at Hash, with Env added to its environment.
type TriggerRequest struct {
	Name           string            `json:"name"`
	Hash           string            `json:"hash"`
	Trigger        string            `json:"trigger"`
	Env            map[string]string `json:"env,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	MaxOutputBytes int               `json:"max_output_bytes"`
}

// runTrigger runs a trigger's command from its package directory, as the
// agent's shell runs commands: bounded, and stopped with its whole process
// group.
func (s *Server) runTrigger(w http.ResponseWriter, r *http.Request) {
	var req TriggerRequest
	if !decode(w, r, &req) {
		return
	}
	switch {
	case !skillNamePattern.MatchString(req.Name) || len(req.Name) > 64:
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid skill name %q", req.Name))
		return
	case strings.TrimSpace(req.Hash) == "" || strings.TrimSpace(req.Trigger) == "":
		writeError(w, http.StatusBadRequest, errors.New("hash and trigger are required"))
		return
	case req.TimeoutSeconds < 1 || req.TimeoutSeconds > MaxTimeoutSeconds:
		writeError(w, http.StatusBadRequest, fmt.Errorf("timeout_seconds must be between 1 and %d", MaxTimeoutSeconds))
		return
	case req.MaxOutputBytes < 2 || req.MaxOutputBytes > MaxOutputBytes:
		writeError(w, http.StatusBadRequest, fmt.Errorf("max_output_bytes must be between 2 and %d", MaxOutputBytes))
		return
	}
	dir := filepath.Join(s.home, skillsDir, req.Name)
	m, err := packageManifest(dir, req.Hash)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	i := -1
	for j, t := range m.Triggers {
		if t.Name == req.Trigger {
			i = j
		}
	}
	if i < 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("package %s has no trigger %s", req.Name, req.Trigger))
		return
	}
	result, err := s.run(r.Context(), ExecRequest{Command: m.Triggers[i].Run, Dir: dir, TimeoutSeconds: req.TimeoutSeconds, MaxOutputBytes: req.MaxOutputBytes}, req.Env)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// RunTrigger runs a package's trigger command on the workstation.
func (c *Client) RunTrigger(ctx context.Context, run skills.TriggerRun) (skills.TriggerResult, error) {
	var out ExecResult
	err := c.call(ctx, http.MethodPost, "/v1/triggers/run", TriggerRequest{
		Name: run.Skill, Hash: run.Hash, Trigger: run.Trigger, Env: run.Env,
		TimeoutSeconds: run.TimeoutSeconds, MaxOutputBytes: run.MaxOutputBytes,
	}, &out)
	if err != nil {
		return skills.TriggerResult{}, err
	}
	return skills.TriggerResult{ExitCode: out.ExitCode, Stdout: out.Stdout, Stderr: out.Stderr, StdoutTruncated: out.StdoutTruncated, TimedOut: out.TimedOut}, nil
}
