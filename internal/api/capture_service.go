package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ycvk/acorn/internal/knowledge"
)

var (
	ErrInvalidCapture  = errors.New("invalid capture")
	ErrCaptureTooLarge = errors.New("capture too large")
)

const (
	maxCaptureTextBytes  = 16 << 10
	maxCaptureImageBytes = 10 << 20
	// maxCaptureBodyBytes leaves room for the text fields and multipart framing.
	maxCaptureBodyBytes = maxCaptureImageBytes + 2<<20
	maxCaptureLinks     = 5
)

var captureLink = regexp.MustCompile(`https?://[^\s<>"]+`)

type captureVault interface {
	SaveAttachment(ctx context.Context, mime string, data []byte, message string) (string, error)
}

type captureThreads interface {
	CreateThread(ctx context.Context, title string) (*Thread, error)
}

type captureRuns interface {
	CreateCaptureRun(ctx context.Context, threadID, input string) (*Run, error)
}

// CaptureService turns something shared from the owner's phone into a run in
// a thread of its own.
type CaptureService struct {
	vault   captureVault
	threads captureThreads
	runs    captureRuns
}

func NewCaptureService(vault captureVault, threads captureThreads, runs captureRuns) *CaptureService {
	return &CaptureService{vault: vault, threads: threads, runs: runs}
}

// CaptureInput is one share. Text or Image is required.
type CaptureInput struct {
	Text    string
	Subject string
	Image   []byte
}

// CaptureAccepted is POST /v1/captures.
type CaptureAccepted struct {
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
}

// Capture stores an image in the knowledge base, opens a thread and starts
// the run that files the capture.
func (s *CaptureService) Capture(ctx context.Context, in CaptureInput) (CaptureAccepted, error) {
	if s == nil || s.vault == nil || s.threads == nil || s.runs == nil {
		return CaptureAccepted{}, errors.New("capture service is not initialized")
	}
	text, subject := strings.TrimSpace(in.Text), strings.TrimSpace(in.Subject)
	if text == "" && len(in.Image) == 0 {
		return CaptureAccepted{}, fmt.Errorf("%w: text or image is required", ErrInvalidCapture)
	}
	if len(text) > maxCaptureTextBytes {
		return CaptureAccepted{}, fmt.Errorf("%w: text is longer than %d bytes", ErrInvalidCapture, maxCaptureTextBytes)
	}
	if len(in.Image) > maxCaptureImageBytes {
		return CaptureAccepted{}, fmt.Errorf("%w: image is larger than 10 MiB", ErrCaptureTooLarge)
	}
	links := captureLink.FindAllString(text, maxCaptureLinks)
	var image string
	if len(in.Image) > 0 {
		mime := http.DetectContentType(in.Image)
		if !knowledge.SupportsAttachment(mime) {
			return CaptureAccepted{}, fmt.Errorf("%w: image type %s is not supported; send JPEG, PNG, WebP or GIF", ErrInvalidCapture, mime)
		}
		attachment, err := s.vault.SaveAttachment(ctx, mime, in.Image, "knowledge: capture image")
		if err != nil {
			return CaptureAccepted{}, err
		}
		image = fmt.Sprintf("%s (%s, %s)", attachment, mime, humanBytes(len(in.Image)))
	}
	thread, err := s.threads.CreateThread(ctx, captureTitle(subject, links, text, image != ""))
	if err != nil {
		return CaptureAccepted{}, err
	}
	run, err := s.runs.CreateCaptureRun(ctx, thread.ID, captureRunInput(subject, links, text, image))
	if err != nil {
		return CaptureAccepted{}, err
	}
	return CaptureAccepted{ThreadID: thread.ID, RunID: run.ID}, nil
}

func captureTitle(subject string, links []string, text string, hasImage bool) string {
	switch {
	case subject != "":
		return truncateRunes(subject, 80)
	case len(links) > 0:
		if u, err := url.Parse(links[0]); err == nil && u.Host != "" {
			return u.Host
		}
		return truncateRunes(links[0], 80)
	case hasImage && text == "":
		return "Shared image"
	default:
		return truncateRunes(compactWhitespace(text), 40)
	}
}

func captureRunInput(subject string, links []string, text, image string) string {
	lines := []string{"[capture] shared from the owner's phone"}
	if subject != "" {
		lines = append(lines, "Subject: "+subject)
	}
	for _, link := range links {
		lines = append(lines, "Link: "+link)
	}
	if rest := textWithoutLinks(text); rest != "" {
		lines = append(lines, "Text:", rest)
	}
	if image != "" {
		lines = append(lines, "Image: "+image)
	}
	return strings.Join(lines, "\n")
}

// textWithoutLinks is the shared text with links removed (they are listed on
// their own lines), spaces tidied and empty lines at either end dropped.
func textWithoutLinks(text string) string {
	lines := strings.Split(captureLink.ReplaceAllString(text, ""), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
