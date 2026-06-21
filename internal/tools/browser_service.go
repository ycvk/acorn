package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	browserCancel, allocCancel, profileDir := s.resetBrowserState()
	return teardownBrowser(browserCancel, allocCancel, profileDir)
}

func (s *Service) resetBrowserState() (context.CancelFunc, context.CancelFunc, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return browserCancel, allocCancel, profileDir
}

func teardownBrowser(browserCancel, allocCancel context.CancelFunc, profileDir string) error {
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
	browserCtx, browserCancel, allocCancel := s.startBrowser(executablePath, profileDir)
	s.allocCancel = allocCancel
	s.browserCancel = browserCancel
	s.browserCtx = browserCtx
	s.profileDir = profileDir
	return browserCtx, nil
}

func (s *Service) startBrowser(executablePath, profileDir string) (context.Context, context.CancelFunc, context.CancelFunc) {
	options := s.buildAllocatorOptions(executablePath, profileDir)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), options...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	chromedp.ListenTarget(browserCtx, s.handleEvent)
	return browserCtx, browserCancel, allocCancel
}

func (s *Service) buildAllocatorOptions(executablePath, profileDir string) []chromedp.ExecAllocatorOption {
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
	return options
}

func verifyBrowserExecutable(configuredPath string) (string, error) {
	trimmed := strings.TrimSpace(configuredPath)
	if trimmed == "" {
		return "", fmt.Errorf("browser.executable_path is not configured; %s", browserExecutableSetupHint)
	}
	if filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, os.PathSeparator) {
		return verifyBrowserExecutablePath(trimmed)
	}
	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return "", fmt.Errorf("browser.executable_path %q was not found in PATH: %w; %s", trimmed, err, browserExecutableSetupHint)
	}
	return resolved, nil
}

func verifyBrowserExecutablePath(trimmed string) (string, error) {
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

func (s *Service) actionContext(parent context.Context, browserCtx context.Context) (context.Context, context.CancelFunc) {
	actionCtx, cancel := context.WithTimeout(browserCtx, s.cfg.Timeout)
	stop := context.AfterFunc(parent, cancel)
	return actionCtx, func() {
		stop()
		cancel()
	}
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
