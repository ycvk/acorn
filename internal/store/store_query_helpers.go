package store

import (
	"database/sql"
	"strings"
	"time"
	"unicode/utf8"
)

const trigramMinRunes = 3

// snippetAround returns up to 40 runes on each side of the first match.
func snippetAround(text, query string) string {
	runes := []rune(text)
	idx := strings.Index(text, query)
	if idx < 0 {
		idx = 0
	}
	start := utf8.RuneCountInString(text[:idx])
	from, to := max(start-40, 0), min(start+utf8.RuneCountInString(query)+40, len(runes))
	out := string(runes[from:to])
	if from > 0 {
		out = "…" + out
	}
	if to < len(runes) {
		out += "…"
	}
	return out
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// scanRows iterates rows, calls fn per row, closes rows and surfaces rows.Err.
func scanRows(rows *sql.Rows, fn func(scan func(...any) error) error) error {
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}

type scannerFunc func(...any) error

func (f scannerFunc) Scan(dest ...any) error { return f(dest...) }

func formatZeroableTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTimestamp(value)
}

func parseOptionalTime(value, field string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseTimestamp(fixedTimestampLayout, value, field)
}
