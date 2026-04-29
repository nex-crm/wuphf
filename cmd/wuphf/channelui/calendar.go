package channelui

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// CalendarRange selects how far ahead the calendar app shows events.
// "day" caps the agenda at the next 24 hours; "week" extends to seven
// days. Used by BuildCalendarLines and FilterCalendarEvents.
type CalendarRange string

const (
	CalendarRangeDay  CalendarRange = "day"
	CalendarRangeWeek CalendarRange = "week"
)

// CalendarEvent is a single agenda row produced by CollectCalendarEvents
// — an upcoming due/follow-up/reminder/recheck for a task or request,
// or a scheduler-driven job. Channel and Participants are rendered as
// metadata; TaskID / RequestID / ThreadID let the renderer wire click
// targets.
type CalendarEvent struct {
	When             time.Time
	WhenLabel        string
	Kind             string
	Title            string
	Secondary        string
	Channel          string
	Provider         string
	ScheduleExpr     string
	Status           string
	IntervalLabel    string
	Participants     []string
	ParticipantSlugs []string
	TaskID           string
	RequestID        string
	ThreadID         string
}

// StatusOrFallback returns the explicit Status when set, otherwise a
// kind-specific default ("scheduled task" / "pending request" /
// "scheduled job" / "scheduled").
func (e CalendarEvent) StatusOrFallback() string {
	if strings.TrimSpace(e.Status) != "" {
		return e.Status
	}
	switch e.Kind {
	case "task":
		return "scheduled task"
	case "request":
		return "pending request"
	case "job":
		return "scheduled job"
	default:
		return "scheduled"
	}
}

// CalendarEventColors returns the (accent, background) pair used to
// border and fill the event card for each kind.
func CalendarEventColors(kind string) (string, string) {
	switch kind {
	case "task":
		return "#2563EB", "#131A27"
	case "request":
		return "#D97706", "#24170E"
	case "job":
		return "#7C3AED", "#1E1630"
	default:
		return "#334155", "#17161C"
	}
}

// CollectCalendarEvents builds a sorted (earliest-first) agenda from
// the scheduler jobs, tasks, and requests in scope. activeChannel is
// used as the fallback channel for events lacking one.
func CollectCalendarEvents(jobs []SchedulerJob, tasks []Task, requests []Interview, activeChannel string, members []Member) []CalendarEvent {
	var events []CalendarEvent
	for _, job := range jobs {
		whenText := strings.TrimSpace(job.NextRun)
		if whenText == "" {
			whenText = strings.TrimSpace(job.LastRun)
		}
		when, ok := ParseChannelTime(whenText)
		if !ok {
			continue
		}
		participants := CalendarParticipantsForJob(job, tasks, requests, activeChannel, members)
		interval := ""
		if job.IntervalMinutes > 0 {
			interval = fmt.Sprintf("every %s min", FormatMinutes(job.IntervalMinutes))
		}
		events = append(events, CalendarEvent{
			When:             when,
			WhenLabel:        PrettyCalendarWhen(when),
			Kind:             "job",
			Title:            job.Label,
			Secondary:        strings.TrimSpace(job.Status),
			Channel:          ChooseCalendarChannel(job.Channel, activeChannel),
			Provider:         strings.TrimSpace(job.Provider),
			ScheduleExpr:     strings.TrimSpace(job.ScheduleExpr),
			Status:           strings.TrimSpace(job.Status),
			IntervalLabel:    interval,
			Participants:     participants,
			ParticipantSlugs: CalendarParticipantSlugsForJob(job, tasks, requests, activeChannel, members),
			TaskID:           SchedulerTargetTaskID(job),
			RequestID:        SchedulerTargetRequestID(job),
			ThreadID:         SchedulerTargetThreadID(job, tasks, requests),
		})
	}
	for _, task := range tasks {
		if task.Status == "done" {
			continue
		}
		events = append(events, TaskCalendarEvents(task, activeChannel, members)...)
	}
	for _, req := range requests {
		events = append(events, RequestCalendarEvents(req, activeChannel, members)...)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].When.Equal(events[j].When) {
			if events[i].Kind == events[j].Kind {
				return events[i].Title < events[j].Title
			}
			return events[i].Kind < events[j].Kind
		}
		return events[i].When.Before(events[j].When)
	})
	return events
}

