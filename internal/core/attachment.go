package core

import (
	"context"
	"strings"
)

// Attachment is an image stored under attachments/ in the storage dir.
type Attachment struct {
	MIME string
	Data []byte
}

// AttachmentReader reads an attachment by the path it was saved under.
type AttachmentReader interface {
	ReadAttachment(ctx context.Context, path string) (Attachment, error)
}

// ImageInputTokens approximates what a model bills for one image input; token
// budgets count each image as this many tokens.
const ImageInputTokens = 1600

// CaptureImagePrefix starts the line of a capture input that names its stored
// image: "Image: attachments/2026/10/<id>.png (image/png, 1.2 MiB)".
const CaptureImagePrefix = "Image: "

// CaptureImagePaths returns the attachment paths a capture input names.
func CaptureImagePaths(input string) []string {
	var paths []string
	for line := range strings.SplitSeq(input, "\n") {
		rest, ok := strings.CutPrefix(line, CaptureImagePrefix)
		if !ok {
			continue
		}
		path, _, _ := strings.Cut(rest, " ")
		if strings.HasPrefix(path, "attachments/") {
			paths = append(paths, path)
		}
	}
	return paths
}
