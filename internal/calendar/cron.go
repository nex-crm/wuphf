package calendar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule represents a parsed cron expression that can compute next fire times.
type CronSchedule struct {
	Minutes    []int // 0-59
	Hours      []int // 0-23
	DaysOfMonth []int // 1-31 (empty = any)
	Months     []int // 1-12 (empty = any)
	DaysOfWeek []int // 0-6, 0=Sunday (empty = any)
}

// ParseCron parses a cron expression string into a CronSchedule.
// Supported formats:
//   - "daily"       -> 09:00 every day
//   - "hourly"      -> every hour on the hour
//   - "Nh" (e.g. "4h") -> every N hours
//   - Standard 5-field: "minute hour dom month dow"
func ParseCron(expr string) (CronSchedule, error) {
	expr = strings.TrimSpace(strings.ToLower(expr))

	switch expr {
	case "daily":
		return CronSchedule{
			Minutes: []int{0},
			Hours:   []int{9},
		}, nil
	case "hourly":
		return CronSchedule{
			Minutes: []int{0},
		}, nil
	}

	// Check for Nh shorthand (e.g. "4h", "2h")
	if strings.HasSuffix(expr, "h") {
		numStr := strings.TrimSuffix(expr, "h")
		n, err := strconv.Atoi(numStr)
		if err == nil && n > 0 && n <= 24 {
			var hours []int
			for h := 0; h < 24; h += n {
				hours = append(hours, h)
			}
			return CronSchedule{
				Minutes: []int{0},
				Hours:   hours,
			}, nil
		}
	}

	// Standard 5-field cron: minute hour dom month dow
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return CronSchedule{}, fmt.Errorf("invalid cron expression: %q (expected 5 fields or shorthand)", expr)
	}

	minutes, err := parseField(fields[0], 0, 59)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("invalid minute field: %w", err)
	}
	hours, err := parseField(fields[1], 0, 23)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("invalid hour field: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("invalid day-of-month field: %w", err)
	}
	months, err := parseField(fields[3], 1, 12)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("invalid month field: %w", err)
	}
	dow, err := parseField(fields[4], 0, 6)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("invalid day-of-week field: %w", err)
	}

	return CronSchedule{
		Minutes:     minutes,
		Hours:       hours,
		DaysOfMonth: dom,
		Months:      months,
		DaysOfWeek:  dow,
	}, nil
}

// Next returns the next fire time strictly after the given time.
func (cs CronSchedule) Next(after time.Time) time.Time {
	// Start from the next minute boundary
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Search up to 2 years ahead to find a match
	limit := after.Add(2 * 365 * 24 * time.Hour)

	for t.Before(limit) {
		if cs.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}

	// Fallback: should not happen with valid schedules
	return time.Time{}
}

// matches returns true if the given time matches this schedule.
func (cs CronSchedule) matches(t time.Time) bool {
	if len(cs.Minutes) > 0 && !contains(cs.Minutes, t.Minute()) {
		return false
	}
	if len(cs.Hours) > 0 && !contains(cs.Hours, t.Hour()) {
		return false
	}
	if len(cs.DaysOfMonth) > 0 && !contains(cs.DaysOfMonth, t.Day()) {
		return false
	}
	if len(cs.Months) > 0 && !contains(cs.Months, int(t.Month())) {
		return false
	}
	if len(cs.DaysOfWeek) > 0 && !contains(cs.DaysOfWeek, int(t.Weekday())) {
		return false
	}
	return true
}

func contains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// parseField parses a single cron field (supports *, N, N-M, */N, N-M/N, N,M,O).
func parseField(field string, min, max int) ([]int, error) {
	if field == "*" {
		return nil, nil // nil means "any"
	}

	var result []int
	parts := strings.Split(field, ",")
	for _, part := range parts {
		vals, err := parseRange(part, min, max)
		if err != nil {
			return nil, err
		}
		result = append(result, vals...)
	}
	return result, nil
}

// parseRange parses "N", "N-M", "*/N", or "N-M/N".
func parseRange(part string, min, max int) ([]int, error) {
	var step int
	stepParts := strings.SplitN(part, "/", 2)
	rangePart := stepParts[0]
	if len(stepParts) == 2 {
		s, err := strconv.Atoi(stepParts[1])
		if err != nil || s <= 0 {
			return nil, fmt.Errorf("invalid step: %q", stepParts[1])
		}
		step = s
	}

	var start, end int
	if rangePart == "*" {
		start, end = min, max
	} else if strings.Contains(rangePart, "-") {
		bounds := strings.SplitN(rangePart, "-", 2)
		s, err := strconv.Atoi(bounds[0])
		if err != nil {
			return nil, fmt.Errorf("invalid range start: %q", bounds[0])
		}
		e, err := strconv.Atoi(bounds[1])
		if err != nil {
			return nil, fmt.Errorf("invalid range end: %q", bounds[1])
		}
		start, end = s, e
	} else {
		n, err := strconv.Atoi(rangePart)
		if err != nil {
			return nil, fmt.Errorf("invalid value: %q", rangePart)
		}
		if step == 0 {
			return []int{n}, nil
		}
		start, end = n, max
	}

	if start < min || end > max || start > end {
		return nil, fmt.Errorf("range %d-%d out of bounds [%d-%d]", start, end, min, max)
	}

	if step == 0 {
		step = 1
	}
	var result []int
	for i := start; i <= end; i += step {
		result = append(result, i)
	}
	return result, nil
}
