package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/views"
)

// ViewStore keeps the views show_view shows and update_view replaces, and
// records each on its run.
type ViewStore interface {
	SaveThreadView(ctx context.Context, in core.ThreadViewWrite) (core.ThreadView, error)
	ThreadView(ctx context.Context, sessionID, viewID string) (core.ThreadView, error)
	UpdateThreadView(ctx context.Context, in core.ThreadViewUpdate) (core.ThreadView, error)
	AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error)
}

// TriggerLister lists the triggers of the skills in use, whose view_data
// lines may feed a pinned view.
type TriggerLister interface {
	Triggers(ctx context.Context) ([]skills.PackageTrigger, error)
}

// ViewToolDeps are what show_view and update_view need: the store, what a
// view may point to, the triggers that may feed it, and the current run.
type ViewToolDeps struct {
	Store    ViewStore
	Sources  views.Sources
	Triggers TriggerLister
	Context  core.ToolCallContextBridge
}

type ShowViewInput struct {
	Title      string           `json:"title" jsonschema:"required" jsonschema_description:"One line naming what the view shows, at most 80 characters."`
	Components []map[string]any `json:"components" jsonschema:"required" jsonschema_description:"Components from the catalog in the views skill, flat: each has id and component, containers list their children's ids, and one has id root."`
	Data       map[string]any   `json:"data,omitempty" jsonschema_description:"JSON object that the components' path values point into."`
	Source     string           `json:"source,omitempty" jsonschema_description:"A trigger of a skill package in use, as skill/trigger, whose view_data lines replace the data while the owner keeps the view pinned to the now page. Leave out for a view only you update."`
}

type ShowViewOutput struct {
	ViewID string `json:"view_id"`
	Note   string `json:"note"`
}

type UpdateViewInput struct {
	ViewID     string           `json:"view_id" jsonschema:"required" jsonschema_description:"Id show_view returned for a view in this conversation."`
	Title      string           `json:"title,omitempty" jsonschema_description:"New title; leave out to keep the current one."`
	Components []map[string]any `json:"components,omitempty" jsonschema_description:"All the view's components again, flat; leave out to keep the current ones."`
	Data       map[string]any   `json:"data,omitempty" jsonschema_description:"The whole data object again; leave out to keep the current data."`
}

type UpdateViewOutput struct {
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
		source := strings.TrimSpace(input.Source)
		if source != "" {
			if err := checkTrigger(ctx, deps.Triggers, source); err != nil {
				return ShowViewOutput{}, fmt.Errorf("source: %w", err)
			}
		}
		checked, err := views.Check(ctx, input.Title, input.Components, input.Data, deps.Sources.For(sessionID))
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
			Source:     source,
		})
		if err != nil {
			return ShowViewOutput{}, err
		}
		if _, err := deps.Store.AppendEvent(ctx, runID, "view.shown", core.ViewShownData{View: core.ViewDataOf(view)}); err != nil {
			return ShowViewOutput{}, fmt.Errorf("append view.shown event: %w", err)
		}
		note := "The owner sees the view under your reply; the reply should say what it shows instead of repeating it."
		if source != "" {
			note += " Once the owner pins it to the now page, each view_data line " + source + " prints replaces its data."
		}
		return ShowViewOutput{ViewID: view.ID, Note: note}, nil
	})
}

func buildUpdateViewTool(deps ViewToolDeps) (einotool.BaseTool, error) {
	description := "Replace the title, components or data of a view shown earlier in this conversation, such as newer numbers in its table; what you leave out stays. The whole view is checked again, and the buttons and choices the owner already sent stay sent."
	return inferProgressTool("update_view", description, func(ctx context.Context, input UpdateViewInput, _ ToolProgressEmitter) (UpdateViewOutput, error) {
		sessionID, runID := strings.TrimSpace(deps.Context.CurrentSessionID(ctx)), strings.TrimSpace(deps.Context.CurrentRunID(ctx))
		if sessionID == "" || runID == "" {
			return UpdateViewOutput{}, errors.New("update_view needs the current conversation and run")
		}
		current, err := deps.Store.ThreadView(ctx, sessionID, strings.TrimSpace(input.ViewID))
		if err != nil {
			return UpdateViewOutput{}, err
		}
		title, components, data := current.Title, input.Components, input.Data
		if strings.TrimSpace(input.Title) != "" {
			title = input.Title
		}
		if components == nil {
			if err := json.Unmarshal(current.Components, &components); err != nil {
				return UpdateViewOutput{}, fmt.Errorf("stored view components: %w", err)
			}
		}
		if data == nil {
			if err := json.Unmarshal(current.Data, &data); err != nil {
				return UpdateViewOutput{}, fmt.Errorf("stored view data: %w", err)
			}
		}
		checked, err := views.Check(ctx, title, components, data, deps.Sources.For(sessionID))
		if err != nil {
			return UpdateViewOutput{}, err
		}
		updated, err := deps.Store.UpdateThreadView(ctx, core.ThreadViewUpdate{SessionID: sessionID, ViewID: current.ID, Title: checked.Title, Components: checked.Components, Data: checked.Data})
		if err != nil {
			return UpdateViewOutput{}, err
		}
		if _, err := deps.Store.AppendEvent(ctx, runID, "view.updated", core.ViewShownData{View: core.ViewDataOf(updated)}); err != nil {
			return UpdateViewOutput{}, fmt.Errorf("append view.updated event: %w", err)
		}
		return UpdateViewOutput{ViewID: updated.ID, Note: "The owner sees the view's new content where it was shown."}, nil
	})
}

// checkTrigger accepts id when it names a trigger of a skill in use, as
// skill/trigger.
func checkTrigger(ctx context.Context, triggers TriggerLister, id string) error {
	listed, err := triggers.Triggers(ctx)
	if err != nil {
		return fmt.Errorf("list triggers: %w", err)
	}
	ids := make([]string, 0, len(listed))
	for _, t := range listed {
		if t.ID() == id {
			return nil
		}
		ids = append(ids, t.ID())
	}
	if len(ids) == 0 {
		return fmt.Errorf("%q is not a trigger: no skill in use has triggers", id)
	}
	return fmt.Errorf("%q is not a trigger of a skill in use; the triggers are %s", id, strings.Join(ids, ", "))
}
