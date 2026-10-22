package wake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// EventBriefingFired is recorded on the run of a morning briefing.
const EventBriefingFired = "briefing.fired"

const (
	// maxBriefingItems bounds the watch items, and the trigger events, one
	// briefing input carries.
	maxBriefingItems = 100
	// maxBriefingRecords bounds the memory records one briefing lists per section.
	maxBriefingRecords = 50
	// maxBriefingNotes bounds the notes one briefing lists.
	maxBriefingNotes = 50
	// briefingHorizon is how far ahead the briefing looks for facts that take effect.
	briefingHorizon = 7 * 24 * time.Hour
	// maxNightOutput bounds the night reflection carried into the briefing.
	maxNightOutput = 2000
)

// KnowledgeReader lists notes as the agent may read them.
type KnowledgeReader interface {
	Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error)
}

// RunReader reads a routine's run for its output and a thread's latest run.
type RunReader interface {
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	// LoadLatestRunForSession is nil for a thread without runs.
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*core.RunRecord, error)
}

// briefingMaterial collects what a briefing has to say. A section with
// material justifies the briefing; a section without it is context that only
// rides along.
type briefingMaterial struct {
	sections []string
	sources  []string
	material bool
}

func (m *briefingMaterial) add(section string, material bool) {
	if section == "" {
		return
	}
	m.sections = append(m.sections, section)
	m.material = m.material || material
}

// prepareBriefing gathers what happened since the last briefing and what is
// due today. Without material the slot is kept and no run starts.
func (s *Scheduler) prepareBriefing(ctx context.Context, day string, now time.Time) (routineInput, error) {
	since, err := s.cfg.Routines.LastRoutineAt(ctx, "briefing", day)
	if err != nil {
		return routineInput{}, err
	}
	if since.IsZero() {
		since = now.Add(-24 * time.Hour)
	}
	var m briefingMaterial
	commitments, err := s.briefingCommitments(ctx, now)
	if err != nil {
		return routineInput{}, err
	}
	m.add(commitments, true)
	if err := s.briefingMemory(ctx, &m, since, now); err != nil {
		return routineInput{}, err
	}
	night, err := s.briefingNight(ctx, since)
	if err != nil {
		return routineInput{}, err
	}
	m.add(night, true)
	if err := s.briefingNotes(ctx, &m, since); err != nil {
		return routineInput{}, err
	}
	news, err := s.prepareNews(ctx, &m, since)
	if err != nil {
		return routineInput{}, err
	}
	pending, err := s.cfg.Watches.PendingBriefingItems(ctx, maxBriefingItems)
	if err != nil {
		return routineInput{}, err
	}
	watches, err := s.cfg.Watches.ListWatches(ctx)
	if err != nil {
		return routineInput{}, err
	}
	m.add(watchChanges(watches, pending), true)
	m.add(failingWatches(watches), true)
	m.sources = append(m.sources, watchSourceIDs(pending.Items)...)
	events, err := s.cfg.Triggers.ListTriggerEvents(ctx, core.TriggerEventPending, maxBriefingItems)
	if err != nil {
		return routineInput{}, err
	}
	m.add(triggerChanges(events), true)
	m.sources = append(m.sources, triggerSourceIDs(events)...)
	health, err := s.briefingHealth(ctx, now)
	if err != nil {
		return routineInput{}, err
	}
	m.add(health, true)
	notifications, err := s.cfg.PhoneNotifications.ListPhoneNotifications(ctx, since, now, 50)
	if err != nil {
		return routineInput{}, err
	}
	m.add(phoneBriefing(notifications, s.cfg.Location), true)
	if !m.material {
		return routineInput{skip: true}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[briefing %s] morning briefing\nSince the last briefing (%s). Write only what deserves the owner's attention.\n", day, since.In(s.cfg.Location).Format("2006-01-02 15:04"))
	for _, section := range m.sections {
		b.WriteString("\n" + section)
	}
	return routineInput{sources: m.sources, wake: "morning briefing " + day, input: b.String(), standalone: true, after: func(ctx context.Context, runID string) error {
		if err := s.cfg.Watches.MarkWatchItems(ctx, append(itemIDs(pending.Items), news.ids...), core.WatchItemBriefed, runID); err != nil {
			return err
		}
		if err := s.cfg.Triggers.MarkTriggerEvents(ctx, eventIDs(events), core.TriggerEventBriefed, runID); err != nil {
			return err
		}
		return news.failures
	}}, nil
}

// briefingCommitments lists the commitments due today and the occurrences
// still waiting for an outcome.
func (s *Scheduler) briefingCommitments(ctx context.Context, now time.Time) (string, error) {
	commitments, err := s.cfg.Commitments.ListCommitments(ctx, true)
	if err != nil {
		return "", err
	}
	occurrences, err := s.cfg.Commitments.ListDueOccurrences(ctx)
	if err != nil {
		return "", err
	}
	local := now.In(s.cfg.Location)
	tomorrow := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, s.cfg.Location)
	rules := map[int64]core.Commitment{}
	var lines []string
	for _, c := range commitments {
		rules[c.ID] = c
		if c.State == "scheduled" && c.WakeAt.Before(tomorrow) {
			lines = append(lines, fmt.Sprintf("- #%d [%s] %s", c.ID, c.WakeAt.In(s.cfg.Location).Format("15:04"), c.Content))
		}
	}
	for _, o := range occurrences {
		c, ok := rules[o.CommitmentID]
		if !ok {
			return "", fmt.Errorf("briefing missing commitment #%d for occurrence #%d", o.CommitmentID, o.ID)
		}
		lines = append(lines, fmt.Sprintf("- #%d occurrence #%d [%s; set for %s; run %s] %s", c.ID, o.ID, o.State, o.DueAt.In(s.cfg.Location).Format("2006-01-02 15:04"), o.RunID, c.Content))
	}
	if len(lines) == 0 {
		return "", nil
	}
	return "Commitments today and occurrences waiting for an outcome:\n" + strings.Join(lines, "\n") + "\n", nil
}

