package wire

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/workstation"
)

const fileTestToken = "0123456789abcdef0123456789abcdef"

func (p pairedClient) upload(t *testing.T, threadID, name string, content []byte, wantStatus int) api.ThreadFileDTO {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/threads/"+threadID+"/files", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	var file api.ThreadFileDTO
	if wantStatus != http.StatusCreated {
		p.do(t, req, wantStatus, nil)
		return file
	}
	p.do(t, req, wantStatus, &file)
	return file
}

func (p pairedClient) postJSON(t *testing.T, path string, in any, wantStatus int, out any) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	p.do(t, req, wantStatus, out)
}

// TestFilesTravelBetweenOwnerAndAgent sends the agent an image and a text
// file with a message, has the agent send back a book from its workstation,
// and deletes the thread.
func TestFilesTravelBetweenOwnerAndAgent(t *testing.T) {
	home := t.TempDir()
	ws, err := workstation.NewServer(workstation.ServerConfig{Home: home, Token: fileTestToken})
	if err != nil {
		t.Fatal(err)
	}
	wsServer := httptest.NewServer(ws.Handler())
	defer wsServer.Close()
	book := []byte("PK\x03\x04 the story of the stone")
	if err := os.MkdirAll(filepath.Join(home, "books"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "books", "hlm.epub"), book, 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_send", "send_file", map[string]string{"path": "books/hlm.epub", "name": "红楼梦.epub"}),
		textReply("发给你了：红楼梦.epub"),
		textReply("看到了"),
		textReply("收到"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, fmt.Sprintf("workstation:\n  url: %s\n  token: %s\nfiles:\n  max_upload_mib: 1\n", wsServer.URL, fileTestToken))
	c, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	client := newPairedClient(t, c)
	ctx := context.Background()
	thread, err := c.threads.CreateThread(ctx, "书")
	if err != nil {
		t.Fatal(err)
	}

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR fake image")
	notes := []byte("第一回 甄士隐梦幻识通灵")
	photo := client.upload(t, thread.ID, "photo.png", png, http.StatusCreated)
	text := client.upload(t, thread.ID, "读书笔记.txt", notes, http.StatusCreated)
	if photo.MIMEType != "image/png" || text.Name != "读书笔记.txt" || text.MIMEType != "text/plain" || text.Origin != "owner" {
		t.Fatalf("uploaded = %+v %+v", photo, text)
	}
	client.upload(t, thread.ID, "big.bin", bytes.Repeat([]byte{1}, 1<<20+1), http.StatusRequestEntityTooLarge)
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "x", "file_ids": []string{"file_00000000000000000000000000000000"}}, http.StatusBadRequest, nil)

	var run api.RunDTO
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "找一本红楼梦给我，顺便看看这两个", "file_ids": []string{photo.ID, text.ID}}, http.StatusCreated, &run)
	first := waitNewRun(t, c, thread.ID, "")

	// The model sees the photo and where both files are on the workstation.
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if got := requestImageURLs(provider.request(0)); !slices.Equal(got, []string{dataURL}) {
		t.Fatalf("request images = %d, want the photo", len(got))
	}
	input := lastOwnerInput(provider.request(0))
	notesPath := filepath.Join(home, "uploads", text.ID, "读书笔记.txt")
	if !strings.Contains(input, "找一本红楼梦给我") || !strings.Contains(input, fmt.Sprintf("- 读书笔记.txt (text/plain, %d B): %s", len(notes), notesPath)) || !strings.Contains(input, fmt.Sprintf("- photo.png (image/png, %d B), shown with this message: ", len(png))) {
		t.Fatalf("model input = %q", input)
	}
	if copied, err := os.ReadFile(notesPath); err != nil || !bytes.Equal(copied, notes) {
		t.Fatalf("workstation copy = %q, %v", copied, err)
	}

	// The thread shows the owner's files on their message and the book under the reply.
	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if len(listed.Items) != 2 || len(listed.Items[0].Files) != 2 || len(listed.Items[1].Files) != 1 {
		t.Fatalf("messages = %+v", listed.Items)
	}
	sent := listed.Items[1].Files[0]
	if sent.Name != "红楼梦.epub" || sent.MIMEType != "application/epub+zip" || sent.Origin != "agent" || listed.Items[1].RunID != first.RunID {
		t.Fatalf("sent file = %+v", sent)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/files/"+sent.ID, nil)
	req.Header.Set("Authorization", "Bearer "+client.token)
	rec := httptest.NewRecorder()
	client.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), book) || rec.Header().Get("Content-Disposition") != "attachment; filename*=utf-8''%E7%BA%A2%E6%A5%BC%E6%A2%A6.epub" {
		t.Fatalf("download: status %d disposition %q", rec.Code, rec.Header().Get("Content-Disposition"))
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/other/files/"+sent.ID, nil), http.StatusNotFound, nil)

	// A message can send files without text, and the photo stays in view on later turns.
	again := client.upload(t, thread.ID, "page.txt", []byte("第二回"), http.StatusCreated)
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"file_ids": []string{again.ID}}, http.StatusCreated, &run)
	waitNewRun(t, c, thread.ID, first.RunID)
	if input := lastOwnerInput(provider.request(provider.requestCount() - 1)); !strings.HasPrefix(input, "[Files the owner sent with this message]\n- page.txt") {
		t.Fatalf("files-only input = %q", input)
	}
	if got := requestImageURLs(provider.request(provider.requestCount() - 1)); !slices.Equal(got, []string{dataURL}) {
		t.Fatalf("later request images = %d, want the photo", len(got))
	}

	// Deleting the thread deletes its files; the next run anywhere removes their workstation copies.
	client.do(t, httptest.NewRequest(http.MethodDelete, "/v1/threads/"+thread.ID, nil), http.StatusNoContent, nil)
	if entries, err := os.ReadDir(filepath.Join(cfg.Runtime.StorageDir, "files")); err != nil || len(entries) != 0 {
		t.Fatalf("stored files after delete = %v, %v", entries, err)
	}
	other, err := c.threads.CreateThread(ctx, "别的")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.runs.CreateRun(ctx, other.ID, "", "在吗", nil); err != nil {
		t.Fatal(err)
	}
	waitNewRun(t, c, other.ID, "")
	if entries, err := os.ReadDir(filepath.Join(home, "uploads")); err != nil || len(entries) != 0 {
		t.Fatalf("workstation uploads after delete = %v, %v", entries, err)
	}
	if ids, err := c.store.OwnerThreadFileIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("owner files after delete = %v, %v", ids, err)
	}
}

