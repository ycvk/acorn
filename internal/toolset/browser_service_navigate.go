package toolset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

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
