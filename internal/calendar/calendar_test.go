package calendar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCronDaily(t *testing.T) {
	sched, err := ParseCron("daily")
	if err != nil {
		t.Fatalf("ParseCron(daily) error: %v", err)
	}
	if len(sched.Minutes) != 1 || sched.Minutes[0] != 0 {
		t.Errorf("daily minutes: got %v, want [0]", sched.Minutes)
	}
	if len(sched.Hours) != 1 || sched.Hours[0] != 9 {
		t.Errorf("daily hours: got %v, want [9]", sched.Hours)
	}
}

func TestParseCronHourly(t *testing.T) {
	sched, err := ParseCron("hourly")
	if err != nil {
		t.Fatalf("ParseCron(hourly) error: %v", err)
	}
	if len(sched.Minutes) != 1 || sched.Minutes[0] != 0 {
		t.Errorf("hourly minutes: got %v, want [0]", sched.Minutes)
	}
	if sched.Hours != nil {
		t.Errorf("hourly hours: got %v, want nil (any)", sched.Hours)
	}
}

func TestParseCronNh(t *testing.T) {
	sched, err := ParseCron("4h")
	if err != nil {
		t.Fatalf("ParseCron(4h) error: %v", err)
	}
	want := []int{0, 4, 8, 12, 16, 20}
	if len(sched.Hours) != len(want) {
		t.Fatalf("4h hours: got %v, want %v", sched.Hours, want)
	}
	for i, h := range sched.Hours {
		if h != want[i] {
			t.Errorf("4h hours[%d]: got %d, want %d", i, h, want[i])
		}
	}
}

func TestParseCronFiveField(t *testing.T) {
	// 9am weekdays
	sched, err := ParseCron("0 9 * * 1-5")
	if err != nil {
		t.Fatalf("ParseCron(5-field) error: %v", err)
	}
	if len(sched.Minutes) != 1 || sched.Minutes[0] != 0 {
		t.Errorf("5-field minutes: got %v, want [0]", sched.Minutes)
	}
	if len(sched.Hours) != 1 || sched.Hours[0] != 9 {
		t.Errorf("5-field hours: got %v, want [9]", sched.Hours)
	}
	if sched.DaysOfMonth != nil {
		t.Errorf("5-field dom: got %v, want nil", sched.DaysOfMonth)
	}
	if sched.Months != nil {
		t.Errorf("5-field months: got %v, want nil", sched.Months)
	}
	wantDow := []int{1, 2, 3, 4, 5}
	if len(sched.DaysOfWeek) != len(wantDow) {
		t.Fatalf("5-field dow: got %v, want %v", sched.DaysOfWeek, wantDow)
	}
	for i, d := range sched.DaysOfWeek {
		if d != wantDow[i] {
			t.Errorf("5-field dow[%d]: got %d, want %d", i, d, wantDow[i])
		}
	}
}

func TestParseCronInvalid(t *testing.T) {
	_, err := ParseCron("not a cron")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestNextFireDaily(t *testing.T) {
	sched, _ := ParseCron("daily")
	// Monday 2026-03-23 at 10:00
	after := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	next := sched.Next(after)
	want := time.Date(2026, 3, 24, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("daily Next after 10:00: got %v, want %v", next, want)
	}
}

func TestNextFireBeforeSchedule(t *testing.T) {
	sched, _ := ParseCron("daily")
	// 2026-03-23 at 08:00 — next should be same day at 09:00
	after := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)
	next := sched.Next(after)
	want := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("daily Next before 9am: got %v, want %v", next, want)
	}
}

func TestNextFireHourly(t *testing.T) {
	sched, _ := ParseCron("hourly")
	after := time.Date(2026, 3, 23, 14, 30, 0, 0, time.UTC)
	next := sched.Next(after)
	want := time.Date(2026, 3, 23, 15, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("hourly Next: got %v, want %v", next, want)
	}
}

func TestNextFire4h(t *testing.T) {
	sched, _ := ParseCron("4h")
	after := time.Date(2026, 3, 23, 5, 0, 0, 0, time.UTC)
	next := sched.Next(after)
	want := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("4h Next after 05:00: got %v, want %v", next, want)
	}
}

func TestNextFireWeekdays(t *testing.T) {
	sched, _ := ParseCron("0 9 * * 1-5")
	// Saturday 2026-03-28
	after := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	next := sched.Next(after)
	// Should skip to Monday 2026-03-30
	want := time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("weekdays Next from Saturday: got %v (weekday=%s), want %v", next, next.Weekday(), want)
	}
}

func TestScheduleCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "calendar.json")
	store := NewCalendarStore(path)

	// Add
	err := store.AddSchedule("ceo", "daily", "CEO daily standup")
	if err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}
	err = store.AddSchedule("cto", "4h", "CTO check-in")
	if err != nil {
		t.Fatalf("AddSchedule: %v", err)
	}

	// List
	schedules := store.ListSchedules()
	if len(schedules) != 2 {
		t.Fatalf("ListSchedules: got %d, want 2", len(schedules))
	}
	if schedules[0].AgentSlug != "ceo" {
		t.Errorf("ListSchedules[0]: got %s, want ceo", schedules[0].AgentSlug)
	}
	if schedules[1].AgentSlug != "cto" {
		t.Errorf("ListSchedules[1]: got %s, want cto", schedules[1].AgentSlug)
	}

	// Verify persistence
	store2 := NewCalendarStore(path)
	if len(store2.ListSchedules()) != 2 {
		t.Error("schedules not persisted")
	}

	// Remove
	store.RemoveSchedule("ceo")
	schedules = store.ListSchedules()
	if len(schedules) != 1 {
		t.Fatalf("after remove: got %d, want 1", len(schedules))
	}
	if schedules[0].AgentSlug != "cto" {
		t.Errorf("remaining schedule: got %s, want cto", schedules[0].AgentSlug)
	}

	// Replace (AddSchedule for existing slug)
	err = store.AddSchedule("cto", "hourly", "CTO hourly")
	if err != nil {
		t.Fatalf("AddSchedule replace: %v", err)
	}
	schedules = store.ListSchedules()
	if len(schedules) != 1 {
		t.Fatalf("after replace: got %d, want 1", len(schedules))
	}
	if schedules[0].CronExpr != "hourly" {
		t.Errorf("replaced cron: got %s, want hourly", schedules[0].CronExpr)
	}
}

func TestGetEventsForWeek(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "calendar.json")
	store := NewCalendarStore(path)

	_ = store.AddSchedule("ceo", "daily", "CEO standup")

	// Week starting Monday 2026-03-23
	start := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	events := store.GetEventsForWeek(start)

	// daily at 09:00 should produce 7 events
	if len(events) != 7 {
		t.Fatalf("GetEventsForWeek(daily): got %d events, want 7", len(events))
	}

	for i, ev := range events {
		if ev.AgentSlug != "ceo" {
			t.Errorf("event[%d] agent: got %s, want ceo", i, ev.AgentSlug)
		}
		if ev.ScheduledAt.Hour() != 9 || ev.ScheduledAt.Minute() != 0 {
			t.Errorf("event[%d] time: got %s, want 09:00", i, ev.ScheduledAt.Format("15:04"))
		}
	}
}

func TestStoreFileCreation(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested", "deep")
	path := filepath.Join(subdir, "calendar.json")
	store := NewCalendarStore(path)

	_ = store.AddSchedule("test", "hourly", "test")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("calendar.json not created")
	}
}