// TestFilesOfARunWithoutAReplyStayInTheThread has the agent send a file and
// then fail before it replies; the thread still shows the file after the
// owner's message.
func TestFilesOfARunWithoutAReplyStayInTheThread(t *testing.T) {
	home := t.TempDir()
	ws, err := workstation.NewServer(workstation.ServerConfig{Home: home, Token: fileTestToken})
	if err != nil {
		t.Fatal(err)
	}
	wsServer := httptest.NewServer(ws.Handler())
	defer wsServer.Close()
	if err := os.WriteFile(filepath.Join(home, "report.txt"), []byte("周报"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The model has nothing scripted after the tool call, so the run fails before replying.
	provider := &fakeOpenAI{replies: []string{
		toolCallChunk("call_send", "send_file", map[string]string{"path": "report.txt"}),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, fmt.Sprintf("workstation:\n  url: %s\n  token: %s\n", wsServer.URL, fileTestToken))
	c, err := NewContainer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(context.Background(), "周报")
	if err != nil {
		t.Fatal(err)
	}

	var run api.RunDTO
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "把周报发我"}, http.StatusCreated, &run)
	waitRunStatus(t, c, run.ID, "failed")

	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if len(listed.Items) != 2 || listed.Items[0].Role != "user" {
		t.Fatalf("messages = %+v", listed.Items)
	}
	reply := listed.Items[1]
	if reply.Role != "assistant" || reply.ID != run.ID || reply.RunID != run.ID || reply.Content.Text != "" || len(reply.Files) != 1 {
		t.Fatalf("message after the owner's = %+v", reply)
	}
	if sent := reply.Files[0]; sent.Name != "report.txt" || sent.Origin != "agent" || !reply.CreatedAt.Equal(sent.CreatedAt) {
		t.Fatalf("sent file = %+v", sent)
	}
}

// lastOwnerInput is the text of the last user message a request sends.
func lastOwnerInput(request map[string]any) string {
	messages := requestMessages(request)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i]["role"] == "user" {
			return chatContentText(messages[i]["content"])
		}
	}
	return ""
}
