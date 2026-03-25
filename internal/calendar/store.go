package calendar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// storeData is the on-disk JSON structure.
type storeData struct {
	Schedules []ScheduleEntry `json:"schedules"`
	Events    []CalendarEvent `json:"events"`
}

// CalendarStore persists schedules and events to ~/.wuphf/calendar.json.
type CalendarStore struct {
	path string
	data storeData
}

// NewCalendarStore creates a store backed by the given file path.
// If path is empty, defaults to ~/.wuphf/calendar.json.
func NewCalendarStore(path string) *CalendarStore {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".wuphf", "calendar.json")
	}
	s := &CalendarStore{path: path}
	s.load()
	return s
}

// AddSchedule adds or replaces a schedule for the given agent.
func (s *CalendarStore) AddSchedule(agentSlug, cronExpr, description string) error {
	sched, err := ParseCron(cronExpr)
	if err != nil {
		return err
	}

	nextFire := sched.Next(time.Now())

	// Remove existing schedule for this agent
	s.RemoveSchedule(agentSlug)

	s.data.Schedules = append(s.data.Schedules, ScheduleEntry{
		AgentSlug:   agentSlug,
		CronExpr:    cronExpr,
		Description: description,
		Enabled:     true,
		NextFire:    nextFire,
	})

	return s.save()
}

// RemoveSchedule removes the schedule for the given agent.
func (s *CalendarStore) RemoveSchedule(agentSlug string) {
	filtered := s.data.Schedules[:0]
	for _, entry := range s.data.Schedules {
		if entry.AgentSlug != agentSlug {
			filtered = append(filtered, entry)
		}
	}
	s.data.Schedules = filtered
	_ = s.save()
}

// ListSchedules returns all schedule entries sorted by agent slug.
func (s *CalendarStore) ListSchedules() []ScheduleEntry {
	result := make([]ScheduleEntry, len(s.data.Schedules))
	copy(result, s.data.Schedules)
	sort.Slice(result, func(i, j int) bool {
		return result[i].AgentSlug < result[j].AgentSlug
	})
	return result
}

// GetEventsForWeek generates calendar events for the 7-day period starting at start.
func (s *CalendarStore) GetEventsForWeek(start time.Time) []CalendarEvent {
	end := start.Add(7 * 24 * time.Hour)
	var events []CalendarEvent

	for _, entry := range s.data.Schedules {
		if !entry.Enabled {
			continue
		}
		sched, err := ParseCron(entry.CronExpr)
		if err != nil {
			continue
		}

		// Walk through all fire times in the week window
		t := sched.Next(start.Add(-time.Minute))
		for t.Before(end) {
			status := StatusPending
			if t.Before(time.Now()) {
				status = StatusMissed
			}
			events = append(events, CalendarEvent{
				AgentSlug:   entry.AgentSlug,
				ScheduledAt: t,
				Status:      status,
			})
			t = sched.Next(t)
			if t.IsZero() {
				break
			}
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].ScheduledAt.Before(events[j].ScheduledAt)
	})
	return events
}

// load reads the store from disk. Missing file is not an error.
func (s *CalendarStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.data)
}

// save writes the store to disk, creating directories as needed.
func (s *CalendarStore) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
