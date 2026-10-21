package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// ThreadViewLister lists a thread's views with the actions the owner sent
// from them.
type ThreadViewLister interface {
	ThreadViews(ctx context.Context, sessionID string) ([]core.ThreadView, error)
}

// loadViewActionNotes tells the model, for each owner message a view sent,
// which view and action it came from and what the action carried. The
// message itself holds only what the owner saw: the button's label or the
// options they chose.
func loadViewActionNotes(ctx context.Context, views ThreadViewLister, sessionID string) (map[int64]string, error) {
	listed, err := views.ThreadViews(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("thread views: %w", err)
	}
	notes := map[int64]string{}
	for _, view := range listed {
		for _, action := range view.Actions {
			notes[action.MessageID] = viewActionNote(view, action)
		}
	}
	return notes, nil
}

func viewActionNote(view core.ThreadView, action core.ViewAction) string {
	lines := []string{fmt.Sprintf("[Sent from your view %q (%s) through %s: action %s]", view.Title, view.ID, action.ComponentID, action.Name)}
	if len(action.Scope) > 0 {
		lines = append(lines, fmt.Sprintf("List items: %v", action.Scope))
	}
	if len(action.Selected) > 0 {
		chosen := make([]string, len(action.Selected))
		for i, option := range action.Selected {
			chosen[i] = fmt.Sprintf("%s (%s)", option.ID, option.Label)
		}
		lines = append(lines, "Chosen: "+strings.Join(chosen, ", "))
	}
	if context := strings.TrimSpace(string(action.Context)); context != "" && context != "{}" {
		lines = append(lines, "Context: "+context)
	}
	return strings.Join(lines, "\n")
}
