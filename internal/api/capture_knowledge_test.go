package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

type captureFakes struct {
	attachments []string
	titles      []string
	inputs      []string
}

func (f *captureFakes) SaveAttachment(_ context.Context, mime string, data []byte) (string, error) {
	path := fmt.Sprintf("attachments/2026/10/img%d.png", len(f.attachments))
	f.attachments = append(f.attachments, mime)
	return path, nil
}

func (f *captureFakes) CreateThread(_ context.Context, title string) (*Thread, error) {
	f.titles = append(f.titles, title)
	return &Thread{ID: fmt.Sprintf("thread_%d", len(f.titles))}, nil
}

func (f *captureFakes) CreateCaptureRun(_ context.Context, threadID, input string) (*Run, error) {
	f.inputs = append(f.inputs, input)
	return &Run{ID: "run_for_" + threadID}, nil
}

type knowledgeFake struct {
	query, prefix string
	limit         int
}

var knowledgeFakeTime = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)

func (k *knowledgeFake) Search(_ context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	k.query, k.limit = query, limit
	return []core.KnowledgeHit{{Path: "inbox/tokio.md", Title: "Tokio", Snippet: "…runtime…", UpdatedAt: knowledgeFakeTime}}, nil
}

func (k *knowledgeFake) Recent(_ context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	k.prefix, k.limit = prefix, limit
	return nil, nil
}

func (k *knowledgeFake) Read(_ context.Context, path string) (core.KnowledgeNote, error) {
	if _, err := knowledge.CleanNotePath(path); err != nil {
		return core.KnowledgeNote{}, err
	}
	if path != "inbox/tokio.md" {
		return core.KnowledgeNote{}, fmt.Errorf("%w: %s", core.ErrKnowledgeNoteNotFound, path)
	}
	return core.KnowledgeNote{Path: path, Title: "Tokio", Source: "https://tokio.rs", CreatedAt: knowledgeFakeTime, UpdatedAt: knowledgeFakeTime, Body: "Async runtime."}, nil
}

const knowledgeFakeAttachment = "attachments/2026/10/0123456789abcdef.png"

func (k *knowledgeFake) ReadAttachment(_ context.Context, path string) (knowledge.Attachment, error) {
	switch {
	case path == knowledgeFakeAttachment:
		return knowledge.Attachment{MIME: "image/png", Data: pngHeader}, nil
	case strings.HasPrefix(path, "attachments/"):
		return knowledge.Attachment{}, fmt.Errorf("%w: %s", knowledge.ErrAttachmentNotFound, path)
	default:
		return knowledge.Attachment{}, fmt.Errorf("%w: %s", knowledge.ErrInvalidPath, path)
	}
}

func newCaptureKnowledgeRouter(t *testing.T) (*captureFakes, *knowledgeFake, http.Handler) {
	t.Helper()
	fakes, kf := &captureFakes{}, &knowledgeFake{}
	server := &Server{
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		captures:   NewCaptureService(fakes, fakes, fakes),
		knowledge:  NewKnowledgeService(kf),
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)
	return fakes, kf, router
}

