package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// loadActiveFacts returns non-retired user-scoped facts as a frozen snapshot,
// fit to charLimit. Unlike the search-based Entries, this is injected
// unconditionally into every run's system prompt so the agent always sees
// its most important persistent facts — it does not need to search for them.
// The snapshot is stable within a run (frozen) to preserve the model's
// prefix cache.
//
// A fact is an atomic semantic unit — if the whole fact doesn't fit the
// remaining budget, it is skipped (not truncated). Facts that don't fit are
// still in the searchable Archive; the agent retrieves them via memory_search
// when needed.
func (s *LocalService) loadActiveFacts(ctx context.Context, charLimit int) ([]Entry, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if charLimit <= 0 {
		return nil, nil
	}
	records, err := s.ListFacts(ctx, RecordSelection{})
	if err != nil {
		return nil, fmt.Errorf("load active facts: %w", err)
	}
	// Filter: non-retired + user scope only. Workspace-scoped facts are not
	// part of the always-on snapshot (they're context-dependent). Single-owner
	// semantics: facts the agent wrote (unverified or verified) are trusted;
	// only retired facts are excluded.
	filtered := make([]Record, 0, len(records))
	for _, r := range records {
		if r.Status == StatusRetired {
			continue
		}
		if r.Scope == "user" || r.Scope == "" {
			filtered = append(filtered, r)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Ref < filtered[j].Ref
	})

	var entries []Entry
	total := 0
	for _, r := range filtered {
		// Body already contains the full markdown content (including the
		// title as a heading). Use it directly for the snapshot.
		text := strings.TrimSpace(r.Body)
		if text == "" {
			text = strings.TrimSpace(r.Title)
		}
		if total+len(text) > charLimit {
			continue
		}
		entries = append(entries, Entry{
			Ref:     r.Ref,
			Kind:    string(r.Kind),
			Title:   r.Title,
			Content: text,
		})
		total += len(text)
	}
	return entries, nil
}

// RenderActiveFacts formats the active facts snapshot as a string block
// for injection into the system prompt.
func RenderActiveFacts(facts []Entry) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Active Memory (your persistent facts — always visible)\n")
	for _, f := range facts {
		b.WriteString(fmt.Sprintf("- %s\n", f.Content))
	}
	return b.String()
}
