// Package workstation runs commands and file operations on the agent's own
// Linux computer. Server runs inside the workstation container as
// `acorn workstation serve`; Client is how the backend reaches it over
// loopback HTTP with a bearer token.
package workstation

import "errors"

// TokenEnv names the environment variable that carries the shared bearer
// token on both sides.
const TokenEnv = "ACORN_WORKSTATION_TOKEN"

// MinTokenLength rejects tokens too short to resist guessing.
const MinTokenLength = 32

// CommandTimeoutSeconds and CommandOutputBytes bound each shell command and
// trigger run the backend sends.
const (
	CommandTimeoutSeconds = 300
	CommandOutputBytes    = 32 << 10
)

const (
	MaxTimeoutSeconds = 3600
	MaxOutputBytes    = 1 << 20
	maxWriteBytes     = 8 << 20
	maxRenderBytes    = 8 << 20
)

// ErrNotText is returned when a file is not UTF-8 text.
var ErrNotText = errors.New("file is not UTF-8 text")

// ErrNoBrowser is returned by render when the workstation has no browser.
var ErrNoBrowser = errors.New("the workstation has no browser for rendering")

type ExecRequest struct {
	Command        string `json:"command"`
	Dir            string `json:"dir,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxOutputBytes int    `json:"max_output_bytes"`
}

// ExecResult reports a finished command. Each stream keeps its head and tail
// when it exceeds the requested size; the omitted middle is marked in place.
type ExecResult struct {
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated bool   `json:"stderr_truncated,omitempty"`
	TimedOut        bool   `json:"timed_out,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
}

type ReadRequest struct {
	Path     string `json:"path"`
	MaxBytes int    `json:"max_bytes"`
}

type ReadResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated,omitempty"`
}

type WriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WriteResult struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

type RenderRequest struct {
	URL            string `json:"url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// RenderResult is a page's HTML after its scripts ran.
type RenderResult struct {
	HTML string `json:"html"`
}

type Status struct {
	Home string `json:"home"`
}

type errorBody struct {
	Error string `json:"error"`
}