// TaskCalendarEvents fans out a Task into its scheduled events
// (due / follow up / reminder / recheck). Returns deduped events.
func TaskCalendarEvents(task Task, activeChannel string, members []Member) []CalendarEvent {
	var events []CalendarEvent
	appendTaskEvent := func(label, whenText string) {
		when, ok := ParseChannelTime(whenText)
		if !ok {
			return
		}
		participants := CalendarParticipantsForTask(task, activeChannel, members)
		status := strings.ReplaceAll(task.Status, "_", " ")
		secondary := label
		if strings.TrimSpace(task.PipelineStage) != "" {
			secondary += " · " + task.PipelineStage
		}
		if strings.TrimSpace(task.ReviewState) != "" && task.ReviewState != "not_required" {
			secondary += " · " + task.ReviewState
		}
		events = append(events, CalendarEvent{
			When:             when,
			WhenLabel:        PrettyCalendarWhen(when),
			Kind:             "task",
			Title:            task.Title,
			Secondary:        secondary,
			Channel:          ChooseCalendarChannel(task.Channel, activeChannel),
			Status:           status,
			Participants:     participants,
			ParticipantSlugs: CalendarParticipantSlugsForTask(task, activeChannel, members),
			TaskID:           task.ID,
			ThreadID:         task.ThreadID,
		})
	}
	appendTaskEvent("due", task.DueAt)
	appendTaskEvent("follow up", task.FollowUpAt)
	appendTaskEvent("reminder", task.ReminderAt)
	appendTaskEvent("recheck", task.RecheckAt)
	return DedupeCalendarEvents(events)
}

// RequestCalendarEvents fans out an Interview (request) into its
// scheduled events (due / follow up / reminder / recheck). Returns
// deduped events.
func RequestCalendarEvents(req Interview, activeChannel string, members []Member) []CalendarEvent {
	var events []CalendarEvent
	appendRequestEvent := func(label, whenText string) {
		when, ok := ParseChannelTime(whenText)
		if !ok {
			return
		}
		participants := CalendarParticipantsForRequest(req, activeChannel, members)
		status := strings.TrimSpace(req.Status)
		if status == "" {
			status = "pending"
		}
		events = append(events, CalendarEvent{
			When:             when,
			WhenLabel:        PrettyCalendarWhen(when),
			Kind:             "request",
			Title:            req.Question,
			Secondary:        label,
			Channel:          ChooseCalendarChannel(req.Channel, activeChannel),
			Status:           status,
			Participants:     participants,
			ParticipantSlugs: CalendarParticipantSlugsForRequest(req, activeChannel, members),
			RequestID:        req.ID,
			ThreadID:         req.ReplyTo,
		})
	}
	appendRequestEvent("due", req.DueAt)
	appendRequestEvent("follow up", req.FollowUpAt)
	appendRequestEvent("reminder", req.ReminderAt)
	appendRequestEvent("recheck", req.RecheckAt)
	return DedupeCalendarEvents(events)
}

// DedupeCalendarEvents drops duplicate events, keying primarily by
// TaskID / RequestID when set so two reminders for the same task
// collapse to one row.
func DedupeCalendarEvents(events []CalendarEvent) []CalendarEvent {
	seen := make(map[string]bool)
	var out []CalendarEvent
	for _, event := range events {
		identity := event.Kind + "|" + event.Title
		if strings.TrimSpace(event.TaskID) != "" {
			identity = "task|" + strings.TrimSpace(event.TaskID)
		} else if strings.TrimSpace(event.RequestID) != "" {
			identity = "request|" + strings.TrimSpace(event.RequestID)
		} else if strings.TrimSpace(event.ThreadID) != "" {
			identity = identity + "|thread:" + strings.TrimSpace(event.ThreadID)
		}
		key := identity + "|" + event.Secondary + "|" + event.When.Format(time.RFC3339)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, event)
	}
	return out
}

// FilterCalendarEvents narrows an agenda by view range (day / week)
// and an optional participant slug filter. Events are dropped when
// they fall after the range cutoff or, if filterSlug is set, when the
// participant slugs do not contain it.
func FilterCalendarEvents(events []CalendarEvent, viewRange CalendarRange, filterSlug string) []CalendarEvent {
	now := time.Now()
	filterSlug = strings.TrimSpace(filterSlug)
	var out []CalendarEvent
	for _, event := range events {
		if filterSlug != "" && !ContainsString(event.ParticipantSlugs, filterSlug) {
			continue
		}
		switch viewRange {
		case CalendarRangeDay:
			end := now.Add(24 * time.Hour)
			if event.When.After(end) {
				continue
			}
		default:
			end := now.Add(7 * 24 * time.Hour)
			if event.When.After(end) {
				continue
			}
		}
		out = append(out, event)
	}
	return out
}

