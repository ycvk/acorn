package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
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

func (p pairedClient) capture(t *testing.T, fields map[string]string, image []byte) api.CaptureAccepted {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if image != nil {
		part, err := writer.CreateFormFile("image", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/captures", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	var accepted api.CaptureAccepted
	p.do(t, req, http.StatusAccepted, &accepted)
	return accepted
}

// installSeedSkill copies a repository seed skill into the config's skills dir.
func installSeedSkill(t *testing.T, root, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "skills", name, "SKILL.md"))
	if err != nil {
		t.Fatalf("read seed skill: %v", err)
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

const sharedPage = `<!doctype html><html><head><title>Tokio work stealing</title></head><body><article>
<h1>Tokio work stealing</h1>
<p>The Tokio scheduler balances tasks between worker threads by work stealing, so an idle worker takes queued tasks from a busy one.</p>
<p>Each worker keeps a local run queue and a LIFO slot for the most recently woken task.</p>
</article></body></html>`

func TestSharedLinkBecomesACommittedNote(t *testing.T) {
	page := newPrivateNetworkServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, sharedPage)
	}))
	defer page.Close()
	link := page.URL + "/blog/work-stealing"
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_skill", "skill", map[string]string{"skill": "skill.capture.to.note"}),
		toolCallChunk("call_search_tools", "tool_search", map[string]string{"query": "select:web_fetch"}),
		toolCallChunk("call_fetch", "web_fetch", map[string]string{"url": link}),
		toolCallChunk("call_search", "knowledge_search", map[string]string{"query": "Tokio work stealing"}),
		toolCallChunk("call_write", "knowledge_write", map[string]any{
			"path":   "inbox/tokio-work-stealing.md",
			"title":  "Tokio work stealing",
			"source": link,
			"tags":   []string{"rust", "async"},
			"body":   "Idle workers take queued tasks from busy ones.\n\n- Local run queue per worker\n- LIFO slot\n\nowner 附言: 周末读",
		}),
		textReply("记到了 inbox/tokio-work-stealing.md"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, "web_access:\n  allow_private_networks: true\n")
	installSeedSkill(t, cfg.Skills.Dir, "capture_to_note")
	c, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	client := newPairedClient(t, c)

	accepted := client.capture(t, map[string]string{"text": link + "\n\n周末读", "subject": "Tokio work stealing"}, nil)
	run := waitNewRun(t, c, accepted.ThreadID, "")
	if run.RunID != accepted.RunID {
		t.Fatalf("finished run %s, capture started %s", run.RunID, accepted.RunID)
	}

	if loaded := toolResult(t, provider.request(1), "call_skill"); !strings.Contains(loaded, "knowledge_write") {
		t.Fatalf("skill tool result = %q", loaded)
	}
	if fetched := toolResult(t, provider.request(3), "call_fetch"); !strings.Contains(fetched, "work stealing") {
		t.Fatalf("web_fetch result = %q", fetched)
	}
	first := requestMessages(provider.request(0))
	if input := first[len(first)-3]; !strings.HasPrefix(chatContentText(input["content"]), "[capture] shared from the owner's phone\nSubject: Tokio work stealing\nLink: "+link) {
		t.Fatalf("model input = %v", input)
	}

	history, err := c.threads.ListMessages(context.Background(), accepted.ThreadID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, msg := range history {
		roles = append(roles, msg.Role)
		if msg.Role == core.MessageRoleCapture && msg.RunID != accepted.RunID {
			t.Fatalf("capture message bound to %q, want %q", msg.RunID, accepted.RunID)
		}
	}
	if got := strings.Join(roles, ","); got != "capture,assistant" {
		t.Fatalf("thread roles = %s", got)
	}

	note, err := c.store.KnowledgeNote(context.Background(), "inbox/tokio-work-stealing.md")
	if err != nil || note.Source != link || !strings.Contains(note.Body, "owner 附言: 周末读") {
		t.Fatalf("note = %+v, %v", note, err)
	}
	if source, err := c.store.LoadMemorySource(context.Background(), core.KnowledgeSourceID(note.Path, note.Revision)); err != nil || source.RunID != accepted.RunID {
		t.Fatalf("note revision source = %+v, %v", source, err)
	}

	var found api.KnowledgeNoteListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/knowledge/notes?q=LIFO", nil), http.StatusOK, &found)
	if len(found.Notes) != 1 || found.Notes[0].Path != "inbox/tokio-work-stealing.md" {
		t.Fatalf("search = %+v", found)
	}
	wakes, err := c.store.CountWakesSince(context.Background(), time.Time{})
	if err != nil || wakes != 0 {
		t.Fatalf("captures counted as wakes: %d, %v", wakes, err)
	}
}

func TestSharedImageIsStoredBeforeTheRunAndServedToTheOwner(t *testing.T) {
	provider := &fakeOpenAI{replies: []string{textReply("收到图片")}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, "")
	c, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	client := newPairedClient(t, c)

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR fake image")
	accepted := client.capture(t, map[string]string{"text": "白板照片"}, png)
	waitNewRun(t, c, accepted.ThreadID, "")

	history, err := c.threads.ListMessages(context.Background(), accepted.ThreadID, 10)
	if err != nil || len(history) == 0 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	input := history[0].Content.Text
	start := strings.Index(input, "Image: ")
	if start < 0 {
		t.Fatalf("capture input lacks the image:\n%s", input)
	}
	attachment := strings.Fields(input[start+len("Image: "):])[0]
	stored, err := os.ReadFile(filepath.Join(cfg.Runtime.StorageDir, filepath.FromSlash(attachment)))
	if err != nil || !bytes.Equal(stored, png) {
		t.Fatalf("attachment %s: %v", attachment, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/knowledge/attachment?path="+url.QueryEscape(attachment), nil)
	req.Header.Set("Authorization", "Bearer "+client.token)
	rec := httptest.NewRecorder()
	client.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), png) {
		t.Fatalf("served attachment: status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// newPrivateNetworkServer serves handler on this machine's first private
// IPv4 address. web_fetch never reaches loopback, so the shared page must not
// live on 127.0.0.1.
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
	t.Fatal("this test needs a private IPv4 address to serve the shared page on")
	return nil
}

// toolResult finds the tool message answering callID in a model request.
func toolResult(t *testing.T, request map[string]any, callID string) string {
	t.Helper()
	for _, msg := range requestMessages(request) {
		if msg["role"] == "tool" && msg["tool_call_id"] == callID {
			content := chatContentText(msg["content"])
			return content
		}
	}
	t.Fatalf("no tool result for %s in request", callID)
	return ""
}