func postCapture(t *testing.T, handler http.Handler, fields map[string]string, image []byte, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if image != nil {
		part, err := writer.CreateFormFile("image", "shared.png")
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
	if auth {
		req.Header.Set("Authorization", "Bearer test-token")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestCaptureStartsARunInANewThread(t *testing.T) {
	fakes, _, router := newCaptureKnowledgeRouter(t)
	rec := postCapture(t, router, map[string]string{"text": "Worth reading https://tokio.rs/blog/2026 later", "subject": "Tokio 2.0"}, nil, true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var accepted CaptureAccepted
	decodeClientTestJSON(t, rec, &accepted)
	if accepted.ThreadID != "thread_1" || accepted.RunID != "run_for_thread_1" {
		t.Fatalf("accepted = %+v", accepted)
	}
	want := "[capture] shared from the owner's phone\nSubject: Tokio 2.0\nLink: https://tokio.rs/blog/2026\nText:\nWorth reading later"
	if fakes.titles[0] != "Tokio 2.0" || fakes.inputs[0] != want {
		t.Fatalf("title %q input:\n%s", fakes.titles[0], fakes.inputs[0])
	}

	postCapture(t, router, map[string]string{"text": "https://example.com/a"}, nil, true)
	if fakes.titles[1] != "example.com" || strings.Contains(fakes.inputs[1], "Text:") {
		t.Fatalf("link-only capture: title %q input:\n%s", fakes.titles[1], fakes.inputs[1])
	}

	rec = postCapture(t, router, nil, pngHeader, true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("image status = %d body=%s", rec.Code, rec.Body.String())
	}
	if fakes.attachments[0] != "image/png" || fakes.titles[2] != "Shared image" {
		t.Fatalf("attachments %v titles %v", fakes.attachments, fakes.titles)
	}
	if !strings.Contains(fakes.inputs[2], "Image: attachments/2026/10/img0.png (image/png, 16 B)") {
		t.Fatalf("image input:\n%s", fakes.inputs[2])
	}
}

func TestCaptureRejectsBadInput(t *testing.T) {
	fakes, _, router := newCaptureKnowledgeRouter(t)
	for name, tc := range map[string]struct {
		fields map[string]string
		image  []byte
		status int
	}{
		"empty":      {map[string]string{"text": "  "}, nil, http.StatusBadRequest},
		"long text":  {map[string]string{"text": strings.Repeat("a", maxCaptureTextBytes+1)}, nil, http.StatusBadRequest},
		"pdf":        {nil, []byte("%PDF-1.7 fake"), http.StatusBadRequest},
		"huge image": {nil, append(append([]byte{}, pngHeader...), make([]byte, maxCaptureImageBytes)...), http.StatusRequestEntityTooLarge},
	} {
		if rec := postCapture(t, router, tc.fields, tc.image, true); rec.Code != tc.status {
			t.Errorf("%s: status = %d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	if rec := performClientRequest(router, http.MethodPost, "/v1/captures", `{"text":"x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("json body status = %d", rec.Code)
	}
	if rec := postCapture(t, router, map[string]string{"text": "x"}, nil, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", rec.Code)
	}
	if len(fakes.inputs) != 0 || len(fakes.attachments) != 0 {
		t.Fatalf("rejected captures started work: %v %v", fakes.inputs, fakes.attachments)
	}
}

func TestKnowledgeEndpoints(t *testing.T) {
	_, kf, router := newCaptureKnowledgeRouter(t)
	rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/notes?q=tokio&limit=5", "")
	if rec.Code != http.StatusOK || kf.query != "tokio" || kf.limit != 5 {
		t.Fatalf("search status %d query %q limit %d body=%s", rec.Code, kf.query, kf.limit, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tags":[]`) || !strings.Contains(rec.Body.String(), `"updated_at":"2026-10-03T01:00:00Z"`) {
		t.Fatalf("search body = %s", rec.Body.String())
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/knowledge/notes?prefix=inbox/", "")
	if rec.Code != http.StatusOK || kf.prefix != "inbox/" || kf.limit != defaultKnowledgeListLimit || rec.Body.String() != `{"notes":[]}` {
		t.Fatalf("list status %d prefix %q limit %d body=%s", rec.Code, kf.prefix, kf.limit, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/notes?limit=101", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("limit 101 status = %d", rec.Code)
	}

	rec = performClientRequest(router, http.MethodGet, "/v1/knowledge/note?path=inbox/tokio.md", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"body":"Async runtime."`) || !strings.Contains(rec.Body.String(), `"created_at":"2026-10-03T01:00:00Z"`) {
		t.Fatalf("read status %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/note?path=inbox/missing.md", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "knowledge_note_not_found") {
		t.Fatalf("missing status %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/note?path=../x.md", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("escape status = %d", rec.Code)
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodGet, "/v1/knowledge/notes", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}
}

func TestKnowledgeAttachmentServesImageBytes(t *testing.T) {
	_, _, router := newCaptureKnowledgeRouter(t)
	rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/attachment?path="+knowledgeFakeAttachment, "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), pngHeader) {
		t.Fatalf("status %d type %q body %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("cache-control = %q", got)
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/attachment?path=attachments/2026/10/ffffffffffffffff.png", ""); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "knowledge_attachment_not_found") {
		t.Fatalf("missing status %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/knowledge/attachment?path=acorn.db", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-attachment status = %d", rec.Code)
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodGet, "/v1/knowledge/attachment?path="+knowledgeFakeAttachment, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", rec.Code)
	}
}
