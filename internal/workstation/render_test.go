package workstation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBrowser writes a stand-in for chromium that prints its last argument
// (the URL) inside a page, or runs body when given.
func fakeBrowser(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chromium")
	if body == "" {
		body = `for last; do :; done; printf '<html><body>%s</body></html>' "$last"`
	}
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRenderReturnsTheBrowsersDOM(t *testing.T) {
	client, _ := newTestClientWithBrowser(t, fakeBrowser(t, ""))
	html, err := client.RenderHTML(t.Context(), "https://example.com/a?b=c")
	if err != nil {
		t.Fatal(err)
	}
	if html != "<html><body>https://example.com/a?b=c</body></html>" {
		t.Fatalf("html = %q", html)
	}
	if _, err := client.RenderHTML(t.Context(), "file:///etc/passwd"); err == nil || !strings.Contains(err.Error(), "http(s) URL") {
		t.Fatalf("file url err = %v", err)
	}
}

func TestRenderReportsBrowserFailures(t *testing.T) {
	client, _ := newTestClientWithBrowser(t, fakeBrowser(t, `echo "net::ERR_NAME_NOT_RESOLVED" >&2; exit 1`))
	if _, err := client.RenderHTML(t.Context(), "https://nowhere.invalid"); err == nil || !strings.Contains(err.Error(), "ERR_NAME_NOT_RESOLVED") {
		t.Fatalf("err = %v", err)
	}
	none, _ := newTestClient(t)
	if _, err := none.RenderHTML(t.Context(), "https://example.com"); err == nil || !strings.Contains(err.Error(), ErrNoBrowser.Error()) {
		t.Fatalf("no browser err = %v", err)
	}
}
