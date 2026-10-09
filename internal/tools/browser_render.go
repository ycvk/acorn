package tools

import (
	"context"

	"github.com/chromedp/chromedp"
)

// RenderHTML opens rawURL under the browser's URL policy and returns the
// document's HTML after scripts have run.
func (s *Service) RenderHTML(ctx context.Context, rawURL string) (string, error) {
	if _, err := s.Open(ctx, rawURL); err != nil {
		return "", err
	}
	browserCtx, err := s.ensureStarted(ctx)
	if err != nil {
		return "", err
	}
	actionCtx, cancel := s.actionContext(ctx, browserCtx)
	defer cancel()
	var html string
	if err := chromedp.Run(actionCtx, chromedp.OuterHTML("html", &html, chromedp.ByQuery)); err != nil {
		return "", err
	}
	return html, nil
}
