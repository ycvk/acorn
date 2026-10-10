package core

import (
	"slices"
	"testing"
)

func TestCaptureImagePathsReadsImageLines(t *testing.T) {
	input := "[capture] shared from the owner's phone\nText:\nImage: not a path\nImage: attachments/2026/10/0123456789abcdef.png (image/png, 1.2 MiB)"
	if got := CaptureImagePaths(input); !slices.Equal(got, []string{"attachments/2026/10/0123456789abcdef.png"}) {
		t.Fatalf("paths = %v", got)
	}
	if got := CaptureImagePaths("no image here"); got != nil {
		t.Fatalf("paths = %v", got)
	}
}
