package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// PackageTrigger is a trigger of a skill in use, with the package it runs
// from, the secrets the package declares and those that have no value.
type PackageTrigger struct {
	Trigger
	Skill    string
	Revision int64
	Hash     string
	Secrets  []string
	Missing  []string
}

// ID names the trigger as package/trigger.
func (t PackageTrigger) ID() string {
	return t.Skill + "/" + t.Name
}

// Triggers lists the triggers of the skills in use.
func (l *Library) Triggers(ctx context.Context) ([]PackageTrigger, error) {
	scan, err := l.Scan(ctx)
	if err != nil {
		return nil, err
	}
	var out []PackageTrigger
	for _, spec := range scan.Skills {
		if len(spec.Triggers) == 0 {
			continue
		}
		missing, err := l.MissingSecrets(ctx, spec)
		if err != nil {
			return nil, err
		}
		for _, t := range spec.Triggers {
			out = append(out, PackageTrigger{Trigger: t, Skill: spec.Name, Revision: spec.Revision, Hash: spec.Hash(), Secrets: spec.Secrets, Missing: missing})
		}
	}
	return out, nil
}

// TriggerRun asks a host to run a trigger's command once, from the package
// it holds under Skill at Hash.
type TriggerRun struct {
	Skill          string
	Hash           string
	Trigger        string
	Env            map[string]string
	TimeoutSeconds int
	MaxOutputBytes int
}

// TriggerResult is a finished trigger command.
type TriggerResult struct {
	ExitCode        int
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	TimedOut        bool
}

// TriggerHost holds packages and runs their trigger commands.
type TriggerHost interface {
	PackageHost
	RunTrigger(ctx context.Context, run TriggerRun) (TriggerResult, error)
}

// ErrMissingSecrets means a package's secrets are not all set, so its
// commands do not run.
var ErrMissingSecrets = errors.New("secrets missing")

// TriggerRunner fires package triggers on a host, bounded like the agent's
// shell commands.
type TriggerRunner struct {
	library        *Library
	host           TriggerHost
	timeoutSeconds int
	maxOutputBytes int
}

func NewTriggerRunner(library *Library, host TriggerHost, timeoutSeconds, maxOutputBytes int) (*TriggerRunner, error) {
	switch {
	case library == nil || host == nil:
		return nil, errors.New("trigger runner: library and host are required")
	case timeoutSeconds <= 0 || maxOutputBytes <= 0:
		return nil, errors.New("trigger runner: timeout and output limit must be positive")
	}
	return &TriggerRunner{library: library, host: host, timeoutSeconds: timeoutSeconds, maxOutputBytes: maxOutputBytes}, nil
}

// Triggers lists the triggers of the skills in use.
func (r *TriggerRunner) Triggers(ctx context.Context) ([]PackageTrigger, error) {
	return r.library.Triggers(ctx)
}

// Fire runs a trigger's command and returns what it printed. The host
// gets the trigger's package first when it holds another revision; the
// command gets the package's secrets as environment variables.
func (r *TriggerRunner) Fire(ctx context.Context, t PackageTrigger) (Output, error) {
	env, missing, err := r.library.secretEnv(ctx, Spec{Name: t.Skill, Secrets: t.Secrets})
	if err != nil {
		return Output{}, err
	}
	if len(missing) > 0 {
		return Output{}, fmt.Errorf("%w: %s", ErrMissingSecrets, strings.Join(missing, ", "))
	}
	if err := r.place(ctx, t); err != nil {
		return Output{}, err
	}
	result, err := r.host.RunTrigger(ctx, TriggerRun{Skill: t.Skill, Hash: t.Hash, Trigger: t.Name, Env: env, TimeoutSeconds: r.timeoutSeconds, MaxOutputBytes: r.maxOutputBytes})
	if err != nil {
		return Output{}, err
	}
	switch {
	case result.TimedOut:
		return Output{}, fmt.Errorf("the command timed out after %d seconds", r.timeoutSeconds)
	case result.ExitCode != 0:
		return Output{}, fmt.Errorf("the command exited with status %d: %s", result.ExitCode, tail(result.Stderr, 300))
	case result.StdoutTruncated:
		return Output{}, fmt.Errorf("the command printed more than %d bytes", r.maxOutputBytes/2)
	}
	return ParseOutput(result.Stdout)
}

// place puts the trigger's package on the host unless the host holds it.
func (r *TriggerRunner) place(ctx context.Context, t PackageTrigger) error {
	_, held, err := r.host.SkillPackages(ctx)
	if err != nil {
		return fmt.Errorf("list skill packages: %w", err)
	}
	if held[t.Skill] == t.Hash {
		return nil
	}
	current, err := r.library.store.Skill(ctx, t.Skill)
	if err != nil {
		return err
	}
	if current.Revision != t.Revision {
		return fmt.Errorf("skill %s changed to revision %d since its triggers were read", t.Skill, current.Revision)
	}
	files, err := r.library.store.SkillFiles(ctx, t.Skill, t.Revision)
	if err != nil {
		return err
	}
	if err := r.host.PutSkillPackage(ctx, t.Skill, t.Hash, current.Markdown, files); err != nil {
		return fmt.Errorf("put skill package %s: %w", t.Skill, err)
	}
	return nil
}

