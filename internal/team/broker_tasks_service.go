package team

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var errTaskChannelAccessDenied = errors.New("task channel access denied")
var errTaskAckInvalid = errors.New("id and slug required")
var errTaskAckOwnerOnly = errors.New("only the task owner can ack")
var errTaskNotFound = errors.New("task not found")
var errTaskPersistFailed = errors.New("failed to persist")

func (b *Broker) ListTasks(req TaskListRequest) (TaskListResponse, error) {
	statusFilter := strings.TrimSpace(req.StatusFilter)
	mySlug := strings.TrimSpace(req.MySlug)
	viewerSlug := strings.TrimSpace(req.ViewerSlug)
	channel := normalizeChannelSlug(req.Channel)
	allChannels := req.AllChannels
	includeDone := req.IncludeDone

	b.mu.Lock()
	defer b.mu.Unlock()

	if !allChannels && !b.canAccessChannelLocked(viewerSlug, channel) {
		return TaskListResponse{}, errTaskChannelAccessDenied
	}

	result := make([]teamTask, 0, len(b.tasks))
	// allChannels=true must NOT bypass channel authorization. Without this
	// per-task check, an authenticated viewer could enumerate every task in
	// every channel, including private ones they aren't a member of,
	// just by passing all_channels=true. Apply the same access predicate
	// to each candidate channel before letting the task into the response.
	allChannelsCache := make(map[string]bool)
	channelAllowed := func(slug string) bool {
		if !allChannels {
			return true
		}
		if v, ok := allChannelsCache[slug]; ok {
			return v
		}
		v := b.canAccessChannelLocked(viewerSlug, slug)
		allChannelsCache[slug] = v
		return v
	}
	for _, task := range b.tasks {
		taskChannel := normalizeChannelSlug(task.Channel)
		if !allChannels && taskChannel != channel {
			continue
		}
		if !channelAllowed(taskChannel) {
			continue
		}
		if task.status == "done" && !includeDone && statusFilter == "" {
			continue
		}
		if statusFilter != "" && task.status != statusFilter {
			continue
		}
		if mySlug != "" && task.Owner != "" && task.Owner != mySlug {
			continue
		}
		result = append(result, task)
	}

	return TaskListResponse{Channel: channel, Tasks: result}, nil
}

func (b *Broker) AckTask(req TaskAckRequest) (TaskResponse, error) {
	taskID := strings.TrimSpace(req.ID)
	slug := strings.TrimSpace(req.Slug)
	channel := normalizeChannelSlug(req.Channel)
	if channel == "" {
		channel = "general"
	}
	if taskID == "" || slug == "" {
		return TaskResponse{}, errTaskAckInvalid
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == taskID && normalizeChannelSlug(b.tasks[i].Channel) == channel {
			if b.tasks[i].Owner != slug {
				return TaskResponse{}, errTaskAckOwnerOnly
			}
			now := time.Now().UTC().Format(time.RFC3339)
			b.tasks[i].AckedAt = now
			b.tasks[i].UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				return TaskResponse{}, fmt.Errorf("%w: %w", errTaskPersistFailed, err)
			}
			return TaskResponse{Task: b.tasks[i]}, nil
		}
	}
	return TaskResponse{}, errTaskNotFound
}
