package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Who put a file in a conversation.
const (
	ThreadFileFromOwner = "owner"
	ThreadFileFromAgent = "agent"
)

var (
	ErrThreadFileNotFound = errors.New("thread file not found")
	ErrThreadFileTooLarge = errors.New("thread file too large")
	ErrInvalidThreadFile  = errors.New("invalid thread file")
)

// ThreadFile is a file in a conversation: one the owner attached to a
// message, or one the agent sent the owner during a run. Its content never
// changes.
type ThreadFile struct {
	ID        string
	SessionID string
	// MessageID is the owner message a file was sent with; 0 until it is.
	MessageID int64
	// RunID is the run that sent an agent file.
	RunID     string
	Origin    string
	Name      string
	MIME      string
	Size      int64
	SHA256    string
	CreatedAt time.Time
}

// ThreadFileWrite is a file to store. Content is read to its end, and more
// than MaxBytes fails with ErrThreadFileTooLarge.
type ThreadFileWrite struct {
	SessionID string
	RunID     string
	Origin    string
	Name      string
	Content   io.Reader
	MaxBytes  int64
}

// ThreadFiles keeps the files of conversations. Deleting a thread deletes
// its files.
type ThreadFiles interface {
	SaveThreadFile(ctx context.Context, in ThreadFileWrite) (ThreadFile, error)
	// ThreadFiles lists a thread's files, oldest first.
	ThreadFiles(ctx context.Context, sessionID string) ([]ThreadFile, error)
	OpenThreadFile(ctx context.Context, sessionID, fileID string) (ThreadFile, io.ReadCloser, error)
	// AttachThreadFiles sends owner files not yet sent with messageID; it
	// fails unless every id names one.
	AttachThreadFiles(ctx context.Context, sessionID string, messageID int64, fileIDs []string) error
	// OwnerThreadFileIDs lists the owner files of every thread.
	OwnerThreadFileIDs(ctx context.Context) ([]string, error)
}

// MaxInlineImageBytes caps an image the model is shown directly.
const MaxInlineImageBytes = 10 << 20

// inlineImageTypes are the image types models take as input.
var inlineImageTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}

// InlineImage reports whether the model sees f as an image.
func (f ThreadFile) InlineImage() bool {
	return inlineImageTypes[f.MIME] && f.Size <= MaxInlineImageBytes
}

const maxThreadFileNameBytes = 200

// ThreadFileName is the name a file is kept under: the last element of name,
// without control characters or surrounding space, at most 200 bytes with its
// extension kept.
func ThreadFileName(name string) (string, error) {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, name))
	if name == "" || name == "." || name == ".." || name == "/" {
		return "", fmt.Errorf("%w: file name is required", ErrInvalidThreadFile)
	}
	if len(name) <= maxThreadFileNameBytes {
		return name, nil
	}
	ext := path.Ext(name)
	if len(ext) > 16 {
		ext = ""
	}
	stem := strings.TrimSuffix(name, ext)
	for len(stem)+len(ext) > maxThreadFileNameBytes {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem + ext, nil
}

// typesByExtension name what sniffing the content cannot tell apart, such as
// an EPUB from a zip or Markdown from plain text.
var typesByExtension = map[string]string{
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".json": "application/json",
	".html": "text/html", ".htm": "text/html", ".xml": "application/xml", ".svg": "image/svg+xml",
	".pdf": "application/pdf", ".epub": "application/epub+zip", ".mobi": "application/x-mobipocket-ebook",
	".azw3": "application/vnd.amazon.ebook", ".zip": "application/zip", ".gz": "application/gzip",
	".tar": "application/x-tar", ".7z": "application/x-7z-compressed", ".rar": "application/vnd.rar",
	".doc": "application/msword", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls": "application/vnd.ms-excel", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt": "application/vnd.ms-powerpoint", ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".heic": "image/heic", ".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav", ".mp4": "video/mp4", ".mov": "video/quicktime",
}

// ThreadFileMIME is the media type of a file named name whose content starts
// with head. Images, audio, video and PDF are recognized by their content,
// other files by their extension when it is known.
func ThreadFileMIME(name string, head []byte) string {
	sniffed, _, _ := strings.Cut(http.DetectContentType(head), ";")
	if strings.HasPrefix(sniffed, "image/") || strings.HasPrefix(sniffed, "audio/") || strings.HasPrefix(sniffed, "video/") || sniffed == "application/pdf" {
		return sniffed
	}
	if known, ok := typesByExtension[strings.ToLower(path.Ext(name))]; ok {
		return known
	}
	return sniffed
}