// Event is one line a trigger's command printed: a key unique for the
// trigger, and what happened.
type Event struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// Item is a piece of news a trigger's command found, for the news source
// that follows the trigger.
type Item struct {
	Key         string
	Title       string
	URL         string
	Summary     string
	PublishedAt time.Time
}

const (
	maxEventKeyBytes  = 256
	maxEventTextBytes = 8 << 10
	maxItemTitleBytes = 1 << 10
)

// Output is what a trigger's command printed: its events, its news items,
// and the view_data lines in order, each replacing the data of the pinned
// views the trigger feeds.
type Output struct {
	Events   []Event
	Items    []Item
	ViewData []map[string]any
}

// ParseOutput reads a trigger command's output: each non-blank line is an
// event {"key": "...", "text": "..."}, a news item {"item": {...}} or view
// data {"view_data": {...}}. A line that is none of them, or an event or item
// repeated in one output, fails the run.
func ParseOutput(stdout string) (Output, error) {
	var out Output
	seen, seenItems := map[string]bool{}, map[string]bool{}
	for i, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &fields); err == nil && len(fields) == 1 && fields["view_data"] != nil {
			var data map[string]any
			if err := json.Unmarshal(fields["view_data"], &data); err != nil || data == nil {
				return Output{}, fmt.Errorf("output line %d: view_data must be a JSON object", i+1)
			}
			out.ViewData = append(out.ViewData, data)
			continue
		}
		if len(fields) == 1 && fields["item"] != nil {
			item, err := parseItem(fields["item"])
			if err != nil {
				return Output{}, fmt.Errorf("output line %d: %w", i+1, err)
			}
			if seenItems[item.Key] {
				return Output{}, fmt.Errorf("output line %d repeats the item key %q", i+1, item.Key)
			}
			seenItems[item.Key] = true
			out.Items = append(out.Items, item)
			continue
		}
		var e Event
		decoder := json.NewDecoder(bytes.NewReader([]byte(line)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&e); err != nil || decoder.More() {
			return Output{}, fmt.Errorf(`output line %d is neither an event {"key": "...", "text": "..."}, an item {"item": {...}} nor {"view_data": {...}}: %s`, i+1, tail(line, 200))
		}
		e.Key, e.Text = strings.TrimSpace(e.Key), strings.TrimSpace(e.Text)
		switch {
		case e.Key == "" || e.Text == "":
			return Output{}, fmt.Errorf("output line %d: an event needs a key and a text", i+1)
		case len(e.Key) > maxEventKeyBytes:
			return Output{}, fmt.Errorf("output line %d: the key is longer than %d bytes", i+1, maxEventKeyBytes)
		case len(e.Text) > maxEventTextBytes:
			return Output{}, fmt.Errorf("output line %d: the text is longer than %d bytes", i+1, maxEventTextBytes)
		case seen[e.Key]:
			return Output{}, fmt.Errorf("output line %d repeats the key %q", i+1, e.Key)
		}
		seen[e.Key] = true
		out.Events = append(out.Events, e)
	}
	return out, nil
}

// parseItem reads {"key", "title", "url", "summary", "published"}: key and
// title are required, url is an absolute http(s) link and published an
// RFC 3339 time when given.
func parseItem(raw json.RawMessage) (Item, error) {
	var in struct {
		Key       string `json:"key"`
		Title     string `json:"title"`
		URL       string `json:"url"`
		Summary   string `json:"summary"`
		Published string `json:"published"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return Item{}, fmt.Errorf(`an item is {"key", "title", "url", "summary", "published"}: %w`, err)
	}
	item := Item{Key: strings.TrimSpace(in.Key), Title: strings.TrimSpace(in.Title), URL: strings.TrimSpace(in.URL), Summary: strings.TrimSpace(in.Summary)}
	switch {
	case item.Key == "" || item.Title == "":
		return Item{}, errors.New("an item needs a key and a title")
	case len(item.Key) > maxEventKeyBytes:
		return Item{}, fmt.Errorf("the item key is longer than %d bytes", maxEventKeyBytes)
	case len(item.Title) > maxItemTitleBytes:
		return Item{}, fmt.Errorf("the item title is longer than %d bytes", maxItemTitleBytes)
	case len(item.Summary) > maxEventTextBytes:
		return Item{}, fmt.Errorf("the item summary is longer than %d bytes", maxEventTextBytes)
	}
	if item.URL != "" {
		u, err := url.Parse(item.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Item{}, fmt.Errorf("the item url %q must be an absolute http(s) link", item.URL)
		}
	}
	if in.Published != "" {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(in.Published))
		if err != nil {
			return Item{}, fmt.Errorf("the item published %q must be an RFC 3339 time: %w", in.Published, err)
		}
		item.PublishedAt = at.UTC()
	}
	return item, nil
}

// tail keeps the end of a command's output, where errors usually are.
func tail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	cut := len(text) - limit
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut++
	}
	return "…" + text[cut:]
}
