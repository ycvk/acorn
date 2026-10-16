package core

import (
	"errors"
	"strings"
	"testing"
)

func TestThreadFileNameKeepsTheLastElement(t *testing.T) {
	for in, want := range map[string]string{
		"红楼梦.epub":                        "红楼梦.epub",
		"../../etc/passwd":                "passwd",
		`C:\Users\me\notes.txt`:           "notes.txt",
		" report\x00\n.pdf ":              "report.pdf",
		strings.Repeat("长", 80) + ".epub": strings.Repeat("长", 65) + ".epub",
	} {
		got, err := ThreadFileName(in)
		if err != nil || got != want {
			t.Errorf("ThreadFileName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", " ", "..", "a/..", "\x01"} {
		if _, err := ThreadFileName(in); !errors.Is(err, ErrInvalidThreadFile) {
			t.Errorf("ThreadFileName(%q) err = %v, want ErrInvalidThreadFile", in, err)
		}
	}
}

func TestThreadFileMIMEPrefersContentForMedia(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	for _, tc := range []struct {
		name string
		head []byte
		want string
	}{
		{"photo.jpg", png, "image/png"},
		{"book.epub", []byte("PK\x03\x04"), "application/epub+zip"},
		{"notes.md", []byte("# 标题"), "text/markdown"},
		{"data", []byte("plain words"), "text/plain"},
		{"scan.PDF", []byte("%PDF-1.7"), "application/pdf"},
	} {
		if got := ThreadFileMIME(tc.name, tc.head); got != tc.want {
			t.Errorf("ThreadFileMIME(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	if !(ThreadFile{MIME: "image/png", Size: 10}).InlineImage() || (ThreadFile{MIME: "image/heic", Size: 10}).InlineImage() || (ThreadFile{MIME: "image/png", Size: MaxInlineImageBytes + 1}).InlineImage() {
		t.Fatal("InlineImage must take supported images up to the cap")
	}
}
