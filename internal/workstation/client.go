package workstation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client calls a workstation Server. Streams use their own HTTP client so a
// request timeout on the other never cuts a long-lived stream.
type Client struct {
	base   string
	token  string
	http   *http.Client
	stream *http.Client
}

func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("workstation url must be an http(s) URL with a host: %q", baseURL)
	}
	if len(token) < MinTokenLength {
		return nil, fmt.Errorf("workstation token must be at least %d characters", MinTokenLength)
	}
	if httpClient == nil {
		return nil, errors.New("workstation http client is required")
	}
	stream := &http.Client{Transport: httpClient.Transport}
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, http: httpClient, stream: stream}, nil
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var out Status
	return out, c.call(ctx, http.MethodGet, "/v1/status", nil, &out)
}

func (c *Client) Exec(ctx context.Context, req ExecRequest) (ExecResult, error) {
	var out ExecResult
	return out, c.call(ctx, http.MethodPost, "/v1/exec", req, &out)
}

func (c *Client) ReadFile(ctx context.Context, req ReadRequest) (ReadResult, error) {
	var out ReadResult
	return out, c.call(ctx, http.MethodPost, "/v1/files/read", req, &out)
}

func (c *Client) WriteFile(ctx context.Context, req WriteRequest) (WriteResult, error) {
	var out WriteResult
	return out, c.call(ctx, http.MethodPost, "/v1/files/write", req, &out)
}

// BrowserTabs lists the pages of the agent's browser.
func (c *Client) BrowserTabs(ctx context.Context) ([]Tab, error) {
	var out struct {
		Tabs []Tab `json:"tabs"`
	}
	return out.Tabs, c.call(ctx, http.MethodGet, "/v1/browser/tabs", nil, &out)
}

// BrowserInput performs one input in the agent's browser.
func (c *Client) BrowserInput(ctx context.Context, in Input) error {
	var out struct{}
	return c.call(ctx, http.MethodPost, "/v1/browser/input", in, &out)
}

// BrowserFrames opens a tab's frame stream as server-sent events, laid out
// for viewport when it is set. The caller closes the body.
func (c *Client) BrowserFrames(ctx context.Context, tab string, viewport Viewport) (io.ReadCloser, error) {
	query := url.Values{"tab": {tab}}
	if viewport != (Viewport{}) {
		query.Set("width", strconv.Itoa(viewport.Width))
		query.Set("height", strconv.Itoa(viewport.Height))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/browser/frames?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("workstation /v1/browser/frames: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if err != nil {
			return nil, fmt.Errorf("workstation /v1/browser/frames: read error: %w", err)
		}
		return nil, responseError("/v1/browser/frames", resp.StatusCode, data)
	}
	return resp.Body, nil
}

// HTTPError is a workstation response other than 200, so callers can tell a
// missing tab (404) or a workstation without a view (501) from other failures.
type HTTPError struct {
	Path    string
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("workstation %s: %s", e.Path, e.Message)
}

func responseError(path string, status int, body []byte) error {
	var e errorBody
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return &HTTPError{Path: path, Status: status, Message: e.Error}
	}
	return &HTTPError{Path: path, Status: status, Message: fmt.Sprintf("HTTP %d", status)}
}

// RenderHTML returns a page's HTML after its scripts ran.
func (c *Client) RenderHTML(ctx context.Context, target string) (string, error) {
	var out RenderResult
	if err := c.call(ctx, http.MethodPost, "/v1/render", RenderRequest{URL: target, TimeoutSeconds: 45}, &out); err != nil {
		return "", err
	}
	return out.HTML, nil
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("workstation %s: %w", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 3*maxRenderBytes))
	if err != nil {
		return fmt.Errorf("workstation %s: read response: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return responseError(path, resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("workstation %s: decode response: %w", path, err)
	}
	return nil
}
