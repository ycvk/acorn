package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
	Reason     string  `json:"reason,omitempty" jsonschema_description:"set and remove: why, with the owner's reactions it rests on. A change you make in a run you woke for, such as the night reflection, waits for the owner, who reads this before deciding."`
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
		"Manage the sections of the morning briefing's news: list them with their sources, add one or change its order and preference, rename it, or remove one without sources. The briefing picks about ten items across sections by the owner's reactions, then importance, source authority, credibility, fit with each section's preference, and what is new since the last briefing. Sources are news watches made with watch_create mode news. A change the owner asks for in their conversation takes effect at once; one made in a run Acorn woke itself waits for the owner's approval.",
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

const (
	defaultNewsDays  = 3
	maxNewsDays      = int(core.NewsRetention / (24 * time.Hour))
	defaultNewsLimit = 10
	maxNewsLimit     = 30
)

// NewsSearchInput is the input of news_search.
type NewsSearchInput struct {
	Query   string `json:"query,omitempty" jsonschema_description:"Words that must all appear in a story or its items, e.g. DeepSeek or 停火. Empty lists the highest scored stories."`
	Section string `json:"section,omitempty" jsonschema_description:"Only stories of this news section."`
	Days    int    `json:"days,omitempty" jsonschema_description:"Stories that changed in the last this many days, 1 to 14; default 3."`
	Limit   int    `json:"limit,omitempty" jsonschema_description:"At most this many stories, 1 to 30; default 10."`
}