// PrettyCalendarWhen formats an event time relative to "now" — "today
// HH:MM" / "tomorrow HH:MM" within two days, full date+time otherwise.
func PrettyCalendarWhen(when time.Time) string {
	now := time.Now()
	switch {
	case SameDay(when, now):
		return "today " + when.Format("15:04")
	case SameDay(when, now.Add(24*time.Hour)):
		return "tomorrow " + when.Format("15:04")
	default:
		return when.Format("Mon Jan 2 15:04")
	}
}

// CalendarBucketLabel maps a time to its agenda section: "Earlier",
// "Today", "Tomorrow", or "Upcoming".
func CalendarBucketLabel(when time.Time) string {
	now := time.Now()
	switch {
	case when.Before(now):
		return "Earlier"
	case SameDay(when, now):
		return "Today"
	case SameDay(when, now.Add(24*time.Hour)):
		return "Tomorrow"
	default:
		return "Upcoming"
	}
}

// ChooseCalendarChannel returns value when non-empty (after trimming),
// else the trimmed fallback. Lets events fall back to the active
// channel when their own Channel is unset.
func ChooseCalendarChannel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

// CalendarParticipantsForTask returns the display names for the
// task's participants, falling back to the channel roster when no
// owner is set.
func CalendarParticipantsForTask(task Task, activeChannel string, members []Member) []string {
	slugs := make([]string, 0, 2)
	if owner := strings.TrimSpace(task.Owner); owner != "" {
		slugs = append(slugs, owner)
	}
	return CalendarParticipantNames(slugs, ChooseCalendarChannel(task.Channel, activeChannel), members, len(slugs) == 0)
}

// CalendarParticipantSlugsForTask returns the slugs for the task's
// participants, falling back to the channel roster when no owner is
// set.
func CalendarParticipantSlugsForTask(task Task, activeChannel string, members []Member) []string {
	slugs := make([]string, 0, 2)
	if owner := strings.TrimSpace(task.Owner); owner != "" {
		slugs = append(slugs, owner)
	}
	return CalendarParticipantSlugs(slugs, ChooseCalendarChannel(task.Channel, activeChannel), members, len(slugs) == 0)
}

// CalendarParticipantsForRequest returns the display names for the
// request's participants, falling back to the channel roster when no
// requester is set.
func CalendarParticipantsForRequest(req Interview, activeChannel string, members []Member) []string {
	slugs := make([]string, 0, 2)
	if from := strings.TrimSpace(req.From); from != "" {
		slugs = append(slugs, from)
	}
	return CalendarParticipantNames(slugs, ChooseCalendarChannel(req.Channel, activeChannel), members, len(slugs) == 0)
}

// CalendarParticipantSlugsForRequest returns the slugs for the
// request's participants, falling back to the channel roster when no
// requester is set.
func CalendarParticipantSlugsForRequest(req Interview, activeChannel string, members []Member) []string {
	slugs := make([]string, 0, 2)
	if from := strings.TrimSpace(req.From); from != "" {
		slugs = append(slugs, from)
	}
	return CalendarParticipantSlugs(slugs, ChooseCalendarChannel(req.Channel, activeChannel), members, len(slugs) == 0)
}

// CalendarParticipantsForJob resolves a scheduler job's participants
// by following its target (task / request) when set, else falling back
// to the channel roster.
func CalendarParticipantsForJob(job SchedulerJob, tasks []Task, requests []Interview, activeChannel string, members []Member) []string {
	switch strings.TrimSpace(job.TargetType) {
	case "task":
		for _, task := range tasks {
			if task.ID == job.TargetID {
				return CalendarParticipantsForTask(task, activeChannel, members)
			}
		}
	case "request":
		for _, req := range requests {
			if req.ID == job.TargetID {
				return CalendarParticipantsForRequest(req, activeChannel, members)
			}
		}
	}
	channel := ChooseCalendarChannel(job.Channel, activeChannel)
	return CalendarParticipantNames(nil, channel, members, true)
}

