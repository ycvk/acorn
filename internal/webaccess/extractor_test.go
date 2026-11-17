package webaccess

import (
	"errors"
	"strings"
	"testing"
)

// javaScriptShell is a page that JavaScript builds in the browser, in the
// shape m.chinanews.com.cn and bazaarplusplus.com/download answered web_fetch
// with: a title, scripts, a noscript notice and an empty mount point.
const javaScriptShell = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>中国新闻网</title>
<script type="module" crossorigin src="/assets/index-4f2a.js"></script>
<link rel="stylesheet" href="/assets/index-9c1b.css"></head>
<body>
<noscript><strong>We're sorry but 中国新闻网 doesn't work properly without JavaScript enabled. Please enable it to continue.</strong></noscript>
<div id="app"></div>
<script>window.__INITIAL_STATE__={"id":10729021}</script>
</body>
</html>`

func TestExtractHTMLFindsNoTextInAPageJavaScriptBuilds(t *testing.T) {
	for _, mode := range []ExtractionMode{ExtractionModeAuto, ExtractionModeFullPageMarkdown} {
		_, err := ExtractHTML(ExtractRequest{URL: "https://m.chinanews.com.cn/wap/detail/chs/zw/10729021.shtml", Mode: mode, HTML: []byte(javaScriptShell)})
		if !errors.Is(err, ErrNoText) {
			t.Errorf("%s: error = %v, want ErrNoText", mode, err)
		}
	}
}

func TestExtractHTMLFullPageMarkdownResolvesLinks(t *testing.T) {
	result, err := ExtractHTML(ExtractRequest{
		URL:  "https://example.com/articles/one",
		Mode: ExtractionModeFullPageMarkdown,
		HTML: []byte(`<!doctype html>
<html>
<head><title>Docs</title></head>
<body>
  <h1>Docs</h1>
  <p>Read the docs.</p>
  <a href="/install">Install</a>
</body>
</html>`),
	})
	if err != nil {
		t.Fatalf("ExtractHTML: %v", err)
	}
	if result.Title != "Docs" {
		t.Fatalf("title = %q, want Docs", result.Title)
	}
	if result.Method != "full_page_markdown" {
		t.Fatalf("method = %q", result.Method)
	}
	if !strings.Contains(result.Markdown, "Read the docs") {
		t.Fatalf("markdown = %q", result.Markdown)
	}
	if !strings.Contains(result.Markdown, "[Install](https://example.com/install)") {
		t.Fatalf("markdown = %q, want the link resolved against the page", result.Markdown)
	}
}

func TestExtractHTMLRejectsUnsupportedMode(t *testing.T) {
	_, err := ExtractHTML(ExtractRequest{
		URL:  "https://example.com",
		Mode: ExtractionMode("custom"),
		HTML: []byte("<html><body>hello</body></html>"),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported extraction mode") {
		t.Fatalf("ExtractHTML error = %v", err)
	}
}
