package toolset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/webaccess"
)

const defaultBrowserPreviewBytes = 4000

type BrowserInput struct {
	Action      string `json:"action" jsonschema:"required,description=Browser action: status, open, tabs, scan, snapshot, click, fill, press, select, screenshot, console, network, or close."`
	Mode        string `json:"mode,omitempty" jsonschema:"description=Mode for console or network actions: start, list, or stop."`
	URL         string `json:"url,omitempty" jsonschema:"description=URL for open."`
	Ref         string `json:"ref,omitempty" jsonschema:"description=Element ref from snapshot. Mutually exclusive with selector."`
	Selector    string `json:"selector,omitempty" jsonschema:"description=CSS selector. Must match exactly one element for interaction actions."`
	Text        string `json:"text,omitempty" jsonschema:"description=Text for fill."`
	Key         string `json:"key,omitempty" jsonschema:"description=Key for press, such as Enter or Tab."`
	Value       string `json:"value,omitempty" jsonschema:"description=Value/label/text for select."`
	ExtractMode string `json:"extract_mode,omitempty" jsonschema:"description=Scan extraction mode: auto, readability, full_page_markdown, or visible_text."`
	FullPage    bool   `json:"full_page,omitempty" jsonschema:"description=Capture a full-page screenshot instead of the viewport."`
}

type BrowserScanOutput struct {
	URL                string               `json:"url,omitempty"`
	Title              string               `json:"title,omitempty"`
	ExtractionMethod   string               `json:"extraction_method,omitempty"`
	ExtractionWarning  string               `json:"extraction_warning,omitempty"`
	MarkdownPreview    string               `json:"markdown_preview,omitempty"`
	MarkdownTruncated  bool                 `json:"markdown_truncated,omitempty"`
	MarkdownArtifactID string               `json:"markdown_artifact_id,omitempty"`
	MarkdownArtifact   *ArtifactSummary     `json:"markdown_artifact,omitempty"`
	Links              []webaccess.PageLink `json:"links,omitempty"`
}

type BrowserScreenshotOutput struct {
	ArtifactID string          `json:"artifact_id,omitempty"`
	Artifact   ArtifactSummary `json:"artifact"`
	FullPage   bool            `json:"full_page,omitempty"`
}

type BrowserOutput struct {
	Action     string                   `json:"action"`
	Mode       string                   `json:"mode,omitempty"`
	Status     *Status                  `json:"status,omitempty"`
	Tabs       []Tab                    `json:"tabs,omitempty"`
	Navigation *NavigateResult          `json:"navigation,omitempty"`
	Scan       *BrowserScanOutput       `json:"scan,omitempty"`
	Snapshot   *SnapshotResult          `json:"snapshot,omitempty"`
	Screenshot *BrowserScreenshotOutput `json:"screenshot,omitempty"`
	Console    *ConsoleResult           `json:"console,omitempty"`
	Network    *NetworkResult           `json:"network,omitempty"`
	Message    string                   `json:"message,omitempty"`
}

