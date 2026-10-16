package workstation

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOpenFileStreamsBinaryContent(t *testing.T) {
	client, home := newTestClient(t)
	data := []byte("PK\x03\x04\x00\xff binary")
	if err := os.WriteFile(filepath.Join(home, "book.epub"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	body, size, err := client.OpenFile(t.Context(), "book.epub")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || size != int64(len(data)) || !bytes.Equal(got, data) {
		t.Fatalf("open = %q size %d err %v", got, size, err)
	}
	var httpErr *HTTPError
	if _, _, err := client.OpenFile(t.Context(), "missing.pdf"); !errors.As(err, &httpErr) || httpErr.Status != http.StatusNotFound {
		t.Fatalf("missing file err = %v", err)
	}
	if _, _, err := client.OpenFile(t.Context(), home); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("directory err = %v", err)
	}
}

func TestUploadsArePutListedAndDeleted(t *testing.T) {
	client, home := newTestClient(t)
	const id = "file_0123456789abcdef0123456789abcdef"
	if err := client.PutUpload(t.Context(), id, "读书笔记.txt", strings.NewReader("第一回"), int64(len("第一回"))); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(home, "uploads", id, "读书笔记.txt")); err != nil || string(got) != "第一回" {
		t.Fatalf("copy = %q, %v", got, err)
	}
	// A repeated put replaces the copy.
	if err := client.PutUpload(t.Context(), id, "读书笔记.txt", strings.NewReader("第二回"), int64(len("第二回"))); err != nil {
		t.Fatal(err)
	}
	dir, ids, err := client.Uploads(t.Context())
	if err != nil || dir != filepath.Join(home, "uploads") || !slices.Equal(ids, []string{id}) {
		t.Fatalf("uploads = %s %v, %v", dir, ids, err)
	}
	for _, bad := range []struct{ id, name string }{{"../etc", "a.txt"}, {id, "../a.txt"}, {id, ".."}} {
		if err := client.PutUpload(t.Context(), bad.id, bad.name, strings.NewReader("x"), 1); err == nil {
			t.Fatalf("put %q %q must fail", bad.id, bad.name)
		}
	}
	if err := client.DeleteUpload(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, ids, err := client.Uploads(t.Context()); err != nil || len(ids) != 0 {
		t.Fatalf("uploads after delete = %v, %v", ids, err)
	}
}
