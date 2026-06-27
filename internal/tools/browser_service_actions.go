package tools

// Console, Network, Navigate, and event-handling methods split from browser_service.go for line limits.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	cruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

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
	result, err := s.navigateOpen(actionCtx, rawURL)
	if err != nil {
		if policyErr := s.takePolicyError(); policyErr != "" {
			return NavigateResult{}, errors.New(policyErr)
		}
		return NavigateResult{}, err
	}
	return result, nil
}

func (s *Service) navigateOpen(actionCtx context.Context, rawURL string) (NavigateResult, error) {
	var result NavigateResult
	err := chromedp.Run(actionCtx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		chromedp.Navigate(rawURL),
		chromedp.Location(&result.URL),
		chromedp.Title(&result.Title),
	)
	return result, err
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
	return pageTabs(targets), nil
}

func pageTabs(targets []*target.Info) []Tab {
	tabs := make([]Tab, 0, len(targets))
	for _, t := range targets {
		if t == nil || t.Type != "page" {
			continue
		}
		tabs = append(tabs, Tab{
			ID:    string(t.TargetID),
			URL:   t.URL,
			Title: t.Title,
			Type:  t.Type,
		})
	}
	return tabs
}

func (s *Service) Click(ctx context.Context, ref, selector string) (NavigateResult, error) {
	resolved, err := s.resolveSelector(ref, selector)
	if err != nil {
		return NavigateResult{}, err
	}
	return s.runUniqueAction(ctx, resolved, func(actionCtx context.Context, result *NavigateResult) error {
		return chromedp.Run(actionCtx,
			chromedp.ScrollIntoView(resolved, chromedp.ByQuery),
			chromedp.Click(resolved, chromedp.ByQuery),
			chromedp.Location(&result.URL),
			chromedp.Title(&result.Title),
		)
	})
}

func (s *Service) Fill(ctx context.Context, ref, selector, text string) (NavigateResult, error) {
	resolved, err := s.resolveSelector(ref, selector)
	if err != nil {
		return NavigateResult{}, err
	}
	return s.runUniqueAction(ctx, resolved, func(actionCtx context.Context, result *NavigateResult) error {
		return chromedp.Run(actionCtx,
			chromedp.Focus(resolved, chromedp.ByQuery),
			chromedp.Evaluate(actionScript("fill", resolved, text), nil),
			chromedp.Location(&result.URL),
			chromedp.Title(&result.Title),
		)
	})
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
	return s.runUniqueAction(ctx, resolved, func(actionCtx context.Context, result *NavigateResult) error {
		return chromedp.Run(actionCtx,
			chromedp.Focus(resolved, chromedp.ByQuery),
			chromedp.Evaluate(actionScript("select", resolved, value), nil),
			chromedp.Location(&result.URL),
			chromedp.Title(&result.Title),
		)
	})
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
	if err := s.pressFocused(actionCtx, resolved, key); err != nil {
		return NavigateResult{}, err
	}
	return s.navigateAfterAction(actionCtx)
}

func (s *Service) pressFocused(actionCtx context.Context, resolved, key string) error {
	if resolved != "" {
		if err := s.requireUniqueSelector(actionCtx, resolved); err != nil {
			return err
		}
		if err := chromedp.Run(actionCtx, chromedp.Focus(resolved, chromedp.ByQuery)); err != nil {
			return err
		}
	}
	return chromedp.Run(actionCtx, chromedp.KeyEvent(key))
}

func (s *Service) navigateAfterAction(actionCtx context.Context) (NavigateResult, error) {
	var result NavigateResult
	err := chromedp.Run(actionCtx,
		chromedp.Location(&result.URL),
		chromedp.Title(&result.Title),
	)
	return result, err
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
	buf, err := captureScreenshot(actionCtx, fullPage)
	if err != nil {
		return nil, err
	}
	if len(buf) == 0 {
		return nil, errors.New("browser screenshot produced empty image")
	}
	return buf, nil
}

func captureScreenshot(actionCtx context.Context, fullPage bool) ([]byte, error) {
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
	return buf, nil
}

// runUniqueAction runs a selector-based browser action that requires a unique match.
func (s *Service) runUniqueAction(ctx context.Context, resolved string, run func(context.Context, *NavigateResult) error) (NavigateResult, error) {
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
	if err := run(actionCtx, &result); err != nil {
		return NavigateResult{}, err
	}
	return result, nil
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
	return s.resolveSnapshotRef(ref)
}

func (s *Service) resolveSnapshotRef(ref string) (string, error) {
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
		s.recordConsoleEvent(event, now)
	case *network.EventResponseReceived:
		s.recordNetworkResponse(event, now)
	case *fetch.EventRequestPaused:
		s.handlePausedEvent(event)
	}
}

func (s *Service) recordConsoleEvent(event *cruntime.EventConsoleAPICalled, now string) {
	if event == nil {
		return
	}
	entry := ConsoleEntry{Type: string(event.Type), Timestamp: now}
	for _, arg := range event.Args {
		entry.Args = append(entry.Args, remoteObjectString(arg))
	}
	s.mu.Lock()
	if s.consoleEnabled {
		s.consoleEntries = append(s.consoleEntries, entry)
	}
	s.mu.Unlock()
}

func (s *Service) recordNetworkResponse(event *network.EventResponseReceived, now string) {
	if event == nil || event.Response == nil {
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
}

func (s *Service) handlePausedEvent(event *fetch.EventRequestPaused) {
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

func (s *Service) handlePausedRequest(requestID fetch.RequestID, requestURL string) {
	policyCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	if !s.evaluateRequestPolicy(policyCtx, requestURL) {
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

func (s *Service) evaluateRequestPolicy(ctx context.Context, requestURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(requestURL))
	if err != nil {
		s.recordPolicyError(fmt.Sprintf("browser blocked request %s: parse url: %v", requestURL, err))
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case "about", "blob", "data":
		return true
	case "http", "https":
	default:
		s.recordPolicyError(fmt.Sprintf("browser blocked request %s: unsupported url scheme %q", requestURL, parsed.Scheme))
		return false
	}
	if _, err := s.cfg.Policy.Validate(ctx, requestURL); err != nil {
		s.recordPolicyError(fmt.Sprintf("browser blocked request %s: %v", requestURL, err))
		return false
	}
	return true
}

func (s *Service) recordPolicyError(message string) {
	s.mu.Lock()
	s.policyErrors = append(s.policyErrors, message)
	s.mu.Unlock()
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
