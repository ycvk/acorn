package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/views"
	"github.com/ycvk/acorn/internal/webaccess"
)

// ViewStore keeps the views show_view shows and records each on its run.
type ViewStore interface {
	SaveThreadView(ctx context.Context, in core.ThreadViewWrite) (core.ThreadView, error)
	AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error)
}

// ViewFiles lists a conversation's files, which a view's images may show.
type ViewFiles interface {
	ThreadFiles(ctx context.Context, sessionID string) ([]core.ThreadFile, error)
}

// LinkPolicy checks a web address before the owner is sent to it.
type LinkPolicy interface {
	Validate(ctx context.Context, raw string) (webaccess.ValidatedURL, error)
}

// ViewToolDeps are what show_view needs: the store, the conversation's files
// and the knowledge attachments a view's images may show, and the policy its
// links pass.
type ViewToolDeps struct {
	Store       ViewStore
	Files       ViewFiles
	Attachments core.AttachmentReader
	Links       LinkPolicy
	Context     core.ToolCallContextBridge
}

type ShowViewInput struct {
	Title      string           `json:"title" jsonschema:"required" jsonschema_description:"One line naming what the view shows, at most 80 characters."`
	Components []map[string]any `json:"components" jsonschema:"required" jsonschema_description:"Components from the catalog in the views skill, flat: each has id and component, containers list their children's ids, and one has id root."`
	Data       map[string]any   `json:"data,omitempty" jsonschema_description:"JSON object that the components' path values point into."`
}

type ShowViewOutput struct {
	ViewID string `json:"view_id"`
	Note   string `json:"note"`
}

func buildShowViewTool(deps ViewToolDeps) (einotool.BaseTool, error) {
	description := "Show the owner a view under your reply: tables, lists, charts, cards, links and images drawn by the app from the catalog in the views skill. Load the views skill before the first view. A view that fails its checks is not shown, and the result lists what to fix."
	return inferProgressTool("show_view", description, func(ctx context.Context, input ShowViewInput, _ ToolProgressEmitter) (ShowViewOutput, error) {
		sessionID, runID := strings.TrimSpace(deps.Context.CurrentSessionID(ctx)), strings.TrimSpace(deps.Context.CurrentRunID(ctx))
		if sessionID == "" || runID == "" {
			return ShowViewOutput{}, errors.New("show_view needs the current conversation and run")
		}
		checked, err := views.Check(ctx, input.Title, input.Components, input.Data, &viewReferences{deps: deps, sessionID: sessionID})
		if err != nil {
			return ShowViewOutput{}, err
		}
		view, err := deps.Store.SaveThreadView(ctx, core.ThreadViewWrite{
			SessionID:  sessionID,
			RunID:      runID,
			Title:      checked.Title,
			Catalog:    views.Catalog,
			Components: checked.Components,
			Data:       checked.Data,
		})
		if err != nil {
			return ShowViewOutput{}, err
		}
		if _, err := deps.Store.AppendEvent(ctx, runID, "view.shown", core.ViewShownData{View: core.ViewDataOf(view)}); err != nil {
			return ShowViewOutput{}, fmt.Errorf("append view.shown event: %w", err)
		}
		return ShowViewOutput{ViewID: view.ID, Note: "The owner sees the view under your reply; the reply should say what it shows instead of repeating it."}, nil
	})
}

// viewReferences checks a view's links with the link policy and its images
// against the conversation's files and the knowledge attachments.
type viewReferences struct {
	deps      ViewToolDeps
	sessionID string
	files     map[string]core.ThreadFile
}

func (r *viewReferences) Link(ctx context.Context, url string) error {
	_, err := r.deps.Links.Validate(ctx, url)
	return err
}

func (r *viewReferences) Image(ctx context.Context, ref string) error {
	var mime string
	switch {
	case strings.HasPrefix(ref, "file_"):
		if r.files == nil {
			files, err := r.deps.Files.ThreadFiles(ctx, r.sessionID)
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
		mime = file.MIME
	case strings.HasPrefix(ref, "attachments/"):
		attachment, err := r.deps.Attachments.ReadAttachment(ctx, ref)
		if err != nil {
			return fmt.Errorf("knowledge attachment %s cannot be read: %w", ref, err)
		}
		mime = attachment.MIME
	default:
		return fmt.Errorf("%q is neither a file id of this conversation (file_...) nor a knowledge attachment path (attachments/...)", ref)
	}
	if !strings.HasPrefix(mime, "image/") {
		return fmt.Errorf("%s is %s, not an image", ref, mime)
	}
	return nil
}
