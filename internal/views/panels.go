package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// PanelStore keeps the pinned views package triggers feed.
type PanelStore interface {
	FedPanels(ctx context.Context, source string) ([]core.ThreadView, error)
	FeedThreadView(ctx context.Context, viewID string, components, data json.RawMessage) (bool, error)
}

// Feeder replaces the data of pinned views with the view_data lines of the
// trigger they name as their source.
type Feeder struct {
	store   PanelStore
	sources Sources
}

func NewFeeder(store PanelStore, sources Sources) (*Feeder, error) {
	if store == nil || sources.Files == nil || sources.Attachments == nil || sources.Links == nil {
		return nil, errors.New("view feeder: store, files, attachments and link policy are required")
	}
	return &Feeder{store: store, sources: sources}, nil
}

// Feed replaces the data of each pinned view whose source is the trigger
// source with data, checked against the view's components as show_view
// checks a view. A view the data does not fit keeps its data and is named in
// the error; a view that changed or was unpinned meanwhile is left alone.
func (f *Feeder) Feed(ctx context.Context, source string, data map[string]any) error {
	panels, err := f.store.FedPanels(ctx, source)
	if err != nil {
		return err
	}
	var errs []error
	for _, view := range panels {
		var components []map[string]any
		if err := json.Unmarshal(view.Components, &components); err != nil {
			errs = append(errs, fmt.Errorf("view %s components: %w", view.ID, err))
			continue
		}
		checked, err := Check(ctx, view.Title, components, data, f.sources.For(view.SessionID))
		var problems *CheckError
		switch {
		case errors.As(err, &problems):
			errs = append(errs, fmt.Errorf("view_data does not fit the pinned view %q (%s), which keeps its data:\n%s", view.Title, view.ID, problems.List()))
			continue
		case err != nil:
			errs = append(errs, fmt.Errorf("check view_data for %s: %w", view.ID, err))
			continue
		}
		if _, err := f.store.FeedThreadView(ctx, view.ID, view.Components, checked.Data); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
