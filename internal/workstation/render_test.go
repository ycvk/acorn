package workstation

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
	html, err := client.RenderHTML(t.Context(), "https://example.com/a?b=c", false)
	if err != nil {
		t.Fatal(err)
	}
	if html != "<html><body>https://example.com/a?b=c</body></html>" {
		t.Fatalf("html = %q", html)
	}
	if _, err := client.RenderHTML(t.Context(), "file:///etc/passwd", false); err == nil || !strings.Contains(err.Error(), "http(s) URL") {
		t.Fatalf("file url err = %v", err)
	}
}

func TestRenderReportsBrowserFailures(t *testing.T) {
	client, _ := newTestClientWithBrowser(t, fakeBrowser(t, `echo "net::ERR_NAME_NOT_RESOLVED" >&2; exit 1`))
	if _, err := client.RenderHTML(t.Context(), "https://nowhere.invalid", false); err == nil || !strings.Contains(err.Error(), "ERR_NAME_NOT_RESOLVED") {
		t.Fatalf("err = %v", err)
	}
	none, _ := newTestClient(t)
	if _, err := none.RenderHTML(t.Context(), "https://example.com", false); err == nil || !strings.Contains(err.Error(), ErrNoBrowser.Error()) {
		t.Fatalf("no browser err = %v", err)
	}
}

type renderResolver map[string]string

func (r renderResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ip, ok := r[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
}

func TestRenderReachesTheNetworkOnlyThroughThePolicy(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello from " + r.Host))
	}))
	defer site.Close()
	siteAddr := site.Listener.Addr().String()
	// The stand-in browser insists on the flags that send loopback through
	// the proxy, loads the page through it, then two more addresses.
	browser := fakeBrowser(t, `for a; do case $a in --proxy-server=*) proxy=${a#--proxy-server=};; esac; last=$a; done
[[ " $* " == *" --proxy-bypass-list=<-loopback> "* && " $* " == *" --force-webrtc-ip-handling-policy=disable_non_proxied_udp "* ]] || exit 3
page=$(curl -sS --max-time 5 -x "$proxy" "$last") || exit 4
curl -s --max-time 5 -x "$proxy" -o /dev/null http://inside.test/
curl -s --max-time 5 -x "$proxy" -o /dev/null https://loop.test/
printf '<html><body>%s</body></html>' "$page"`)
	server, err := NewServer(ServerConfig{Home: t.TempDir(), Token: testToken, Chromium: browser, Supervisor: NewSupervisor(io.Discard)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	server.renderResolver = renderResolver{"page.test": "93.184.216.34", "inside.test": "10.0.0.5", "loop.test": "127.0.0.1"}
	var mu sync.Mutex
	var dialed []string
	server.renderDial = func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		var d net.Dialer
		return d.DialContext(ctx, network, siteAddr)
	}
	takeDialed := func() []string {
		mu.Lock()
		defer mu.Unlock()
		got := dialed
		dialed = nil
		return got
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	client, err := NewClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.RenderHTML(t.Context(), "http://page.test/", false)
	if err == nil || !strings.Contains(err.Error(), "the page reached for hosts the URL policy refuses: inside.test, loop.test") {
		t.Fatalf("err = %v, want inside.test and loop.test refused", err)
	}
	if got := takeDialed(); !slices.Equal(got, []string{"93.184.216.34:80"}) {
		t.Fatalf("dialed %q, want only the page's checked address", got)
	}

	_, err = client.RenderHTML(t.Context(), "http://page.test/", true)
	if err == nil || !strings.Contains(err.Error(), "refuses: loop.test") {
		t.Fatalf("with private networks allowed, err = %v, want loop.test refused", err)
	}
	if got := takeDialed(); !slices.Equal(got, []string{"93.184.216.34:80", "10.0.0.5:80"}) {
		t.Fatalf("dialed %q", got)
	}
}
