package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/webaccess"
)

func TestIntegrationBrowserOpenSnapshotScan(t *testing.T) {
	executablePath := os.Getenv("ACORN_BROWSER_TEST_CHROMIUM")
	if executablePath == "" {
		t.Skip("ACORN_BROWSER_TEST_CHROMIUM is not set")
	}
	testURL := os.Getenv("ACORN_BROWSER_TEST_URL")
	if testURL == "" {
		testURL = "https://example.com"
	}
	service, err := NewService(Config{
		ExecutablePath: executablePath,
		Headless:       true,
		Timeout:        20 * time.Second,
		Policy:         webaccess.URLPolicy{},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer func() {
		if err := service.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()

	if _, err := service.Open(context.Background(), testURL); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.URL == "" {
		t.Fatal("snapshot URL is empty")
	}
	scan, err := service.Scan(context.Background(), ScanRequest{ExtractMode: webaccess.ExtractionModeFullPageMarkdown})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if scan.Extracted.Markdown == "" {
		t.Fatal("scan markdown is empty")
	}
}