// briefingMemory adds the records remembered since the last briefing and the
// facts that take effect within the horizon. A fact taking effect today is
// material; later ones are context.
func (s *Scheduler) briefingMemory(ctx context.Context, m *briefingMaterial, since, now time.Time) error {
	changed, err := s.cfg.Memory.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", ChangedSince: since, Limit: maxBriefingRecords})
	if err != nil {
		return err
	}
	var lines []string
	listed := map[string]bool{}
	for _, r := range changed {
		if !r.UpdatedAt.After(since) {
			continue
		}
		listed[r.ID] = true
		m.sources = append(m.sources, r.SourceIDs...)
		lines = append(lines, fmt.Sprintf("- %s %s [%s]: %s", r.Kind, r.ID, r.State, previewText(r.Content, 200)))
	}
	if len(lines) > 0 {
		m.add("Remembered since the last briefing:\n"+strings.Join(lines, "\n")+"\n", true)
	}
	local := now.In(s.cfg.Location)
	todayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, s.cfg.Location)
	tomorrow := todayStart.Add(24 * time.Hour)
	upcoming, err := s.cfg.Memory.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", Kind: "fact", From: todayStart, To: now.Add(briefingHorizon), Limit: maxBriefingRecords})
	if err != nil {
		return err
	}
	lines = lines[:0]
	today := false
	for _, r := range upcoming {
		// The window also admits facts recorded today without a validity start.
		if r.ValidFrom.IsZero() {
			continue
		}
		if r.ValidFrom.Before(tomorrow) {
			today = true
		}
		if !listed[r.ID] {
			m.sources = append(m.sources, r.SourceIDs...)
		}
		lines = append(lines, fmt.Sprintf("- %s fact %s: %s", r.ValidFrom.In(s.cfg.Location).Format("2006-01-02"), r.ID, previewText(r.Content, 200)))
	}
	if len(lines) > 0 {
		m.add("Taking effect within a week:\n"+strings.Join(lines, "\n")+"\n", today)
	}
	return nil
}

