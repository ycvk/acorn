package runtime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// UploadHost keeps copies of the files the owner sent on the workstation,
// where the agent works on them.
type UploadHost interface {
	Uploads(ctx context.Context) (string, []string, error)
	PutUpload(ctx context.Context, id, name string, content io.Reader, size int64) error
	DeleteUpload(ctx context.Context, id string) error
}

// ownerFiles are the files the owner sent in a run's thread and where their
// copies are on the workstation. dir is empty when there are none, with
// missing saying why.
type ownerFiles struct {
	byMessage map[int64][]core.ThreadFile
	dir       string
	missing   string
}

// loadOwnerFiles reads the files the owner sent in the thread and copies
// them to the workstation. Copies of files no thread holds any more are
// removed, so a deleted thread's files leave the workstation too. When the
// copies fail, the run goes on and the model is told why.
func loadOwnerFiles(ctx context.Context, files core.ThreadFiles, host UploadHost, sessionID string) (ownerFiles, error) {
	all, err := files.ThreadFiles(ctx, sessionID)
	if err != nil {
		return ownerFiles{}, fmt.Errorf("thread files: %w", err)
	}
	out := ownerFiles{byMessage: map[int64][]core.ThreadFile{}}
	var sent []core.ThreadFile
	for _, file := range all {
		if file.Origin == core.ThreadFileFromOwner && file.MessageID > 0 {
			out.byMessage[file.MessageID] = append(out.byMessage[file.MessageID], file)
			sent = append(sent, file)
		}
	}
	if host == nil {
		out.missing = "no workstation is configured"
		return out, nil
	}
	dir, err := copyOwnerFiles(ctx, files, host, sessionID, sent)
	if err != nil {
		slog.Warn("owner file copies failed", "thread", sessionID, "error", err)
		out.missing = err.Error()
		return out, nil
	}
	out.dir = dir
	return out, nil
}

func copyOwnerFiles(ctx context.Context, files core.ThreadFiles, host UploadHost, sessionID string, sent []core.ThreadFile) (string, error) {
	dir, present, err := host.Uploads(ctx)
	if err != nil {
		return "", err
	}
	live, err := files.OwnerThreadFileIDs(ctx)
	if err != nil {
		return "", err
	}
	keep := map[string]bool{}
	for _, id := range live {
		keep[id] = true
	}
	have := map[string]bool{}
	for _, id := range present {
		if !keep[id] {
			if err := host.DeleteUpload(ctx, id); err != nil {
				return "", err
			}
			continue
		}
		have[id] = true
	}
	for _, file := range sent {
		if have[file.ID] {
			continue
		}
		if err := putOwnerFile(ctx, files, host, sessionID, file); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func putOwnerFile(ctx context.Context, files core.ThreadFiles, host UploadHost, sessionID string, file core.ThreadFile) error {
	_, content, err := files.OpenThreadFile(ctx, sessionID, file.ID)
	if err != nil {
		return err
	}
	defer content.Close()
	return host.PutUpload(ctx, file.ID, file.Name, content, file.Size)
}

// ownerFileImages loads the images the model is shown for messages in
// history.
func ownerFileImages(ctx context.Context, files core.ThreadFiles, sessionID string, owner ownerFiles, messages []core.SessionMessageRecord) (map[int64][]*schema.UserInputImage, error) {
	images := map[int64][]*schema.UserInputImage{}
	for _, msg := range messages {
		for _, file := range owner.byMessage[msg.ID] {
			if !file.InlineImage() {
				continue
			}
			data, err := readThreadFile(ctx, files, sessionID, file.ID)
			if err != nil {
				return nil, fmt.Errorf("image %s for message %d: %w", file.ID, msg.ID, err)
			}
			images[msg.ID] = append(images[msg.ID], &schema.UserInputImage{Base64Data: base64.StdEncoding.EncodeToString(data), MIMEType: file.MIME})
		}
	}
	return images, nil
}

func readThreadFile(ctx context.Context, files core.ThreadFiles, sessionID, fileID string) ([]byte, error) {
	_, content, err := files.OpenThreadFile(ctx, sessionID, fileID)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(content)
	return data, errors.Join(readErr, content.Close())
}

// note lists the files the owner sent with a message for the model.
func (o ownerFiles) note(messageID int64) string {
	sent := o.byMessage[messageID]
	if len(sent) == 0 {
		return ""
	}
	lines := []string{"[Files the owner sent with this message]"}
	for _, file := range sent {
		line := fmt.Sprintf("- %s (%s, %s)", file.Name, file.MIME, humanBytes(file.Size))
		if file.InlineImage() {
			line += ", shown with this message"
		}
		if o.dir != "" {
			line += ": " + path.Join(o.dir, file.ID, file.Name)
		}
		lines = append(lines, line)
	}
	if o.dir == "" {
		lines = append(lines, "They are not on your workstation: "+o.missing)
	}
	return strings.Join(lines, "\n")
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
