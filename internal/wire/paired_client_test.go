package wire

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
)

// pairedClient is the /v1 handler with a paired device's token.
type pairedClient struct {
	handler http.Handler
	token   string
}

func newPairedClient(t *testing.T, c *Container) pairedClient {
	t.Helper()
	ctx := context.Background()
	handler, err := c.Handler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("api handler: %v", err)
	}
	code, err := c.DeviceAuth().CreatePairingCode(ctx, time.Minute)
	if err != nil {
		t.Fatalf("pairing code: %v", err)
	}
	paired, err := c.DeviceAuth().PairDevice(ctx, api.PairDeviceInput{PairingCode: code.Code, DeviceName: "phone", Platform: "android"})
	if err != nil {
		t.Fatalf("pair device: %v", err)
	}
	return pairedClient{handler: handler, token: paired.AccessToken}
}

func (p pairedClient) do(t *testing.T, req *http.Request, wantStatus int, out any) {
	t.Helper()
	req.Header.Set("Authorization", "Bearer "+p.token)
	rec := httptest.NewRecorder()
	p.handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s: status %d body %s", req.Method, req.URL, rec.Code, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
	}
}

// requestImageURLs lists the image URLs a chat completions request sends.
func requestImageURLs(request map[string]any) []string {
	var urls []string
	for _, msg := range requestMessages(request) {
		parts, _ := msg["content"].([]any)
		for _, raw := range parts {
			part, _ := raw.(map[string]any)
			if image, ok := part["image_url"].(map[string]any); ok {
				urls = append(urls, image["url"].(string))
			}
		}
	}
	return urls
}

// newPrivateNetworkServer serves handler on this machine's first private
// IPv4 address. web_fetch never reaches loopback, so a page it fetches must
// not live on 127.0.0.1.
func newPrivateNetworkServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("interface addresses: %v", err)
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.To4() == nil || !ipNet.IP.IsPrivate() {
			continue
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(ipNet.IP.String(), "0"))
		if err != nil {
			continue
		}
		server := httptest.NewUnstartedServer(handler)
		server.Listener = listener
		server.Start()
		return server
	}
	t.Fatal("this test needs a private IPv4 address to serve a page on")
	return nil
}

// toolResult finds the tool message answering callID in a model request.
func toolResult(t *testing.T, request map[string]any, callID string) string {
	t.Helper()
	for _, msg := range requestMessages(request) {
		if msg["role"] == "tool" && msg["tool_call_id"] == callID {
			return chatContentText(msg["content"])
		}
	}
	t.Fatalf("no tool result for %s in request", callID)
	return ""
}