func buildBrowserTool(service BrowserService, artifactService ArtifactService, bridge domain.ToolCallContextBridge) (einotool.BaseTool, error) {
	if service == nil {
		return nil, errors.New("browser service is required")
	}
	if artifactService == nil {
		return nil, errors.New("artifact service is required for browser")
	}
	if bridge == nil {
		return nil, errors.New("artifact context bridge is required for browser")
	}
	tool, err := inferProgressTool("browser", "Operate one run-scoped backend-owned Chromium session through fixed actions and persist explicit page/screenshot artifacts.", func(ctx context.Context, input BrowserInput, emit tools.ToolProgressEmitter) (BrowserOutput, error) {
		runID := strings.TrimSpace(bridge.CurrentRunID(ctx))
		if runID == "" {
			return BrowserOutput{}, errors.New("browser requires current run context")
		}
		callID := strings.TrimSpace(bridge.CurrentToolCallID(ctx))
		if callID == "" {
			return BrowserOutput{}, errors.New("browser requires current tool call context")
		}
		action := strings.ToLower(strings.TrimSpace(input.Action))
		output := BrowserOutput{Action: action}
		switch action {
		case "status":
			status, err := service.Status(ctx)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Status = &status
		case "open":
			navigation, err := service.Open(ctx, input.URL)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Navigation = &navigation
			if err := emitToolProgress(ctx, emit, fmt.Sprintf("opened %s", navigation.URL)); err != nil {
				return BrowserOutput{}, err
			}
		case "tabs":
			tabs, err := service.Tabs(ctx)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Tabs = tabs
		case "scan":
			scan, err := service.Scan(ctx, ScanRequest{
				ExtractMode: webaccess.ExtractionMode(strings.TrimSpace(input.ExtractMode)),
			})
			if err != nil {
				return BrowserOutput{}, err
			}
			record, err := artifactService.Write(ctx, store.ArtifactWriteRequest{
				RunID:               runID,
				SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
				SourceToolResultRef: "tool_result:" + strings.TrimSpace(runID) + ":" + strings.TrimSpace(callID),
				Kind:                store.ArtifactKindMarkdown,
				Title:               artifactTitle("browser scan", scan.Extracted.Title, scan.URL),
				MIMEType:            "text/markdown; charset=utf-8",
				Content:             []byte(scan.Extracted.Markdown),
			})
			if err != nil {
				return BrowserOutput{}, err
			}
			preview, truncated := previewString(scan.Extracted.Markdown, defaultBrowserPreviewBytes)
			output.Scan = &BrowserScanOutput{
				URL:                scan.URL,
				Title:              scan.Extracted.Title,
				ExtractionMethod:   scan.Extracted.Method,
				ExtractionWarning:  scan.Extracted.Warning,
				MarkdownPreview:    preview,
				MarkdownTruncated:  truncated,
				MarkdownArtifactID: record.ArtifactID,
				MarkdownArtifact:   new(artifactSummaryFromRecord(record)),
				Links:              append([]webaccess.PageLink(nil), scan.Extracted.Links...),
			}
			if err := emitToolProgress(ctx, emit, fmt.Sprintf("scanned %s into artifact %s", scan.URL, record.ArtifactID)); err != nil {
				return BrowserOutput{}, err
			}
		case "snapshot":
			snapshot, err := service.Snapshot(ctx)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Snapshot = &snapshot
		case "click":
			navigation, err := service.Click(ctx, input.Ref, input.Selector)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Navigation = &navigation
		case "fill":
			navigation, err := service.Fill(ctx, input.Ref, input.Selector, input.Text)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Navigation = &navigation
		case "press":
			navigation, err := service.Press(ctx, input.Ref, input.Selector, input.Key)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Navigation = &navigation
		case "select":
			navigation, err := service.Select(ctx, input.Ref, input.Selector, input.Value)
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Navigation = &navigation
		case "screenshot":
			image, err := service.Screenshot(ctx, input.FullPage)
			if err != nil {
				return BrowserOutput{}, err
			}
			record, err := artifactService.Write(ctx, store.ArtifactWriteRequest{
				RunID:               runID,
				SessionID:           strings.TrimSpace(bridge.CurrentSessionID(ctx)),
				SourceToolResultRef: "tool_result:" + strings.TrimSpace(runID) + ":" + strings.TrimSpace(callID),
				Kind:                store.ArtifactKindBinary,
				Title:               "browser screenshot",
				MIMEType:            "image/png",
				Content:             image,
			})
			if err != nil {
				return BrowserOutput{}, err
			}
			output.Screenshot = &BrowserScreenshotOutput{
				ArtifactID: record.ArtifactID,
				Artifact:   artifactSummaryFromRecord(record),
				FullPage:   input.FullPage,
			}
			if err := emitToolProgress(ctx, emit, fmt.Sprintf("captured browser screenshot %s", record.ArtifactID)); err != nil {
				return BrowserOutput{}, err
			}
		case "console":
			mode := normalizeBrowserWatchMode(input.Mode)
			output.Mode = mode
			switch mode {
			case "start":
				result, err := service.ConsoleStart(ctx)
				if err != nil {
					return BrowserOutput{}, err
				}
				output.Console = &result
			case "list":
				output.Console = new(service.ConsoleList())
			case "stop":
				output.Console = new(service.ConsoleStop())
			default:
				return BrowserOutput{}, fmt.Errorf("unsupported browser console mode %q", input.Mode)
			}
		case "network":
			mode := normalizeBrowserWatchMode(input.Mode)
			output.Mode = mode
			switch mode {
			case "start":
				result, err := service.NetworkStart(ctx)
				if err != nil {
					return BrowserOutput{}, err
				}
				output.Network = &result
			case "list":
				output.Network = new(service.NetworkList())
			case "stop":
				output.Network = new(service.NetworkStop())
			default:
				return BrowserOutput{}, fmt.Errorf("unsupported browser network mode %q", input.Mode)
			}
		case "close":
			if err := service.Close(); err != nil {
				return BrowserOutput{}, err
			}
			output.Message = "browser session closed"
		default:
			return BrowserOutput{}, fmt.Errorf("unsupported browser action %q", input.Action)
		}
		return output, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build browser tool: %w", err)
	}
	return tool, nil
}

func normalizeBrowserWatchMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "list":
		return "list"
	case "start":
		return "start"
	case "stop":
		return "stop"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}
