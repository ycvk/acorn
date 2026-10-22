package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// NewsSectionsInput is the input of news_sections.
type NewsSectionsInput struct {
	Action     string  `json:"action" jsonschema:"required" jsonschema_description:"list, set (add a section or change one) or remove (an empty section)."`
	Name       string  `json:"name,omitempty" jsonschema_description:"set and remove: the section's name, e.g. 国内要闻."`
	Rename     string  `json:"rename,omitempty" jsonschema_description:"set: a new name for the section; its sources move with it."`
	Position   *int    `json:"position,omitempty" jsonschema_description:"set: order in the briefing, smallest first. A new section without one goes last."`
	Preference *string `json:"preference,omitempty" jsonschema_description:"set: what the owner wants from the section, a few numbered lines with the most important first, in the owner's words."`
}

// NewsSectionOutput is one section with its sources.
type NewsSectionOutput struct {
	Name       string        `json:"name"`
	Position   int           `json:"position"`
	Preference string        `json:"preference,omitempty"`
	Sources    []WatchOutput `json:"sources"`
}

// NewsSectionsOutput lists the sections after the action.
type NewsSectionsOutput struct {
	Sections []NewsSectionOutput `json:"sections"`
}

func buildNewsSectionsTool(deps WatchToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("news_sections",
		"Manage the sections of the morning briefing's news: list them with their sources, add one or change its order and preference, rename it, or remove one without sources. The briefing picks about ten items across sections by importance, source authority, credibility, fit with each section's preference, and what is new since the last briefing. Sources are news watches made with watch_create mode news.",
		func(ctx context.Context, input NewsSectionsInput, _ ToolProgressEmitter) (NewsSectionsOutput, error) {
			name := strings.TrimSpace(input.Name)
			now := deps.Clock()
			switch input.Action {
			case "list":
			case "set":
				if name == "" {
					return NewsSectionsOutput{}, errors.New("news_sections: set needs a name")
				}
				sections, err := deps.Store.ListNewsSections(ctx)
				if err != nil {
					return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w", err)
				}
				section, exists := core.NewsSection{Name: name, Position: nextPosition(sections)}, false
				for _, s := range sections {
					if s.Name == name {
						section, exists = s, true
					}
				}
				if !exists && strings.TrimSpace(input.Rename) != "" {
					return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w: %q", core.ErrNewsSectionNotFound, name)
				}
				if input.Position != nil {
					section.Position = *input.Position
				}
				if input.Preference != nil {
					section.Preference = strings.TrimSpace(*input.Preference)
				}
				section.UpdatedAt = now
				if _, err := deps.Store.SaveNewsSection(ctx, section); err != nil {
					return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w", err)
				}
				if rename := strings.TrimSpace(input.Rename); rename != "" && rename != name {
					if err := deps.Store.RenameNewsSection(ctx, name, rename, now); err != nil {
						return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w", err)
					}
				}
			case "remove":
				if name == "" {
					return NewsSectionsOutput{}, errors.New("news_sections: remove needs a name")
				}
				if err := deps.Store.DeleteNewsSection(ctx, name); err != nil {
					return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w; move or pause its sources first", err)
				}
			default:
				return NewsSectionsOutput{}, fmt.Errorf("news_sections: action %q must be list, set or remove", input.Action)
			}
			return listNewsSections(ctx, deps)
		})
}

func nextPosition(sections []core.NewsSection) int {
	next := 1
	for _, s := range sections {
		next = max(next, s.Position+1)
	}
	return next
}

func listNewsSections(ctx context.Context, deps WatchToolDeps) (NewsSectionsOutput, error) {
	sections, err := deps.Store.ListNewsSections(ctx)
	if err != nil {
		return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w", err)
	}
	watches, err := deps.Store.ListWatches(ctx)
	if err != nil {
		return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w", err)
	}
	out := NewsSectionsOutput{Sections: make([]NewsSectionOutput, 0, len(sections))}
	for _, s := range sections {
		section := NewsSectionOutput{Name: s.Name, Position: s.Position, Preference: s.Preference, Sources: []WatchOutput{}}
		for _, w := range watches {
			if w.Mode == core.WatchModeNews && w.Section == s.Name {
				section.Sources = append(section.Sources, watchOutput(w, 0, deps.Location))
			}
		}
		out.Sections = append(out.Sections, section)
	}
	return out, nil
}