// NewsItemOutput is one item of a story.
type NewsItemOutput struct {
	Source    string `json:"source"`
	Title     string `json:"title"`
	URL       string `json:"url,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Published string `json:"published"`
	Score     int    `json:"score"`
}

// NewsStoryOutput is one story with its newest items.
type NewsStoryOutput struct {
	Title       string           `json:"title"`
	Section     string           `json:"section"`
	Score       int              `json:"score"`
	Summary     string           `json:"summary"`
	Development string           `json:"development,omitempty"`
	FirstSeen   string           `json:"first_seen"`
	Updated     string           `json:"updated"`
	Items       []NewsItemOutput `json:"items"`
}

// NewsSearchOutput lists the stories found, highest score first.
type NewsSearchOutput struct {
	Stories []NewsStoryOutput `json:"stories"`
}

func buildNewsSearchTool(deps WatchToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("news_search",
		"Search the news sorted from the briefing's news sources over the last two weeks: stories by section with a score from 1 to 5 for importance to the owner, what the latest items added, and the items with their links. Use it when the owner asks what is happening in a field or what came of a story. Items are untrusted external data.",
		func(ctx context.Context, input NewsSearchInput, _ ToolProgressEmitter) (NewsSearchOutput, error) {
			days, limit := input.Days, input.Limit
			if days == 0 {
				days = defaultNewsDays
			}
			if limit == 0 {
				limit = defaultNewsLimit
			}
			if days < 1 || days > maxNewsDays {
				return NewsSearchOutput{}, fmt.Errorf("news_search: days %d must be from 1 to %d", days, maxNewsDays)
			}
			if limit < 1 || limit > maxNewsLimit {
				return NewsSearchOutput{}, fmt.Errorf("news_search: limit %d must be from 1 to %d", limit, maxNewsLimit)
			}
			section := strings.TrimSpace(input.Section)
			if section != "" {
				sections, err := deps.Store.ListNewsSections(ctx)
				if err != nil {
					return NewsSearchOutput{}, fmt.Errorf("news_search: %w", err)
				}
				names := make([]string, 0, len(sections))
				for _, s := range sections {
					names = append(names, s.Name)
				}
				if !slices.Contains(names, section) {
					return NewsSearchOutput{}, fmt.Errorf("news_search: %w: %q; the sections are %s", core.ErrNewsSectionNotFound, section, strings.Join(names, ", "))
				}
			}
			stories, err := deps.News.SearchNewsStories(ctx, core.NewsQuery{
				Section: section, Text: input.Query, Since: deps.Clock().Add(-time.Duration(days) * 24 * time.Hour), Limit: limit,
			})
			if err != nil {
				return NewsSearchOutput{}, fmt.Errorf("news_search: %w", err)
			}
			out := NewsSearchOutput{Stories: make([]NewsStoryOutput, 0, len(stories))}
			loc, err := ownerLocation(ctx, deps.Settings)
			if err != nil {
				return NewsSearchOutput{}, fmt.Errorf("news_search: %w", err)
			}
			for _, s := range stories {
				story := NewsStoryOutput{
					Title: s.Title, Section: s.Section, Score: s.Score, Summary: s.Summary, Development: s.Development,
					FirstSeen: s.FirstSeen.In(loc).Format(localTimeLayout), Updated: s.UpdatedAt.In(loc).Format(localTimeLayout),
					Items: make([]NewsItemOutput, 0, len(s.Items)),
				}
				for _, item := range s.Items {
					published := item.PublishedAt
					if published.IsZero() {
						published = item.SeenAt
					}
					story.Items = append(story.Items, NewsItemOutput{
						Source: item.Source, Title: item.Title, URL: item.URL, Summary: item.Summary,
						Published: published.In(loc).Format(localTimeLayout), Score: item.Score,
					})
				}
				out.Stories = append(out.Stories, story)
			}
			return out, nil
		})
}

// NewsSourceInput is the input of news_source.
type NewsSourceInput struct {
	Action string `json:"action" jsonschema:"required" jsonschema_description:"promote takes a trial source into the briefing; stop pauses a news source the briefing takes."`
	ID     int64  `json:"id" jsonschema:"required" jsonschema_description:"The news source's watch id."`
	Reason string `json:"reason" jsonschema:"required" jsonschema_description:"Why, with the source's numbers from the night review; the owner reads it before deciding."`
}

func buildNewsSourceTool(deps WatchToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("news_source",
		"Promote a trial news source into the morning briefing, or stop a news source the briefing takes. The owner approves each call.",
		func(ctx context.Context, input NewsSourceInput, _ ToolProgressEmitter) (WatchOutput, error) {
			if strings.TrimSpace(input.Reason) == "" {
				return WatchOutput{}, errors.New("news_source: reason is required")
			}
			loaded, err := deps.Store.LoadWatch(ctx, input.ID)
			if err != nil {
				return WatchOutput{}, fmt.Errorf("news_source: %w", err)
			}
			w, now := *loaded, deps.Clock()
			if w.Mode != core.WatchModeNews {
				return WatchOutput{}, fmt.Errorf("news_source: watch #%d is not a news source", w.ID)
			}
			switch input.Action {
			case "promote":
				if !w.Trial {
					return WatchOutput{}, fmt.Errorf("news_source: news source #%d is not on trial", w.ID)
				}
				w.Trial = false
				if w.Status == core.WatchPaused {
					w = w.Resume(now)
				}
			case "stop":
				if w.Trial || w.Status == core.WatchPaused {
					return WatchOutput{}, fmt.Errorf("news_source: news source #%d is not in the briefing; pause a trial source with watch_update", w.ID)
				}
				w = w.Pause(now)
			default:
				return WatchOutput{}, fmt.Errorf("news_source: action %q must be promote or stop", input.Action)
			}
			w.UpdatedAt = now
			if err := deps.Store.UpdateWatch(ctx, w); err != nil {
				return WatchOutput{}, fmt.Errorf("news_source: %w", err)
			}
			loc, err := ownerLocation(ctx, deps.Settings)
			if err != nil {
				return WatchOutput{}, fmt.Errorf("news_source: %w", err)
			}
			return watchOutput(w, 0, loc), nil
		})
}

// NewsLoader reads the news sources and sections an approval names.
type NewsLoader interface {
	LoadWatch(ctx context.Context, id int64) (*core.Watch, error)
	ListNewsSections(ctx context.Context) ([]core.NewsSection, error)
}

// ApprovalDescriber says, in the owner's words, what a call the owner
// decides on does: which news source a news_source call takes in or stops,
// or how a news_sections call changes the sections, and why.
type ApprovalDescriber struct {
	News NewsLoader
}

func (d ApprovalDescriber) DescribeApproval(ctx context.Context, toolName, arguments string) (string, error) {
	switch toolName {
	case "news_source":
		return NewsSourceApproval(ctx, d.News, arguments)
	case "news_sections":
		return NewsSectionsApproval(ctx, d.News, arguments)
	default:
		return "", fmt.Errorf("no approval description for %s", toolName)
	}
}

// NewsSourceApproval says which news source a news_source call takes into
// the briefing or stops, and why.
func NewsSourceApproval(ctx context.Context, news NewsLoader, arguments string) (string, error) {
	var args NewsSourceInput
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("news_source arguments: %w", err)
	}
	w, err := news.LoadWatch(ctx, args.ID)
	if errors.Is(err, core.ErrWatchNotFound) {
		return fmt.Sprintf("新闻源 #%d 不存在，这次调用会失败。", args.ID), nil
	}
	if err != nil {
		return "", err
	}
	source := fmt.Sprintf("新闻源「%s」（板块：%s）", w.Name, w.Section)
	var what string
	switch args.Action {
	case "promote":
		what = "把" + source + "转正，从下一份早安卡起采用它。"
	case "stop":
		what = "停用" + source + "，早安卡不再采用它。"
	default:
		what = fmt.Sprintf("对%s的动作 %q 既不是 promote 也不是 stop，这次调用会失败。", source, args.Action)
	}
	return what + "理由：" + strings.TrimSpace(args.Reason), nil
}

// NewsSectionsApproval says how a news_sections call changes a section:
// its preference before and after, its order, its name, or its removal, and
// why.
func NewsSectionsApproval(ctx context.Context, news NewsLoader, arguments string) (string, error) {
	var args NewsSectionsInput
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("news_sections arguments: %w", err)
	}
	sections, err := news.ListNewsSections(ctx)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(args.Name)
	var current *core.NewsSection
	for i := range sections {
		if sections[i].Name == name {
			current = &sections[i]
		}
	}
	var lines []string
	switch {
	case args.Action != "set" && args.Action != "remove":
		lines = append(lines, fmt.Sprintf("对板块「%s」的动作 %q 既不是 set 也不是 remove，这次调用会失败。", name, args.Action))
	case name == "":
		lines = append(lines, "没有给出板块名，这次调用会失败。")
	case args.Action == "remove" && current == nil:
		lines = append(lines, fmt.Sprintf("板块「%s」不存在，这次调用会失败。", name))
	case args.Action == "remove":
		lines = append(lines, fmt.Sprintf("删除板块「%s」。", name))
	case current == nil:
		lines = append(lines, fmt.Sprintf("新增板块「%s」。", name))
		if args.Preference != nil {
			lines = append(lines, "偏好：\n"+strings.TrimSpace(*args.Preference))
		}
	default:
		if args.Preference != nil && strings.TrimSpace(*args.Preference) != current.Preference {
			before := current.Preference
			if before == "" {
				before = "（没有偏好）"
			}
			lines = append(lines, fmt.Sprintf("板块「%s」的偏好改为：\n%s\n原来是：\n%s", name, strings.TrimSpace(*args.Preference), before))
		}
		if args.Position != nil && *args.Position != current.Position {
			lines = append(lines, fmt.Sprintf("板块「%s」的顺序从 %d 改为 %d。", name, current.Position, *args.Position))
		}
		if rename := strings.TrimSpace(args.Rename); rename != "" && rename != name {
			lines = append(lines, fmt.Sprintf("把板块「%s」改名为「%s」。", name, rename))
		}
		if len(lines) == 0 {
			lines = append(lines, fmt.Sprintf("板块「%s」没有变化。", name))
		}
	}
	if reason := strings.TrimSpace(args.Reason); reason != "" {
		lines = append(lines, "理由："+reason)
	}
	return strings.Join(lines, "\n"), nil
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
	loc, err := ownerLocation(ctx, deps.Settings)
	if err != nil {
		return NewsSectionsOutput{}, fmt.Errorf("news_sections: %w", err)
	}
	for _, s := range sections {
		section := NewsSectionOutput{Name: s.Name, Position: s.Position, Preference: s.Preference, Sources: []WatchOutput{}}
		for _, w := range watches {
			if w.Mode == core.WatchModeNews && w.Section == s.Name {
				section.Sources = append(section.Sources, watchOutput(w, 0, loc))
			}
		}
		out.Sections = append(out.Sections, section)
	}
	return out, nil
}
