package views

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
)

// FileLister lists a conversation's files, which a view's images may show.
type FileLister interface {
	ThreadFiles(ctx context.Context, sessionID string) ([]core.ThreadFile, error)
}

// LinkPolicy checks a web address before the owner is sent to it.
type LinkPolicy interface {
	Validate(ctx context.Context, raw string) (webaccess.ValidatedURL, error)
}

// Sources are what a view may point to outside itself: the files of its
// conversation and the web addresses the link policy allows.
type Sources struct {
	Files FileLister
	Links LinkPolicy
}

// For returns the References of a view in the conversation sessionID.
func (s Sources) For(sessionID string) References {
	return &conversationReferences{sources: s, sessionID: sessionID}
}

// conversationReferences checks a view's links with the link policy and its
// images against the conversation's files.
type conversationReferences struct {
	sources   Sources
	sessionID string
	files     map[string]core.ThreadFile
}

func (r *conversationReferences) Link(ctx context.Context, url string) error {
	_, err := r.sources.Links.Validate(ctx, url)
	return err
}

func (r *conversationReferences) Image(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, "file_") {
		return fmt.Errorf("%q is not a file id of this conversation (file_...)", ref)
	}
	if r.files == nil {
		files, err := r.sources.Files.ThreadFiles(ctx, r.sessionID)
		if err != nil {
			return err
		}
		r.files = make(map[string]core.ThreadFile, len(files))
		for _, file := range files {
			r.files[file.ID] = file
		}
	}
	file, ok := r.files[ref]
	if !ok {
		return fmt.Errorf("%s is not a file of this conversation", ref)
	}
	if !strings.HasPrefix(file.MIME, "image/") {
		return fmt.Errorf("%s is %s, not an image", ref, file.MIME)
	}
	return nil
}
