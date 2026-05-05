package calendar

import (
	"testing"
	"time"
)

// 2026-03-01 is a Sunday, 2026-03-02 a Monday, 2026-03-03 a Tuesday.
// Pinning these dates keeps the day-of-week math obvious in the cases below.

func TestMatches_DOMandDOW_ORWhenBothRestricted(t *testing.T) {
	sched, err := ParseCron("0 9 1 * 1")
	if err != nil {
		t.Fatalf("ParseCron error: %v", err)
	}

	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"first of month, not Monday", time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), true},
		{"Monday, not first of month", time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), true},
		// 2026-06-01 is both the 1st and a Monday; both fields match, OR still true.
		{"both dom and dow match", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), true},
		{"neither dom nor dow", time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC), false},
		{"correct day, wrong hour", time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC), false},
		{"correct day, wrong minute", time.Date(2026, 3, 2, 9, 30, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sched.matches(tc.when); got != tc.want {
				t.Errorf("matches(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

func TestMatches_DOMOnly_IgnoresWeekday(t *testing.T) {
	sched, err := ParseCron("0 9 15 * *")
	if err != nil {
		t.Fatalf("ParseCron error: %v", err)
	}

	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"15th, any weekday", time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC), true}, // Sunday
		{"15th, weekday again", time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC), true},
		{"not 15th", time.Date(2026, 3, 16, 9, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sched.matches(tc.when); got != tc.want {
				t.Errorf("matches(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

func TestMatches_DOWOnly_IgnoresDayOfMonth(t *testing.T) {
	sched, err := ParseCron("0 9 * * 1")
	if err != nil {
		t.Fatalf("ParseCron error: %v", err)
	}

	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"Monday the 2nd", time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), true},
		{"Monday the 9th", time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC), true},
		{"Sunday", time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sched.matches(tc.when); got != tc.want {
				t.Errorf("matches(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

func TestMatches_NoDayRestrictions_FiresEveryDay(t *testing.T) {
	sched, err := ParseCron("0 9 * * *")
	if err != nil {
		t.Fatalf("ParseCron error: %v", err)
	}

	for day := 1; day <= 7; day++ {
		when := time.Date(2026, 3, day, 9, 0, 0, 0, time.UTC)
		if !sched.matches(when) {
			t.Errorf("matches(%v) = false, want true (wildcard day fields)", when)
		}
	}
	if sched.matches(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("matches at wrong hour returned true")
	}
}

func TestMatches_MonthRestrictionShortCircuits(t *testing.T) {
	// June only, OR semantics on the day fields should not rescue a non-June time.
	sched, err := ParseCron("0 9 1 6 1")
	if err != nil {
		t.Fatalf("ParseCron error: %v", err)
	}

	cases := []struct {
		name string
		when time.Time
		want bool
	}{
		{"June 1st", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), true},
		{"June Monday not 1st", time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC), true},
		{"March 1st (wrong month, would match dom)", time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC), false},
		{"March Monday (wrong month, would match dow)", time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sched.matches(tc.when); got != tc.want {
				t.Errorf("matches(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}
