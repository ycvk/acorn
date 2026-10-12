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
	"strings"
)

// Client calls a workstation Server.
type Client struct {
	base  string
	token string
	http  *http.Client
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
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, http: httpClient}, nil
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
		var e errorBody
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("workstation %s: %s", path, e.Error)
		}
		return fmt.Errorf("workstation %s: HTTP %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("workstation %s: decode response: %w", path, err)
	}
	return nil
}
