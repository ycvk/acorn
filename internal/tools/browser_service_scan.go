package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"

	"github.com/ycvk/acorn/internal/webaccess"
)

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
	pageURL, title, html, err := s.readScanPage(actionCtx)
	if err != nil {
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

func (s *Service) readScanPage(ctx context.Context) (pageURL, title, html string, err error) {
	err = chromedp.Run(ctx,
		chromedp.Location(&pageURL),
		chromedp.Title(&title),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)
	return pageURL, title, html, err
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
	pageURL, title, raw, err := s.evaluateSnapshot(actionCtx)
	if err != nil {
		return SnapshotResult{}, err
	}
	return s.buildSnapshotResult(pageURL, title, raw), nil
}

func (s *Service) evaluateSnapshot(ctx context.Context) (pageURL, title string, raw []snapshotElementRaw, err error) {
	err = chromedp.Run(ctx,
		chromedp.Location(&pageURL),
		chromedp.Title(&title),
		chromedp.Evaluate(snapshotScript(defaultElementLimit), &raw),
	)
	return pageURL, title, raw, err
}

func (s *Service) buildSnapshotResult(pageURL, title string, raw []snapshotElementRaw) SnapshotResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotGeneration++
	generation := s.snapshotGeneration
	s.snapshotRefs = make(map[string]snapshotRef, len(raw))
	elements := make([]SnapshotElement, 0, len(raw))
	for idx, item := range raw {
		selector := strings.TrimSpace(item.Selector)
		if selector == "" {
			continue
		}
		ref := fmt.Sprintf("e%d", idx+1)
		s.snapshotRefs[ref] = snapshotRef{Selector: selector, Generation: generation, URL: pageURL}
		elements = append(elements, snapshotElementFromRaw(ref, item, selector))
	}
	return SnapshotResult{URL: pageURL, Title: title, Generation: generation, Elements: elements}
}

func snapshotElementFromRaw(ref string, item snapshotElementRaw, selector string) SnapshotElement {
	return SnapshotElement{
		Ref:          ref,
		Role:         strings.TrimSpace(item.Role),
		Name:         strings.TrimSpace(item.Name),
		Selector:     selector,
		ValuePreview: strings.TrimSpace(item.ValuePreview),
		Enabled:      item.Enabled,
		Visible:      item.Visible,
	}
}
