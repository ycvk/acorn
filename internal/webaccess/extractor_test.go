package webaccess

import (
	"strings"
	"testing"
)

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
	if len(result.Links) != 1 || result.Links[0].URL != "https://example.com/install" {
		t.Fatalf("links = %+v", result.Links)
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