// CalendarParticipantSlugsForJob resolves a scheduler job's
// participant slugs by following its target (task / request) when set,
// else falling back to the channel roster.
func CalendarParticipantSlugsForJob(job SchedulerJob, tasks []Task, requests []Interview, activeChannel string, members []Member) []string {
	switch strings.TrimSpace(job.TargetType) {
	case "task":
		for _, task := range tasks {
			if task.ID == job.TargetID {
				return CalendarParticipantSlugsForTask(task, activeChannel, members)
			}
		}
	case "request":
		for _, req := range requests {
			if req.ID == job.TargetID {
				return CalendarParticipantSlugsForRequest(req, activeChannel, members)
			}
		}
	}
	channel := ChooseCalendarChannel(job.Channel, activeChannel)
	return CalendarParticipantSlugs(nil, channel, members, true)
}

// CalendarParticipantNames resolves slugs to human names, sorted
// alphabetically and capped at four with "+N more". Falls back to the
// channel roster when fallbackToChannel is true and primary is empty.
func CalendarParticipantNames(primary []string, channel string, members []Member, fallbackToChannel bool) []string {
	slugs := CalendarParticipantSlugs(primary, channel, members, fallbackToChannel)
	var names []string
	for _, slug := range slugs {
		name := DisplayName(slug)
		for _, member := range members {
			if member.Slug == slug {
				if strings.TrimSpace(member.Name) != "" {
					name = member.Name
				}
				break
			}
		}
		names = append(names, name)
	}
	// Sort for stable display (slugs already sorted when fallback, but
	// names may differ from slug sort order)
	sort.Strings(names)
	if len(names) > 4 {
		return append(names[:4], fmt.Sprintf("+%d more", len(names)-4))
	}
	return names
}

// CalendarParticipantSlugs deduplicates primary slugs, optionally
// filling from the channel roster (sorted alphabetically) when
// fallbackToChannel is true and primary is empty.
func CalendarParticipantSlugs(primary []string, channel string, members []Member, fallbackToChannel bool) []string {
	seen := make(map[string]bool)
	var slugs []string
	addSlug := func(slug string) {
		slug = strings.TrimSpace(slug)
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}
	for _, slug := range primary {
		addSlug(slug)
	}
	if fallbackToChannel && len(slugs) == 0 {
		for _, member := range members {
			if member.Disabled {
				continue
			}
			addSlug(member.Slug)
		}
		// Sort alphabetically so order is stable across poll ticks
		sort.Strings(slugs)
	}
	return slugs
}

// NextCalendarEventByParticipant indexes the soonest event each
// participant has across the agenda. Participants with names starting
// with "+" (the "+N more" capped fold) are skipped.
func NextCalendarEventByParticipant(events []CalendarEvent) map[string]CalendarEvent {
	out := make(map[string]CalendarEvent)
	for _, event := range events {
		for _, participant := range event.Participants {
			if strings.HasPrefix(participant, "+") {
				continue
			}
			existing, ok := out[participant]
			if !ok || event.When.Before(existing.When) {
				out[participant] = event
			}
		}
	}
	return out
}

// OrderedCalendarParticipants returns the participant names from
// byParticipant sorted alphabetically. The members parameter is
// retained for compatibility — member-order rendering was deliberately
// replaced with alphabetical sort because poll ticks were shifting
// member order every refresh.
func OrderedCalendarParticipants(byParticipant map[string]CalendarEvent, members []Member) []string {
	_ = members
	var names []string
	for name := range byParticipant {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SchedulerTargetTaskID returns the scheduler job's target task ID
// when its TargetType is "task", "" otherwise.
func SchedulerTargetTaskID(job SchedulerJob) string {
	if strings.TrimSpace(job.TargetType) == "task" {
		return strings.TrimSpace(job.TargetID)
	}
	return ""
}

// SchedulerTargetRequestID returns the scheduler job's target request
// ID when its TargetType is "request", "" otherwise.
func SchedulerTargetRequestID(job SchedulerJob) string {
	if strings.TrimSpace(job.TargetType) == "request" {
		return strings.TrimSpace(job.TargetID)
	}
	return ""
}

// SchedulerTargetThreadID resolves a scheduler job's target thread by
// following its target task or request, returning "" when the target
// is not found or has no thread.
func SchedulerTargetThreadID(job SchedulerJob, tasks []Task, requests []Interview) string {
	switch strings.TrimSpace(job.TargetType) {
	case "task":
		for _, task := range tasks {
			if task.ID == job.TargetID {
				return strings.TrimSpace(task.ThreadID)
			}
		}
	case "request":
		for _, req := range requests {
			if req.ID == job.TargetID {
				return strings.TrimSpace(req.ReplyTo)
			}
		}
	}
	return ""
}
