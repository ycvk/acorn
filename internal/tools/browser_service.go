package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	cruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/ycvk/acorn/internal/webaccess"
)

const (
	defaultElementLimit = 200
)

const browserExecutableSetupHint = "install Chrome/Chromium on the host and set browser.executable_path in acorn.yaml, for example /usr/bin/chromium"

type Config struct {
	ExecutablePath string
	Headless       bool
	Timeout        time.Duration
	UserAgent      string
	Policy         webaccess.URLPolicy
	Now            func() time.Time
}

type Service struct {
	cfg Config

	mu            sync.Mutex
	allocCancel   context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	profileDir    string

	snapshotGeneration int
	snapshotRefs       map[string]snapshotRef

	consoleEnabled bool
	consoleEntries []ConsoleEntry
	networkEnabled bool
	networkEntries []NetworkEntry

	policyErrors []string
}

type Status struct {
	Configured bool   `json:"configured"`
	Active     bool   `json:"active"`
	CurrentURL string `json:"current_url,omitempty"`
	Title      string `json:"title,omitempty"`
}

type Tab struct {
	ID    string `json:"id"`
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
}

type ScanRequest struct {
	ExtractMode webaccess.ExtractionMode
}

type ScanResult struct {
	URL       string
	Title     string
	Extracted webaccess.ExtractResult
}

type SnapshotElement struct {
	Ref          string `json:"ref"`
	Role         string `json:"role,omitempty"`
	Name         string `json:"name,omitempty"`
	Selector     string `json:"selector,omitempty"`
	ValuePreview string `json:"value_preview,omitempty"`
	Enabled      bool   `json:"enabled"`
	Visible      bool   `json:"visible"`
}

type SnapshotResult struct {
	URL        string            `json:"url,omitempty"`
	Title      string            `json:"title,omitempty"`
	Generation int               `json:"generation"`
	Elements   []SnapshotElement `json:"elements"`
}

type NavigateResult struct {
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
}

type ConsoleEntry struct {
	Type      string   `json:"type"`
	Args      []string `json:"args,omitempty"`
	Timestamp string   `json:"timestamp"`
}

type ConsoleResult struct {
	Enabled bool           `json:"enabled"`
	Entries []ConsoleEntry `json:"entries,omitempty"`
}

type NetworkEntry struct {
	URL               string  `json:"url"`
	Status            int64   `json:"status"`
	StatusText        string  `json:"status_text,omitempty"`
	MimeType          string  `json:"mime_type,omitempty"`
	ResourceType      string  `json:"resource_type,omitempty"`
	EncodedDataLength float64 `json:"encoded_data_length,omitempty"`
	Timestamp         string  `json:"timestamp"`
}

type NetworkResult struct {
	Enabled bool           `json:"enabled"`
	Entries []NetworkEntry `json:"entries,omitempty"`
}

type snapshotRef struct {
	Selector   string
	Generation int
	URL        string
}

func NewService(cfg Config) (*Service, error) {
	cfg.ExecutablePath = strings.TrimSpace(cfg.ExecutablePath)
	cfg.UserAgent = strings.TrimSpace(cfg.UserAgent)
	if cfg.Timeout <= 0 {
		return nil, errors.New("browser timeout must be > 0")
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		cfg:          cfg,
		snapshotRefs: make(map[string]snapshotRef),
	}, nil
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	if s == nil {
		return Status{}, errors.New("browser service is nil")
	}
	status := Status{Configured: s.configured()}
	browserCtx := s.currentContext()
	if browserCtx == nil {
		return status, nil
	}
	status.Active = true
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	if err := chromedp.Run(actionCtx, chromedp.Location(&status.CurrentURL), chromedp.Title(&status.Title)); err != nil {
		return Status{}, err
	}
	return status, nil
}

