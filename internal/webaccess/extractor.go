package webaccess

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"golang.org/x/net/html"
)

type ExtractionMode string

const (
	ExtractionModeAuto             ExtractionMode = "auto"
	ExtractionModeReadability      ExtractionMode = "readability"
	ExtractionModeFullPageMarkdown ExtractionMode = "full_page_markdown"
	ExtractionModeVisibleText      ExtractionMode = "visible_text"
)

type PageLink struct {
	Text string
	URL  string
}

type ExtractRequest struct {
	URL  string
	HTML []byte
	Mode ExtractionMode
}

type ExtractResult struct {
	URL             string
	Title           string
	Markdown        string
	Excerpt         string
	Byline          string
	SiteName        string
	PublishedTime   string
	Method          string
	Warning         string
	Links           []PageLink
	MarkdownBytes   int
	MarkdownPreview string
}

func ExtractHTML(req ExtractRequest) (ExtractResult, error) {
	mode := normalizeExtractionMode(req.Mode)
	pageURL, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return ExtractResult{}, fmt.Errorf("parse extraction url: %w", err)
	}
	if len(req.HTML) == 0 {
		return ExtractResult{}, errors.New("html content is required")
	}
	switch mode {
	case ExtractionModeAuto:
		result, err := extractReadability(req.HTML, pageURL)
		if err == nil {
			return withCommonPageMetadata(result, req.HTML, pageURL), nil
		}
		fallback, fallbackErr := extractFullPageMarkdown(req.HTML, pageURL)
		if fallbackErr != nil {
			return ExtractResult{}, fmt.Errorf("readability extraction failed: %v; full-page extraction failed: %w", err, fallbackErr)
		}
		fallback.Warning = "readability_not_suitable"
		return withCommonPageMetadata(fallback, req.HTML, pageURL), nil
	case ExtractionModeReadability:
		result, err := extractReadability(req.HTML, pageURL)
		if err != nil {
			return ExtractResult{}, err
		}
		return withCommonPageMetadata(result, req.HTML, pageURL), nil
	case ExtractionModeFullPageMarkdown:
		result, err := extractFullPageMarkdown(req.HTML, pageURL)
		if err != nil {
			return ExtractResult{}, err
		}
		return withCommonPageMetadata(result, req.HTML, pageURL), nil
	case ExtractionModeVisibleText:
		result, err := extractVisibleText(req.HTML, pageURL)
		if err != nil {
			return ExtractResult{}, err
		}
		return withCommonPageMetadata(result, req.HTML, pageURL), nil
	default:
		return ExtractResult{}, fmt.Errorf("unsupported extraction mode %q", req.Mode)
	}
}

func normalizeExtractionMode(mode ExtractionMode) ExtractionMode {
	switch strings.TrimSpace(string(mode)) {
	case "", string(ExtractionModeAuto):
		return ExtractionModeAuto
	case string(ExtractionModeReadability):
		return ExtractionModeReadability
	case string(ExtractionModeFullPageMarkdown):
		return ExtractionModeFullPageMarkdown
	case string(ExtractionModeVisibleText):
		return ExtractionModeVisibleText
	default:
		return mode
	}
}

func extractReadability(raw []byte, pageURL *url.URL) (ExtractResult, error) {
	article, err := readability.FromReader(bytes.NewReader(raw), pageURL)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("readability extraction: %w", err)
	}
	if article.Node == nil {
		return ExtractResult{}, errors.New("readability extraction produced no article node")
	}
	markdown, err := htmltomarkdown.ConvertNode(article.Node, converter.WithDomain(originFor(pageURL)))
	if err != nil {
		return ExtractResult{}, fmt.Errorf("convert readability html to markdown: %w", err)
	}
	trimmed := strings.TrimSpace(string(markdown))
	if trimmed == "" {
		return ExtractResult{}, errors.New("readability extraction produced empty markdown")
	}
	result := ExtractResult{
		URL:      pageURL.String(),
		Title:    strings.TrimSpace(article.Title()),
		Markdown: trimmed,
		Excerpt:  strings.TrimSpace(article.Excerpt()),
		Byline:   strings.TrimSpace(article.Byline()),
		SiteName: strings.TrimSpace(article.SiteName()),
		Method:   "readability_markdown",
	}
	if published, err := article.PublishedTime(); err == nil && !published.IsZero() {
		result.PublishedTime = published.UTC().Format(time.RFC3339)
	}
	return result, nil
}

