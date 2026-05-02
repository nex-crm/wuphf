package team

import (
	"strings"
	"time"
)

func (b *Broker) ChannelTasks(channel string) []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	out := make([]teamTask, 0, len(b.tasks))
	for _, task := range b.tasks {
		if normalizeChannelSlug(task.Channel) == channel {
			out = append(out, task)
		}
	}
	return out
}

// AllTasks returns a copy of all tasks across all channels. Use this when the
// caller needs to search across channels rather than in a single known channel.
func (b *Broker) AllTasks() []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, len(b.tasks))
	copy(out, b.tasks)
	return out
}

// InFlightTasks returns tasks that have an assigned owner and a non-terminal
// status (anything except "done", "completed", "canceled", or "cancelled").
func (b *Broker) InFlightTasks() []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, 0)
	for _, task := range b.tasks {
		if task.Owner == "" {
			continue
		}
		s := strings.ToLower(strings.TrimSpace(task.Status))
		if s == "done" || s == "completed" || s == "canceled" || s == "cancelled" {
			continue
		}
		out = append(out, task)
	}
	return out
}

// UnackedTasks returns in_progress tasks with an owner that have not been acked
// and were created more than the given duration ago.
func (b *Broker) UnackedTasks(timeout time.Duration) []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := time.Now().UTC().Add(-timeout)
	out := make([]teamTask, 0)
	for _, task := range b.tasks {
		if task.Status != "in_progress" || task.Owner == "" || task.AckedAt != "" {
			continue
		}
		created, err := time.Parse(time.RFC3339, task.CreatedAt)
		if err != nil {
			continue
		}
		if created.Before(cutoff) {
			out = append(out, task)
		}
	}
	return out
}
