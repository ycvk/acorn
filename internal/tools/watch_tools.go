package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/watch"
)

// WatchChecker validates and fetches watches for the watch tools.
type WatchChecker interface {
	Validate(kind core.WatchKind, target, selector string) error
	Fetch(ctx context.Context, w core.Watch) (watch.Fetched, error)
	Apply(ctx context.Context, w core.Watch, fetched watch.Fetched) (watch.Result, error)
}

// WatchToolDeps are what the watch tools need.
type WatchToolDeps struct {
	Store    core.WatchStore
	Checker  WatchChecker
	Context  core.ToolCallContextBridge
	Clock    func() time.Time
	Location *time.Location
}

const defaultWatchEvery = time.Hour

// WatchCreateInput is the input of watch_create.
type WatchCreateInput struct {
	Name     string `json:"name" jsonschema_description:"Short name, e.g. Go releases or phone price."`
	Kind     string `json:"kind" jsonschema_description:"rss (feed URL or rsshub:/route), github (target owner/repo), web (page URL) or web_rendered (page that needs JavaScript)."`
	Target   string `json:"target" jsonschema_description:"Feed URL, rsshub:/route, owner/repo, or page URL."`
	Selector string `json:"selector,omitempty" jsonschema_description:"web and web_rendered: CSS selector of the part to follow, e.g. .price; empty follows the main text. github: releases (default) or issues."`
	Mode     string `json:"mode,omitempty" jsonschema_description:"digest (default) keeps new items for the morning briefing; immediate wakes you as soon as something new appears."`
	Every    string `json:"every,omitempty" jsonschema_description:"How often to check, e.g. 30m, 6h. Default 1h, minimum 15m."`
}

// WatchUpdateInput is the input of watch_update.
type WatchUpdateInput struct {
	ID       int64  `json:"id" jsonschema_description:"Watch id from watch_list."`
	Name     string `json:"name,omitempty" jsonschema_description:"New name."`
	Selector string `json:"selector,omitempty" jsonschema_description:"New selector; the watch takes a new baseline."`
	Mode     string `json:"mode,omitempty" jsonschema_description:"digest or immediate."`
	Every    string `json:"every,omitempty" jsonschema_description:"New check interval, minimum 15m."`
	Status   string `json:"status,omitempty" jsonschema_description:"active to resume, paused to stop checking."`
}