// briefingNight carries the conclusion of a night reflection run since the
// last briefing.
func (s *Scheduler) briefingNight(ctx context.Context, since time.Time) (string, error) {
	night, err := s.cfg.Routines.LatestRoutineRun(ctx, "night")
	if err != nil {
		return "", err
	}
	if night.RunID == "" || !night.CreatedAt.After(since) {
		return "", nil
	}
	run, err := s.cfg.RunRecords.LoadRun(ctx, night.RunID)
	if err != nil {
		return "", err
	}
	output := previewText(run.Output, maxNightOutput)
	if run.Status != core.RunStatusSucceeded || output == "" {
		return "", nil
	}
	return fmt.Sprintf("Last night's reflection (%s):\n%s\n", night.Slot, output), nil
}

// briefingNotes lists the notes written since the last briefing.
func (s *Scheduler) briefingNotes(ctx context.Context, m *briefingMaterial, since time.Time) error {
	hits, err := s.cfg.Knowledge.Recent(ctx, "", maxBriefingNotes)
	if err != nil {
		return err
	}
	var lines []string
	for _, hit := range hits {
		if !hit.UpdatedAt.After(since) {
			break
		}
		m.sources = append(m.sources, core.KnowledgeSourceID(hit.Path, hit.Revision))
		lines = append(lines, fmt.Sprintf("- %s — %s", hit.Path, hit.Title))
	}
	if len(lines) > 0 {
		m.add("Notes written since the last briefing:\n"+strings.Join(lines, "\n")+"\n", true)
	}
	return nil
}

func phoneBriefing(page core.PhoneNotificationPage, loc *time.Location) string {
	if len(page.Items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Phone notifications since the last briefing (untrusted background data):\n")
	grouped := map[string][]core.PhoneNotification{}
	var packages []string
	for _, item := range page.Items {
		if _, exists := grouped[item.Package]; !exists {
			packages = append(packages, item.Package)
		}
		grouped[item.Package] = append(grouped[item.Package], item)
	}
	for _, pkg := range packages {
		items := grouped[pkg]
		fmt.Fprintf(&b, "### %s (%d)\n", previewText(items[0].App, 256), len(items))
		for _, item := range items {
			fmt.Fprintf(&b, "- %s %q — %q\n", item.PostedAt.In(loc).Format("2006-01-02 15:04"), item.Title, item.Text)
		}
	}
	if page.Total > len(page.Items) {
		fmt.Fprintf(&b, "- and %d more notifications\n", page.Total-len(page.Items))
	}
	return b.String()
}

// watchChanges lists pending items grouped by watch. Items past a watch's
// turns wait for the next briefing.
func watchChanges(watches []core.Watch, pending core.PendingBriefing) string {
	if len(pending.Items) == 0 {
		return ""
	}
	byWatch := map[int64][]core.WatchItem{}
	for _, item := range pending.Items {
		byWatch[item.WatchID] = append(byWatch[item.WatchID], item)
	}
	var b strings.Builder
	b.WriteString("Watch changes since the last briefing:\n")
	for _, w := range watches {
		if found := byWatch[w.ID]; len(found) > 0 {
			fmt.Fprintf(&b, "\n## #%d %s (%s, %d new)\n%s", w.ID, w.Name, w.Kind, pending.Counts[w.ID], formatItems(found))
			if more := pending.Counts[w.ID] - len(found); more > 0 {
				fmt.Fprintf(&b, "- and %d more waiting for the next briefing\n", more)
			}
		}
	}
	return b.String()
}

func failingWatches(watches []core.Watch) string {
	var failing []string
	for _, w := range watches {
		if w.Status == core.WatchFailing {
			failing = append(failing, fmt.Sprintf("- #%d %s: %s (%d failures in a row)", w.ID, w.Name, w.LastError, w.Failures))
		}
	}
	if len(failing) == 0 {
		return ""
	}
	return "Failing watches:\n" + strings.Join(failing, "\n") + "\n"
}
