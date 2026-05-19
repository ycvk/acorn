package sqlite

import (
	"fmt"
	"time"
)

func parseTimestamp(layout, value, field string) (time.Time, error) {
	t, err := time.Parse(layout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s timestamp %q: %w", field, value, err)
	}
	return t, nil
}