// WatchOutput describes one watch.
type WatchOutput struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Selector    string `json:"selector,omitempty"`
	Mode        string `json:"mode"`
	Every       string `json:"every"`
	Status      string `json:"status"`
	NextCheck   string `json:"next_check"`
	LastChecked string `json:"last_checked,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	Pending     int    `json:"pending_for_briefing"`
}

// WatchCreateOutput is a new watch and what its first check found.
type WatchCreateOutput struct {
	WatchOutput
	Baseline int      `json:"baseline_items"`
	Sample   []string `json:"sample,omitempty"`
}

// WatchListOutput lists every watch.
type WatchListOutput struct {
	Watches []WatchOutput `json:"watches"`
}

func buildWatchCreateTool(deps WatchToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("watch_create",
		"Follow a source for the owner: a feed, a GitHub repository, or part of a web page. The first check runs now and becomes the baseline; later checks report only what is new.",
		func(ctx context.Context, input WatchCreateInput, _ ToolProgressEmitter) (WatchCreateOutput, error) {
			name, target := strings.TrimSpace(input.Name), strings.TrimSpace(input.Target)
			if name == "" || target == "" {
				return WatchCreateOutput{}, errors.New("watch_create: name and target are required")
			}
			kind := core.WatchKind(strings.TrimSpace(input.Kind))
			selector := strings.TrimSpace(input.Selector)
			if kind == core.WatchGitHub && selector == "" {
				selector = "releases"
			}
			mode, err := watchMode(input.Mode, core.WatchModeDigest)
			if err != nil {
				return WatchCreateOutput{}, fmt.Errorf("watch_create: %w", err)
			}
			every, err := watchEvery(input.Every, defaultWatchEvery)
			if err != nil {
				return WatchCreateOutput{}, fmt.Errorf("watch_create: %w", err)
			}
			if err := deps.Checker.Validate(kind, target, selector); err != nil {
				return WatchCreateOutput{}, fmt.Errorf("watch_create: %w", err)
			}
			now := deps.Clock()
			w := core.Watch{
				Name: name, Kind: kind, Target: target, Selector: selector, Mode: mode, Interval: every,
				Status: core.WatchActive, SessionID: deps.Context.CurrentSessionID(ctx), NextCheckAt: now, CreatedAt: now,
			}
			fetched, err := deps.Checker.Fetch(ctx, w)
			if err != nil {
				return WatchCreateOutput{}, fmt.Errorf("watch_create: the first check failed, so the watch was not created: %w", err)
			}
			if w, err = deps.Store.AddWatch(ctx, w); err != nil {
				return WatchCreateOutput{}, fmt.Errorf("watch_create: %w", err)
			}
			result, err := deps.Checker.Apply(ctx, w, fetched)
			if err != nil {
				return WatchCreateOutput{}, fmt.Errorf("watch_create: %w", err)
			}
			return WatchCreateOutput{
				WatchOutput: watchOutput(result.Watch, 0, deps.Location),
				Baseline:    result.Baseline,
				Sample:      fetchedSample(fetched),
			}, nil
		})
}

func buildWatchUpdateTool(deps WatchToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("watch_update",
		"Change a watch: rename it, change what part of the page it follows, switch between digest and immediate, change how often it is checked, or pause and resume it.",
		func(ctx context.Context, input WatchUpdateInput, _ ToolProgressEmitter) (WatchOutput, error) {
			loaded, err := deps.Store.LoadWatch(ctx, input.ID)
			if err != nil {
				return WatchOutput{}, fmt.Errorf("watch_update: %w", err)
			}
			w := *loaded
			now := deps.Clock()
			if name := strings.TrimSpace(input.Name); name != "" {
				w.Name = name
			}
			if input.Mode != "" {
				if w.Mode, err = watchMode(input.Mode, w.Mode); err != nil {
					return WatchOutput{}, fmt.Errorf("watch_update: %w", err)
				}
			}
			if input.Every != "" {
				if w.Interval, err = watchEvery(input.Every, w.Interval); err != nil {
					return WatchOutput{}, fmt.Errorf("watch_update: %w", err)
				}
			}
			switch input.Status {
			case "":
			case string(core.WatchPaused):
				w = w.Pause(now)
			case string(core.WatchActive):
				w = w.Resume(now)
			default:
				return WatchOutput{}, fmt.Errorf("watch_update: status %q must be active or paused", input.Status)
			}
			w.UpdatedAt = now
			if selector := strings.TrimSpace(input.Selector); selector != "" && selector != w.Selector {
				if err := deps.Checker.Validate(w.Kind, w.Target, selector); err != nil {
					return WatchOutput{}, fmt.Errorf("watch_update: %w", err)
				}
				w.Selector = selector
				return rebaseline(ctx, deps, w)
			}
			if err := deps.Store.UpdateWatch(ctx, w); err != nil {
				return WatchOutput{}, fmt.Errorf("watch_update: %w", err)
			}
			return watchOutput(w, 0, deps.Location), nil
		})
}

// rebaseline fetches a watch whose selector changed and records the result as
// a fresh baseline.
func rebaseline(ctx context.Context, deps WatchToolDeps, w core.Watch) (WatchOutput, error) {
	fetched, err := deps.Checker.Fetch(ctx, w)
	if err != nil {
		return WatchOutput{}, fmt.Errorf("watch_update: the check with the new selector failed, nothing changed: %w", err)
	}
	w.LastCheckedAt, w.Snapshot = time.Time{}, ""
	if err := deps.Store.UpdateWatch(ctx, w); err != nil {
		return WatchOutput{}, fmt.Errorf("watch_update: %w", err)
	}
	result, err := deps.Checker.Apply(ctx, w, fetched)
	if err != nil {
		return WatchOutput{}, fmt.Errorf("watch_update: %w", err)
	}
	return watchOutput(result.Watch, 0, deps.Location), nil
}

func buildWatchListTool(deps WatchToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("watch_list", "List the watches with their state, schedule, last error and how many items wait for the morning briefing.",
		func(ctx context.Context, _ struct{}, _ ToolProgressEmitter) (WatchListOutput, error) {
			watches, err := deps.Store.ListWatches(ctx)
			if err != nil {
				return WatchListOutput{}, fmt.Errorf("watch_list: %w", err)
			}
			pending, err := deps.Store.ListWatchItems(ctx, core.WatchItemPending, 1000)
			if err != nil {
				return WatchListOutput{}, fmt.Errorf("watch_list: %w", err)
			}
			counts := map[int64]int{}
			for _, item := range pending {
				counts[item.WatchID]++
			}
			out := WatchListOutput{Watches: make([]WatchOutput, 0, len(watches))}
			for _, w := range watches {
				out.Watches = append(out.Watches, watchOutput(w, counts[w.ID], deps.Location))
			}
			return out, nil
		})
}

func watchMode(value string, fallback core.WatchMode) (core.WatchMode, error) {
	switch core.WatchMode(strings.TrimSpace(value)) {
	case "":
		return fallback, nil
	case core.WatchModeDigest:
		return core.WatchModeDigest, nil
	case core.WatchModeImmediate:
		return core.WatchModeImmediate, nil
	default:
		return "", fmt.Errorf("mode %q must be digest or immediate", value)
	}
}

func watchEvery(value string, fallback time.Duration) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	every, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("every %q: %w", value, err)
	}
	if every < watch.MinInterval {
		return 0, fmt.Errorf("every %s is shorter than the minimum %s", every, watch.MinInterval)
	}
	return every, nil
}

func watchOutput(w core.Watch, pending int, loc *time.Location) WatchOutput {
	out := WatchOutput{
		ID: w.ID, Name: w.Name, Kind: string(w.Kind), Target: w.Target, Selector: w.Selector,
		Mode: string(w.Mode), Every: w.Interval.String(), Status: string(w.Status),
		NextCheck: w.NextCheckAt.In(loc).Format(localTimeLayout), LastError: w.LastError, Pending: pending,
	}
	if !w.LastCheckedAt.IsZero() {
		out.LastChecked = w.LastCheckedAt.In(loc).Format(localTimeLayout)
	}
	return out
}

// fetchedSample shows what a watch sees: up to three item titles, or the
// start of the page snapshot.
func fetchedSample(fetched watch.Fetched) []string {
	if fetched.Snapshot != "" {
		runes := []rune(fetched.Snapshot)
		if len(runes) > 200 {
			runes = append(runes[:200], '…')
		}
		return []string{string(runes)}
	}
	var sample []string
	for i, item := range fetched.Items {
		if i == 3 {
			break
		}
		sample = append(sample, item.Title)
	}
	return sample
}
