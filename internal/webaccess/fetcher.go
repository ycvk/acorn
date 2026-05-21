package webaccess

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

type FetchConfig struct {
	UserAgent        string
	Timeout          time.Duration
	MaxResponseBytes int64
	Policy           URLPolicy
	HTTPClient       *http.Client
	Now              func() time.Time
}

type FetchService struct {
	userAgent        string
	timeout          time.Duration
	maxResponseBytes int64
	policy           URLPolicy
	httpClient       *http.Client
	now              func() time.Time
}

type FetchRequest struct {
	URL         string
	ExtractMode ExtractionMode
}

type FetchResult struct {
	URL            string
	FinalURL       string
	Status         int
	ContentType    string
	ContentLength  int64
	FetchedAt      time.Time
	Redirects      []string
	Raw            []byte
	RawSHA256      string
	Extracted      ExtractResult
	ValidatedHosts []string
}

func NewFetchService(cfg FetchConfig) (*FetchService, error) {
	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		return nil, errors.New("web fetch user agent is required")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("web fetch timeout must be > 0")
	}
	if cfg.MaxResponseBytes <= 0 {
		return nil, errors.New("web fetch max response bytes must be > 0")
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &FetchService{
		userAgent:        userAgent,
		timeout:          cfg.Timeout,
		maxResponseBytes: cfg.MaxResponseBytes,
		policy:           cfg.Policy,
		httpClient:       client,
		now:              now,
	}, nil
}

func (s *FetchService) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	if s == nil {
		return FetchResult{}, errors.New("web fetch service is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	validated, err := s.policy.Validate(ctx, req.URL)
	if err != nil {
		return FetchResult{}, err
	}
	client := *s.httpClient
	client.Timeout = s.timeout
	redirects := make([]string, 0)
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		nextValidated, err := s.policy.Validate(ctx, next.URL.String())
		if err != nil {
			return fmt.Errorf("redirect target rejected: %w", err)
		}
		redirects = append(redirects, nextValidated.Normalized)
		return nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, validated.Normalized, nil)
	if err != nil {
		return FetchResult{}, fmt.Errorf("create web fetch request: %w", err)
	}
	httpReq.Header.Set("User-Agent", s.userAgent)
	httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.1")

	resp, err := client.Do(httpReq)
	if err != nil {
		return FetchResult{}, fmt.Errorf("web fetch request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return FetchResult{}, fmt.Errorf("web fetch returned HTTP status %d", resp.StatusCode)
	}
	if resp.ContentLength > s.maxResponseBytes {
		return FetchResult{}, fmt.Errorf("web fetch response length %d exceeds max_response_bytes %d", resp.ContentLength, s.maxResponseBytes)
	}
	raw, err := readBounded(resp.Body, s.maxResponseBytes)
	if err != nil {
		return FetchResult{}, err
	}
	contentType := normalizeContentType(resp.Header.Get("Content-Type"), raw)
	extracted, err := s.extract(validated.Normalized, resp.Request.URL.String(), contentType, raw, req.ExtractMode)
	if err != nil {
		return FetchResult{}, err
	}
	sum := sha256.Sum256(raw)
	return FetchResult{
		URL:            validated.Normalized,
		FinalURL:       resp.Request.URL.String(),
		Status:         resp.StatusCode,
		ContentType:    contentType,
		ContentLength:  int64(len(raw)),
		FetchedAt:      s.now().UTC(),
		Redirects:      append([]string(nil), redirects...),
		Raw:            raw,
		RawSHA256:      hex.EncodeToString(sum[:]),
		Extracted:      extracted,
		ValidatedHosts: append([]string(nil), validated.IPs...),
	}, nil
}

func (s *FetchService) extract(originalURL, finalURL, contentType string, raw []byte, mode ExtractionMode) (ExtractResult, error) {
	switch contentType {
	case "text/html", "application/xhtml+xml":
		return ExtractHTML(ExtractRequest{URL: finalURL, HTML: raw, Mode: mode})
	case "text/plain":
		text := strings.TrimSpace(string(raw))
		if text == "" {
			return ExtractResult{}, errors.New("plain text response is empty")
		}
		return ExtractResult{
			URL:           finalURL,
			Markdown:      text,
			Method:        "plain_text",
			MarkdownBytes: len([]byte(text)),
		}, nil
	default:
		return ExtractResult{}, fmt.Errorf("unsupported web fetch content type %q for %s", contentType, originalURL)
	}
}

func readBounded(r io.Reader, maxBytes int64) ([]byte, error) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read web fetch response: %w", err)
	}
	if n > maxBytes {
		return nil, fmt.Errorf("web fetch response exceeds max_response_bytes %d", maxBytes)
	}
	return buf.Bytes(), nil
}

func normalizeContentType(header string, body []byte) string {
	if mediaType, _, err := mime.ParseMediaType(header); err == nil && strings.TrimSpace(mediaType) != "" {
		return strings.ToLower(strings.TrimSpace(mediaType))
	}
	return strings.ToLower(http.DetectContentType(body))
}
