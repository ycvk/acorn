package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/ycvk/acorn/internal/core"
)

const memoryAliasSchema = `
CREATE TABLE IF NOT EXISTS memory_entity_aliases (
 record_id TEXT NOT NULL, revision INTEGER NOT NULL, canonical TEXT NOT NULL,
 alias TEXT NOT NULL, scope TEXT NOT NULL, source_id TEXT NOT NULL, quote TEXT NOT NULL,
 PRIMARY KEY(record_id,revision,canonical,alias,scope),
 FOREIGN KEY(record_id,revision) REFERENCES memory_revisions(record_id,revision),
 FOREIGN KEY(source_id) REFERENCES memory_sources(id)
);
CREATE INDEX IF NOT EXISTS idx_memory_entity_aliases_name ON memory_entity_aliases(alias,scope,canonical);
`

func validateMemoryAliases(record *core.MemoryRecord, aliases []core.MemoryEntityAlias) error {
	for _, a := range aliases {
		a.Canonical, a.Alias, a.Scope = strings.TrimSpace(a.Canonical), strings.TrimSpace(a.Alias), strings.TrimSpace(a.Scope)
		if record.Kind != "fact" || a.Canonical == "" || a.Alias == "" || strings.EqualFold(a.Canonical, a.Alias) {
			return errors.New("entity aliases require a fact, canonical name and distinct alias")
		}
		if !slices.Contains(record.Entities, a.Canonical) || !strings.Contains(a.Quote, a.Canonical) || !strings.Contains(a.Quote, a.Alias) || (a.Scope != "" && !strings.Contains(a.Quote, a.Scope)) {
			return errors.New("entity alias canonical name, alias and scope must occur in its source quote; canonical name must be an entity")
		}
		if !slices.ContainsFunc(record.Evidence, func(e core.MemoryEvidence) bool {
			return e.SourceID == a.SourceID && e.Relation == "supports" && strings.Contains(e.Quote, a.Quote)
		}) {
			return fmt.Errorf("entity alias has no supporting quoted evidence: %s", a.Alias)
		}
		if !slices.Contains(record.Aliases, a) {
			record.Aliases = append(record.Aliases, a)
		}
	}
	return nil
}

func persistMemoryAliases(ctx context.Context, q memorySQL, record core.MemoryRecord) error {
	for _, a := range record.Aliases {
		if _, err := q.ExecContext(ctx, `INSERT INTO memory_entity_aliases(record_id,revision,canonical,alias,scope,source_id,quote) VALUES(?,?,?,?,?,?,?)`, record.ID, record.Revision, a.Canonical, a.Alias, a.Scope, a.SourceID, a.Quote); err != nil {
			return err
		}
	}
	return nil
}

// CROSS JOIN keeps matching alias declarations ahead of revision reads.
const memoryAliasQuerySQL = `SELECT DISTINCT a.canonical,a.alias,a.scope,v.data FROM memory_entity_aliases a CROSS JOIN memory_records r ON r.id=a.record_id CROSS JOIN memory_revisions v ON v.record_id=a.record_id AND v.revision=a.revision WHERE r.excluded=0 AND instr(lower(?),lower(a.alias))>0 AND ` + memoryRevisionVisibility

// Explicit identity assertions expand a name only when its identity is unique
// or the query names a distinguishing scope. Ambiguous names keep their source
// records available without choosing one person's identity for the caller.
func (s *Store) expandMemoryAliases(ctx context.Context, q core.MemoryQuery) (core.MemoryQuery, error) {
	text := q.Query + " " + strings.Join(q.Entities, " ")
	if strings.TrimSpace(text) == "" {
		return q, nil
	}
	query := memoryAliasQuerySQL
	args := []any{text}
	if q.KnownAt.IsZero() {
		query += ` AND a.revision=r.revision`
	} else {
		query += ` AND a.revision=(SELECT revision FROM memory_revisions WHERE record_id=a.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1)`
		args = append(args, formatTimestamp(q.KnownAt))
	}
	query += ` AND json_extract(v.data,'$.state')='current' AND COALESCE(json_extract(v.data,'$.needs_review'),0)=0`
	rows, err := s.read.QueryContext(ctx, query, args...)
	if err != nil {
		return q, err
	}
	byAlias := map[string][]core.MemoryEntityAlias{}
	if err := scanRows(rows, func(scan func(...any) error) error {
		var a core.MemoryEntityAlias
		var data string
		if err := scan(&a.Canonical, &a.Alias, &a.Scope, &data); err != nil {
			return err
		}
		var record core.MemoryRecord
		if err := json.Unmarshal([]byte(data), &record); err != nil {
			return err
		}
		if !memoryRecordMatches(record, core.MemoryQuery{AsOf: q.AsOf}) {
			return nil
		}
		if containsEntityName(text, a.Alias) {
			byAlias[strings.ToLower(a.Alias)] = append(byAlias[strings.ToLower(a.Alias)], a)
		}
		return nil
	}); err != nil {
		return q, err
	}
	names := []string{}
	resolved := map[string]string{}
	for alias, candidates := range byAlias {
		var all, scoped []string
		for _, a := range candidates {
			all = append(all, a.Canonical)
			if a.Scope != "" && containsEntityName(text, a.Scope) {
				scoped = append(scoped, a.Canonical)
			}
		}
		all, scoped = uniqueMemoryStrings(all), uniqueMemoryStrings(scoped)
		if len(scoped) == 1 {
			resolved[alias] = scoped[0]
		} else if len(scoped) == 0 && len(all) == 1 {
			resolved[alias] = all[0]
		}
	}
	for _, name := range resolved {
		names = append(names, name)
	}
	slices.Sort(names)
	if len(names) > 0 {
		q.Query = strings.TrimSpace(q.Query + " " + strings.Join(uniqueMemoryStrings(names), " "))
	}
	q.Entities = slices.Clone(q.Entities)
	for i, entity := range q.Entities {
		if name := resolved[strings.ToLower(entity)]; name != "" {
			q.Entities[i] = name
		}
	}
	return q, nil
}

func containsEntityName(text, name string) bool {
	text, name = strings.ToLower(text), strings.ToLower(name)
	latin := false
	for _, r := range name {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			latin = true
		}
	}
	for start := 0; start < len(text); {
		i := strings.Index(text[start:], name)
		if i < 0 {
			return false
		}
		i += start
		word := func(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' }
		if !latin || (i == 0 || !word(text[i-1])) && (i+len(name) == len(text) || !word(text[i+len(name)])) {
			return true
		}
		start = i + len(name)
	}
	return false
}