func (s *Service) Open(ctx context.Context, rawURL string) (NavigateResult, error) {
	if s == nil {
		return NavigateResult{}, errors.New("browser service is nil")
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return NavigateResult{}, errors.New("browser open requires url")
	}
	if _, err := s.cfg.Policy.Validate(ctx, rawURL); err != nil {
		return NavigateResult{}, err
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return NavigateResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	var result NavigateResult
	if err := chromedp.Run(actionCtx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		chromedp.Navigate(rawURL),
		chromedp.Location(&result.URL),
		chromedp.Title(&result.Title),
	); err != nil {
		if policyErr := s.takePolicyError(); policyErr != "" {
			return NavigateResult{}, errors.New(policyErr)
		}
		return NavigateResult{}, err
	}
	return result, nil
}

func (s *Service) Tabs(ctx context.Context) ([]Tab, error) {
	if s == nil {
		return nil, errors.New("browser service is nil")
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return nil, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	targets, err := chromedp.Targets(actionCtx)
	if err != nil {
		return nil, err
	}
	tabs := make([]Tab, 0, len(targets))
	for _, target := range targets {
		if target == nil || target.Type != "page" {
			continue
		}
		tabs = append(tabs, Tab{
			ID:    string(target.TargetID),
			URL:   target.URL,
			Title: target.Title,
			Type:  target.Type,
		})
	}
	return tabs, nil
}

func (s *Service) Scan(ctx context.Context, req ScanRequest) (ScanResult, error) {
	if s == nil {
		return ScanResult{}, errors.New("browser service is nil")
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return ScanResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	var pageURL, title, html string
	if err := chromedp.Run(actionCtx,
		chromedp.Location(&pageURL),
		chromedp.Title(&title),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	); err != nil {
		return ScanResult{}, err
	}
	if strings.TrimSpace(pageURL) == "" {
		return ScanResult{}, errors.New("browser scan requires an open page")
	}
	extracted, err := webaccess.ExtractHTML(webaccess.ExtractRequest{
		URL:  pageURL,
		HTML: []byte(html),
		Mode: req.ExtractMode,
	})
	if err != nil {
		return ScanResult{}, err
	}
	return ScanResult{URL: pageURL, Title: title, Extracted: extracted}, nil
}

func (s *Service) Snapshot(ctx context.Context) (SnapshotResult, error) {
	if s == nil {
		return SnapshotResult{}, errors.New("browser service is nil")
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return SnapshotResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	var pageURL, title string
	var raw []snapshotElementRaw
	if err := chromedp.Run(actionCtx,
		chromedp.Location(&pageURL),
		chromedp.Title(&title),
		chromedp.Evaluate(snapshotScript(defaultElementLimit), &raw),
	); err != nil {
		return SnapshotResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotGeneration++
	generation := s.snapshotGeneration
	s.snapshotRefs = make(map[string]snapshotRef, len(raw))
	elements := make([]SnapshotElement, 0, len(raw))
	for idx, item := range raw {
		ref := fmt.Sprintf("e%d", idx+1)
		selector := strings.TrimSpace(item.Selector)
		if selector == "" {
			continue
		}
		s.snapshotRefs[ref] = snapshotRef{Selector: selector, Generation: generation, URL: pageURL}
		elements = append(elements, SnapshotElement{
			Ref:          ref,
			Role:         strings.TrimSpace(item.Role),
			Name:         strings.TrimSpace(item.Name),
			Selector:     selector,
			ValuePreview: strings.TrimSpace(item.ValuePreview),
			Enabled:      item.Enabled,
			Visible:      item.Visible,
		})
	}
	return SnapshotResult{URL: pageURL, Title: title, Generation: generation, Elements: elements}, nil
}

func (s *Service) Click(ctx context.Context, ref, selector string) (NavigateResult, error) {
	resolved, err := s.resolveSelector(ref, selector)
	if err != nil {
		return NavigateResult{}, err
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return NavigateResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	if err := s.requireUniqueSelector(actionCtx, resolved); err != nil {
		return NavigateResult{}, err
	}
	var result NavigateResult
	if err := chromedp.Run(actionCtx,
		chromedp.ScrollIntoView(resolved, chromedp.ByQuery),
		chromedp.Click(resolved, chromedp.ByQuery),
		chromedp.Location(&result.URL),
		chromedp.Title(&result.Title),
	); err != nil {
		return NavigateResult{}, err
	}
	return result, nil
}

func (s *Service) Fill(ctx context.Context, ref, selector, text string) (NavigateResult, error) {
	resolved, err := s.resolveSelector(ref, selector)
	if err != nil {
		return NavigateResult{}, err
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return NavigateResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	if err := s.requireUniqueSelector(actionCtx, resolved); err != nil {
		return NavigateResult{}, err
	}
	var result NavigateResult
	if err := chromedp.Run(actionCtx,
		chromedp.Focus(resolved, chromedp.ByQuery),
		chromedp.Evaluate(actionScript("fill", resolved, text), nil),
		chromedp.Location(&result.URL),
		chromedp.Title(&result.Title),
	); err != nil {
		return NavigateResult{}, err
	}
	return result, nil
}

func (s *Service) Press(ctx context.Context, ref, selector, key string) (NavigateResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return NavigateResult{}, errors.New("browser press requires key")
	}
	resolved, err := s.resolveOptionalSelector(ref, selector)
	if err != nil {
		return NavigateResult{}, err
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return NavigateResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	actions := chromedp.Tasks{}
	if resolved != "" {
		if err := s.requireUniqueSelector(actionCtx, resolved); err != nil {
			return NavigateResult{}, err
		}
		actions = append(actions, chromedp.Focus(resolved, chromedp.ByQuery))
	}
	var result NavigateResult
	actions = append(actions,
		chromedp.KeyEvent(key),
		chromedp.Location(&result.URL),
		chromedp.Title(&result.Title),
	)
	if err := chromedp.Run(actionCtx, actions); err != nil {
		return NavigateResult{}, err
	}
	return result, nil
}

func (s *Service) Select(ctx context.Context, ref, selector, value string) (NavigateResult, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return NavigateResult{}, errors.New("browser select requires value")
	}
	resolved, err := s.resolveSelector(ref, selector)
	if err != nil {
		return NavigateResult{}, err
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return NavigateResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	if err := s.requireUniqueSelector(actionCtx, resolved); err != nil {
		return NavigateResult{}, err
	}
	var result NavigateResult
	if err := chromedp.Run(actionCtx,
		chromedp.Focus(resolved, chromedp.ByQuery),
		chromedp.Evaluate(actionScript("select", resolved, value), nil),
		chromedp.Location(&result.URL),
		chromedp.Title(&result.Title),
	); err != nil {
		return NavigateResult{}, err
	}
	return result, nil
}

func (s *Service) Screenshot(ctx context.Context, fullPage bool) ([]byte, error) {
	if s == nil {
		return nil, errors.New("browser service is nil")
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return nil, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	var buf []byte
	var action chromedp.Action
	if fullPage {
		action = chromedp.FullScreenshot(&buf, 90)
	} else {
		action = chromedp.CaptureScreenshot(&buf)
	}
	if err := chromedp.Run(actionCtx, action); err != nil {
		return nil, err
	}
	if len(buf) == 0 {
		return nil, errors.New("browser screenshot produced empty image")
	}
	return buf, nil
}

func (s *Service) ConsoleStart(ctx context.Context) (ConsoleResult, error) {
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return ConsoleResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	if err := chromedp.Run(actionCtx, cruntime.Enable()); err != nil {
		return ConsoleResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consoleEnabled = true
	s.consoleEntries = nil
	return ConsoleResult{Enabled: true}, nil
}

func (s *Service) ConsoleList() ConsoleResult {
	if s == nil {
		return ConsoleResult{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ConsoleResult{Enabled: s.consoleEnabled, Entries: append([]ConsoleEntry(nil), s.consoleEntries...)}
}

func (s *Service) ConsoleStop() ConsoleResult {
	if s == nil {
		return ConsoleResult{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consoleEnabled = false
	return ConsoleResult{Enabled: false, Entries: append([]ConsoleEntry(nil), s.consoleEntries...)}
}

func (s *Service) NetworkStart(ctx context.Context) (NetworkResult, error) {
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return NetworkResult{}, err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	if err := chromedp.Run(actionCtx, network.Enable()); err != nil {
		return NetworkResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networkEnabled = true
	s.networkEntries = nil
	return NetworkResult{Enabled: true}, nil
}

func (s *Service) NetworkList() NetworkResult {
	if s == nil {
		return NetworkResult{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return NetworkResult{Enabled: s.networkEnabled, Entries: append([]NetworkEntry(nil), s.networkEntries...)}
}

func (s *Service) NetworkStop() NetworkResult {
	if s == nil {
		return NetworkResult{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networkEnabled = false
	return NetworkResult{Enabled: false, Entries: append([]NetworkEntry(nil), s.networkEntries...)}
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	browserCancel := s.browserCancel
	allocCancel := s.allocCancel
	profileDir := s.profileDir
	s.allocCancel = nil
	s.browserCtx = nil
	s.browserCancel = nil
	s.profileDir = ""
	s.snapshotRefs = make(map[string]snapshotRef)
	s.consoleEnabled = false
	s.networkEnabled = false
	s.policyErrors = nil
	s.mu.Unlock()

	if browserCancel != nil {
		browserCancel()
	}
	if allocCancel != nil {
		allocCancel()
	}
	if profileDir != "" {
		if err := os.RemoveAll(profileDir); err != nil {
			return fmt.Errorf("remove browser temp profile: %w", err)
		}
	}
	return nil
}

func (s *Service) configured() bool {
	return s != nil && strings.TrimSpace(s.cfg.ExecutablePath) != ""
}

func (s *Service) currentContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.browserCtx
}

func (s *Service) ensureStarted(ctx context.Context) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.browserCtx != nil {
		return s.browserCtx, nil
	}
	executablePath, err := verifyBrowserExecutable(s.cfg.ExecutablePath)
	if err != nil {
		return nil, err
	}
	profileDir, err := os.MkdirTemp("", "acorn-browser-*")
	if err != nil {
		return nil, fmt.Errorf("create browser temp profile: %w", err)
	}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options,
		chromedp.ExecPath(executablePath),
		chromedp.UserDataDir(profileDir),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
	)
	if s.cfg.Headless {
		options = append(options, chromedp.Headless)
	}
	if s.cfg.UserAgent != "" {
		options = append(options, chromedp.UserAgent(s.cfg.UserAgent))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), options...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	chromedp.ListenTarget(browserCtx, s.handleEvent)
	s.allocCancel = allocCancel
	s.browserCancel = browserCancel
	s.browserCtx = browserCtx
	s.profileDir = profileDir
	return browserCtx, nil
}

func verifyBrowserExecutable(configuredPath string) (string, error) {
	trimmed := strings.TrimSpace(configuredPath)
	if trimmed == "" {
		return "", fmt.Errorf("browser.executable_path is not configured; %s", browserExecutableSetupHint)
	}
	if filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, os.PathSeparator) {
		info, err := os.Stat(trimmed)
		if err != nil {
			return "", fmt.Errorf("browser.executable_path %q is not accessible: %w; %s", trimmed, err, browserExecutableSetupHint)
		}
		if info.IsDir() {
			return "", fmt.Errorf("browser.executable_path %q points to a directory; %s", trimmed, browserExecutableSetupHint)
		}
		if info.Mode().Perm()&0111 == 0 {
			return "", fmt.Errorf("browser.executable_path %q is not executable; %s", trimmed, browserExecutableSetupHint)
		}
		return trimmed, nil
	}
	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return "", fmt.Errorf("browser.executable_path %q was not found in PATH: %w; %s", trimmed, err, browserExecutableSetupHint)
	}
	return resolved, nil
}

func (s *Service) actionContext(parent context.Context, browserCtx context.Context) (context.Context, context.CancelFunc) {
	actionCtx, cancel := context.WithTimeout(browserCtx, s.cfg.Timeout)
	stop := context.AfterFunc(parent, cancel)
	return actionCtx, func() {
		stop()
		cancel()
	}
}

func (s *Service) resolveSelector(ref, selector string) (string, error) {
	resolved, err := s.resolveOptionalSelector(ref, selector)
	if err != nil {
		return "", err
	}
	if resolved == "" {
		return "", errors.New("browser action requires ref or selector")
	}
	return resolved, nil
}

func (s *Service) resolveOptionalSelector(ref, selector string) (string, error) {
	ref = strings.TrimSpace(ref)
	selector = strings.TrimSpace(selector)
	if ref != "" && selector != "" {
		return "", errors.New("browser action accepts ref or selector, not both")
	}
	if selector != "" {
		return selector, nil
	}
	if ref == "" {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok := s.snapshotRefs[ref]
	if !ok {
		return "", fmt.Errorf("browser ref %q is expired or unknown; call snapshot again", ref)
	}
	if cached.Generation != s.snapshotGeneration {
		return "", fmt.Errorf("browser ref %q is expired; call snapshot again", ref)
	}
	return cached.Selector, nil
}

func (s *Service) requireUniqueSelector(ctx context.Context, selector string) error {
	var count int
	if err := chromedp.Run(ctx, chromedp.Evaluate(selectorCountScript(selector), &count)); err != nil {
		return err
	}
	switch count {
	case 0:
		return fmt.Errorf("browser selector matched no elements: %s", selector)
	case 1:
		return nil
	default:
		return fmt.Errorf("browser selector matched %d elements: %s", count, selector)
	}
}

func (s *Service) handleEvent(ev any) {
	if s == nil {
		return
	}
	now := s.cfg.Now().Format(time.RFC3339Nano)
	switch event := ev.(type) {
	case *cruntime.EventConsoleAPICalled:
		entry := ConsoleEntry{
			Type:      string(event.Type),
			Timestamp: now,
		}
		for _, arg := range event.Args {
			entry.Args = append(entry.Args, remoteObjectString(arg))
		}
		s.mu.Lock()
		if s.consoleEnabled {
			s.consoleEntries = append(s.consoleEntries, entry)
		}
		s.mu.Unlock()
	case *network.EventResponseReceived:
		if event.Response == nil {
			return
		}
		entry := NetworkEntry{
			URL:               event.Response.URL,
			Status:            event.Response.Status,
			StatusText:        event.Response.StatusText,
			MimeType:          event.Response.MimeType,
			ResourceType:      string(event.Type),
			EncodedDataLength: event.Response.EncodedDataLength,
			Timestamp:         now,
		}
		s.mu.Lock()
		if s.networkEnabled {
			s.networkEntries = append(s.networkEntries, entry)
		}
		s.mu.Unlock()
	case *fetch.EventRequestPaused:
		if event.Request == nil {
			s.continueFetchRequest(event.RequestID)
			return
		}
		requestURL := strings.TrimSpace(event.Request.URL)
		if requestURL == "" {
			s.continueFetchRequest(event.RequestID)
			return
		}
		go s.handlePausedRequest(event.RequestID, requestURL)
	}
}

func (s *Service) handlePausedRequest(requestID fetch.RequestID, requestURL string) {
	policyCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	if !s.handlePausedRequestForTest(policyCtx, requestURL) {
		s.failFetchRequest(requestID)
		return
	}
	s.continueFetchRequest(requestID)
}

func (s *Service) continueFetchRequest(requestID fetch.RequestID) {
	browserCtx := s.currentContext()
	if browserCtx == nil {
		return
	}
	if err := chromedp.Run(browserCtx, fetch.ContinueRequest(requestID)); err != nil {
		s.mu.Lock()
		s.policyErrors = append(s.policyErrors, fmt.Sprintf("continue browser request %s: %v", requestID, err))
		s.mu.Unlock()
	}
}

func (s *Service) failFetchRequest(requestID fetch.RequestID) {
	browserCtx := s.currentContext()
	if browserCtx == nil {
		return
	}
	if err := chromedp.Run(browserCtx, fetch.FailRequest(requestID, network.ErrorReasonBlockedByClient)); err != nil {
		s.mu.Lock()
		s.policyErrors = append(s.policyErrors, fmt.Sprintf("fail browser request %s: %v", requestID, err))
		s.mu.Unlock()
	}
}

func (s *Service) handlePausedRequestForTest(ctx context.Context, requestURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(requestURL))
	if err != nil {
		s.mu.Lock()
		s.policyErrors = append(s.policyErrors, fmt.Sprintf("browser blocked request %s: parse url: %v", requestURL, err))
		s.mu.Unlock()
		return false
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https":
	case "about", "blob", "data":
		return true
	default:
		s.mu.Lock()
		s.policyErrors = append(s.policyErrors, fmt.Sprintf("browser blocked request %s: unsupported url scheme %q", requestURL, parsed.Scheme))
		s.mu.Unlock()
		return false
	}
	if _, err := s.cfg.Policy.Validate(ctx, requestURL); err != nil {
		s.mu.Lock()
		s.policyErrors = append(s.policyErrors, fmt.Sprintf("browser blocked request %s: %v", requestURL, err))
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *Service) takePolicyError() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.policyErrors) == 0 {
		return ""
	}
	message := s.policyErrors[0]
	s.policyErrors = nil
	return message
}

func remoteObjectString(obj *cruntime.RemoteObject) string {
	if obj == nil {
		return ""
	}
	if len(obj.Value) > 0 {
		var value any
		if err := json.Unmarshal(obj.Value, &value); err == nil {
			return fmt.Sprint(value)
		}
	}
	if obj.Description != "" {
		return obj.Description
	}
	if obj.UnserializableValue != "" {
		return string(obj.UnserializableValue)
	}
	return string(obj.Type)
}

type snapshotElementRaw struct {
	Selector     string `json:"selector"`
	Role         string `json:"role"`
	Name         string `json:"name"`
	ValuePreview string `json:"value_preview"`
	Enabled      bool   `json:"enabled"`
	Visible      bool   `json:"visible"`
}

func selectorCountScript(selector string) string {
	selectorJSON := strconv.Quote(selector)
	return fmt.Sprintf(`(() => document.querySelectorAll(%s).length)()`, selectorJSON)
}

func actionScript(action, selector, value string) string {
	selectorJSON := strconv.Quote(selector)
	valueJSON := strconv.Quote(value)
	return fmt.Sprintf(`(() => {
  const matches = document.querySelectorAll(%s);
  if (matches.length !== 1) throw new Error("selector must match exactly one element");
  const el = matches[0];
  if (%q === "fill") {
    if (el.isContentEditable) {
      el.textContent = %s;
    } else if ("value" in el) {
      el.value = %s;
    } else {
      throw new Error("target is not fillable");
    }
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
    return true;
  }
  if (%q === "select") {
    if (!(el instanceof HTMLSelectElement)) throw new Error("target is not a select element");
    const wanted = %s;
    const option = Array.from(el.options).find((candidate) =>
      candidate.value === wanted || candidate.label === wanted || candidate.text === wanted
    );
    if (!option) throw new Error("select option not found");
    el.value = option.value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
    return true;
  }
  throw new Error("unsupported browser action");
})()`, selectorJSON, action, valueJSON, valueJSON, action, valueJSON)
}

func snapshotScript(limit int) string {
	if limit <= 0 {
		limit = defaultElementLimit
	}
	return fmt.Sprintf(`(() => {
  const limit = %d;
  const candidates = Array.from(document.querySelectorAll([
    "a[href]",
    "button",
    "input",
    "textarea",
    "select",
    "[role=button]",
    "[role=link]",
    "[contenteditable=true]",
    "[tabindex]:not([tabindex='-1'])"
  ].join(",")));
  const cssEscape = (value) => {
    if (window.CSS && CSS.escape) return CSS.escape(value);
    return String(value).replace(/[^a-zA-Z0-9_-]/g, "\\$&");
  };
  const visible = (el) => {
    const style = window.getComputedStyle(el);
    const rect = el.getBoundingClientRect();
    return style.display !== "none" && style.visibility !== "hidden" && Number(style.opacity) !== 0 && rect.width > 0 && rect.height > 0;
  };
  const roleFor = (el) => {
    const explicit = el.getAttribute("role");
    if (explicit) return explicit;
    const tag = el.tagName.toLowerCase();
    if (tag === "a") return "link";
    if (tag === "button") return "button";
    if (tag === "select") return "combobox";
    if (tag === "textarea") return "textbox";
    if (tag === "input") {
      const type = (el.getAttribute("type") || "text").toLowerCase();
      if (type === "checkbox") return "checkbox";
      if (type === "radio") return "radio";
      if (type === "submit" || type === "button") return "button";
      return "textbox";
    }
    return tag;
  };
  const nameFor = (el) => {
    const aria = el.getAttribute("aria-label");
    if (aria) return aria.trim();
    const labelledBy = el.getAttribute("aria-labelledby");
    if (labelledBy) {
      const text = labelledBy.split(/\s+/).map((id) => document.getElementById(id)?.innerText || "").join(" ").trim();
      if (text) return text;
    }
    if (el.labels && el.labels.length) {
      const label = Array.from(el.labels).map((item) => item.innerText || "").join(" ").trim();
      if (label) return label;
    }
    if (el.title) return el.title.trim();
    if (el.placeholder) return el.placeholder.trim();
    return (el.innerText || el.value || "").trim();
  };
  const selectorFor = (el) => {
    if (el.id) {
      const selector = "#" + cssEscape(el.id);
      if (document.querySelectorAll(selector).length === 1) return selector;
    }
    const parts = [];
    let node = el;
    while (node && node.nodeType === Node.ELEMENT_NODE && node !== document.documentElement) {
      let part = node.tagName.toLowerCase();
      const parent = node.parentElement;
      if (!parent) break;
      const sameTag = Array.from(parent.children).filter((child) => child.tagName === node.tagName);
      if (sameTag.length > 1) {
        const index = sameTag.indexOf(node) + 1;
        part += ":nth-of-type(" + index + ")";
      }
      parts.unshift(part);
      const selector = parts.join(" > ");
      if (document.querySelectorAll(selector).length === 1) return selector;
      node = parent;
    }
    return parts.join(" > ");
  };
  const out = [];
  for (const el of candidates) {
    if (out.length >= limit) break;
    const selector = selectorFor(el);
    if (!selector) continue;
    const disabled = Boolean(el.disabled) || el.getAttribute("aria-disabled") === "true";
    let value = "";
    if ("value" in el && typeof el.value === "string") value = el.value;
    out.push({
      selector,
      role: roleFor(el),
      name: nameFor(el).slice(0, 160),
      value_preview: value.slice(0, 120),
      enabled: !disabled,
      visible: visible(el)
    });
  }
  return out;
})()`, limit)
}

func ValidURL(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}
