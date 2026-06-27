package triggers

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSchedule is a parsed 5-field cron expression: minute hour day-of-month
// month day-of-week. Each field is a set of matching values. The parser
// supports * (wildcard), comma lists, ranges (1-5), and step values (*/2 or
// 1-10/2). No named months/days (JAN, MON) — numeric only.
type cronSchedule struct {
	minute  map[int]bool
	hour    map[int]bool
	dom     map[int]bool // day of month
	month   map[int]bool
	dow     map[int]bool // day of week (0=Sun, 7=Sun)
	domStar bool         // dom field was * (wildcard)
	dowStar bool         // dow field was * (wildcard)
}

func parseCron(expr string) (*cronSchedule, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields, got %d: %q", len(fields), expr)
	}
	s := &cronSchedule{}
	var err error
	s.minute, err = parseCronField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron minute field %q: %w", fields[0], err)
	}
	s.hour, err = parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron hour field %q: %w", fields[1], err)
	}
	s.dom, s.domStar, err = parseCronFieldTrackingStar(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-month field %q: %w", fields[2], err)
	}
	s.month, err = parseCronField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron month field %q: %w", fields[3], err)
	}
	s.dow, s.dowStar, err = parseCronFieldTrackingStar(fields[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("cron day-of-week field %q: %w", fields[4], err)
	}
	// Normalize 7 to 0 (both mean Sunday).
	if s.dow[7] {
		s.dow[0] = true
		delete(s.dow, 7)
	}
	return s, nil
}

// parseCronField parses one cron field into a set of ints. Supports:
//
//   - — all values in [min, max]
//     */n      — every n-th value starting from min
//     a        — single value
//     a-b      — range
//     a-b/n    — every n-th value in range
//     a,b,c    — comma-separated list of any of the above
func parseCronField(field string, min, max int) (map[int]bool, error) {
	result, _, err := parseCronFieldTrackingStar(field, min, max)
	return result, err
}

// parseCronFieldTrackingStar is parseCronField but also reports whether the
// raw field was a pure * (wildcard). This is needed for the standard cron
// dom/dow OR rule: when both dom and dow are restricted (non-wildcard), a
// match on either suffices.
func parseCronFieldTrackingStar(field string, min, max int) (map[int]bool, bool, error) {
	result := make(map[int]bool)
	star := true
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part != "*" && !strings.HasPrefix(part, "*/") {
			star = false
		}
		vals, err := expandCronPart(part, min, max)
		if err != nil {
			return nil, false, err
		}
		for _, v := range vals {
			result[v] = true
		}
	}
	if len(result) == 0 {
		return nil, false, fmt.Errorf("empty field")
	}
	return result, star, nil
}

func expandCronPart(part string, min, max int) ([]int, error) {
	// Split on / for step.
	rangePart, stepPart, hasStep := strings.Cut(part, "/")
	step := 1
	if hasStep {
		s, err := strconv.Atoi(stepPart)
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("invalid step %q", stepPart)
		}
		step = s
	}

	var lo, hi int
	if rangePart == "*" {
		lo, hi = min, max
	} else if loStr, hiStr, hasRange := strings.Cut(rangePart, "-"); hasRange {
		var err error
		lo, err = strconv.Atoi(loStr)
		if err != nil || lo < min || lo > max {
			return nil, fmt.Errorf("invalid range start %q", loStr)
		}
		hi, err = strconv.Atoi(hiStr)
		if err != nil || hi < min || hi > max {
			return nil, fmt.Errorf("invalid range end %q", hiStr)
		}
		if hi < lo {
			return nil, fmt.Errorf("range end %d < start %d", hi, lo)
		}
	} else {
		v, err := strconv.Atoi(rangePart)
		if err != nil || v < min || v > max {
			return nil, fmt.Errorf("invalid value %q (must be %d-%d)", rangePart, min, max)
		}
		lo, hi = v, v
	}

	var result []int
	for v := lo; v <= hi; v += step {
		result = append(result, v)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("step %d produced no values in [%d,%d]", step, lo, hi)
	}
	return result, nil
}

// next returns the next time at or after from that matches the schedule.
// It iterates minute by minute, which is O(60*24*366) worst case but in
// practice resolves within a few iterations for typical schedules. The
// cap prevents pathological loops on impossible expressions (e.g.,
// Feb 31).
func (s *cronSchedule) next(from time.Time) time.Time {
	// Start from the next minute boundary after 'from'.
	t := from.Truncate(time.Minute).Add(time.Minute)
	// Cap at ~2 years to prevent infinite loops on impossible dates.
	cap := 2 * 365 * 24 * 60
	for i := 0; i < cap; i++ {
		if s.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{} // no match found within cap
}

func (s *cronSchedule) matches(t time.Time) bool {
	if !s.minute[t.Minute()] || !s.hour[t.Hour()] || !s.month[int(t.Month())] {
		return false
	}
	// Standard cron dom/dow rule: if both are restricted (non-wildcard),
	// a match on either suffices (OR). If either is wildcard, both must
	// match (AND).
	if !s.domStar && !s.dowStar {
		return s.dom[t.Day()] || s.dow[int(t.Weekday())]
	}
	return s.dom[t.Day()] && s.dow[int(t.Weekday())]
}