func extractFullPageMarkdown(raw []byte, pageURL *url.URL) (ExtractResult, error) {
	markdown, err := htmltomarkdown.ConvertString(string(raw), converter.WithDomain(originFor(pageURL)))
	if err != nil {
		return ExtractResult{}, fmt.Errorf("convert full page html to markdown: %w", err)
	}
	trimmed := strings.TrimSpace(markdown)
	if trimmed == "" {
		return ExtractResult{}, errors.New("full-page extraction produced empty markdown")
	}
	return ExtractResult{
		URL:      pageURL.String(),
		Title:    extractHTMLTitle(raw),
		Markdown: trimmed,
		Method:   "full_page_markdown",
	}, nil
}

func extractVisibleText(raw []byte, pageURL *url.URL) (ExtractResult, error) {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return ExtractResult{}, fmt.Errorf("parse html for visible text: %w", err)
	}
	var parts []string
	collectVisibleText(doc, false, &parts)
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return ExtractResult{}, errors.New("visible-text extraction produced empty text")
	}
	return ExtractResult{
		URL:      pageURL.String(),
		Title:    extractHTMLTitle(raw),
		Markdown: text,
		Method:   "visible_text",
	}, nil
}

func withCommonPageMetadata(result ExtractResult, raw []byte, pageURL *url.URL) ExtractResult {
	if strings.TrimSpace(result.Title) == "" {
		result.Title = extractHTMLTitle(raw)
	}
	result.URL = pageURL.String()
	result.Links = extractHTMLLinks(raw, pageURL, 100)
	result.MarkdownBytes = len([]byte(result.Markdown))
	return result
}

func originFor(pageURL *url.URL) string {
	if pageURL == nil || pageURL.Scheme == "" || pageURL.Host == "" {
		return ""
	}
	return pageURL.Scheme + "://" + pageURL.Host
}

func extractHTMLTitle(raw []byte) string {
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	var walk func(*html.Node) string
	walk = func(node *html.Node) string {
		if node == nil {
			return ""
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "title") {
			return strings.TrimSpace(nodeText(node))
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if title := walk(child); title != "" {
				return title
			}
		}
		return ""
	}
	return walk(doc)
}

func extractHTMLLinks(raw []byte, pageURL *url.URL, limit int) []PageLink {
	if limit <= 0 {
		return nil
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	links := make([]PageLink, 0)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil || len(links) >= limit {
			return
		}
		if node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") {
			if href := attrValue(node, "href"); href != "" {
				resolved := resolveLink(pageURL, href)
				if resolved != "" {
					links = append(links, PageLink{
						Text: strings.TrimSpace(nodeText(node)),
						URL:  resolved,
					})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if len(links) >= limit {
				return
			}
		}
	}
	walk(doc)
	return links
}

func collectVisibleText(node *html.Node, suppressed bool, out *[]string) {
	if node == nil {
		return
	}
	if node.Type == html.ElementNode {
		switch strings.ToLower(node.Data) {
		case "script", "style", "noscript", "template":
			suppressed = true
		}
	}
	if node.Type == html.TextNode && !suppressed {
		text := strings.Join(strings.Fields(node.Data), " ")
		if text != "" {
			*out = append(*out, text)
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectVisibleText(child, suppressed, out)
	}
}

func nodeText(node *html.Node) string {
	var parts []string
	collectVisibleText(node, false, &parts)
	return strings.Join(parts, " ")
}

func attrValue(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func resolveLink(pageURL *url.URL, raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if pageURL != nil {
		parsed = pageURL.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}
