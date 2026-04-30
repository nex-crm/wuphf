package main

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Picker option builders. These are pure functions on channelModel that
// project state into the option list a TUI picker renders. They are
// extracted from channel.go because they have no side effects, no state
// mutation, and a sharp single responsibility — the canonical "epicenter
// testable" shape.
//
// Adding a new picker? Add it here, alongside its kin.

func (m channelModel) buildThreadPickerOptions() []tui.PickerOption {
	// Find root messages with replies.
	replyCount := make(map[string]int)
	for _, msg := range m.messages {
		if msg.ReplyTo != "" {
			replyCount[msg.ReplyTo]++
		}
	}

	var options []tui.PickerOption
	for _, msg := range m.messages {
		count, hasReplies := replyCount[msg.ID]
		if !hasReplies || msg.ReplyTo != "" {
			continue // skip non-root or messages without replies
		}

		preview := channelui.TruncateText(msg.Content, 50)
		status := "collapsed"
		if m.expandedThreads[msg.ID] {
			status = "expanded"
		}

		options = append(options, tui.PickerOption{
			Label:       fmt.Sprintf("@%s: %s", msg.From, preview),
			Value:       msg.ID,
			Description: fmt.Sprintf("%d replies · %s", count, status),
		})
	}
	return options
}

func (m channelModel) buildRequestPickerOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(m.requests))
	for _, req := range m.requests {
		if req.Channel != "" && req.Channel != m.activeChannel {
			continue
		}
		if req.Status != "" && req.Status != "pending" && req.Status != "open" {
			continue
		}
		label := req.Question
		if strings.TrimSpace(req.Title) != "" {
			label = req.Title
		}
		desc := fmt.Sprintf("%s from @%s", req.Kind, req.From)
		if req.Blocking {
			desc += " · blocking"
		}
		options = append(options, tui.PickerOption{
			Label:       channelui.TruncateText(label, 56),
			Value:       req.ID,
			Description: desc,
		})
	}
	return options
}

func (m channelModel) buildTaskPickerOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(m.tasks))
	for _, task := range m.tasks {
		taskChannel := strings.ToLower(strings.TrimSpace(task.Channel))
		if taskChannel == "" {
			taskChannel = "general"
		}
		if taskChannel != strings.ToLower(strings.TrimSpace(m.activeChannel)) {
			continue
		}
		label := task.Title
		if strings.TrimSpace(task.Owner) != "" {
			label = fmt.Sprintf("%s · %s", task.Title, channelui.DisplayName(task.Owner))
		}
		desc := task.Status
		if task.ThreadID != "" {
			desc += " · thread " + task.ThreadID
		}
		options = append(options, tui.PickerOption{
			Label:       channelui.TruncateText(label, 56),
			Value:       task.ID,
			Description: desc,
		})
	}
	return options
}

func (m channelModel) buildTaskActionPickerOptions(task channelui.Task) []tui.PickerOption {
	options := []tui.PickerOption{
		{Label: "Claim task", Value: "claim:" + task.ID, Description: "Take ownership as you"},
		{Label: "Release task", Value: "release:" + task.ID, Description: "Clear the current owner"},
	}
	if task.ReviewState == "ready_for_review" || task.Status == "review" {
		options = append(options, tui.PickerOption{Label: "Approve task", Value: "approve:" + task.ID, Description: "Mark this review-ready task done"})
	} else if task.ReviewState == "pending_review" || task.ExecutionMode == "local_worktree" {
		options = append(options, tui.PickerOption{Label: "Ready for review", Value: "complete:" + task.ID, Description: "Move this task into review"})
	} else {
		options = append(options, tui.PickerOption{Label: "Complete task", Value: "complete:" + task.ID, Description: "Mark this task done"})
	}
	if task.Status != "done" {
		options = append(options, tui.PickerOption{Label: "Block task", Value: "block:" + task.ID, Description: "Mark this work blocked"})
	}
	if task.ThreadID != "" {
		options = append(options, tui.PickerOption{Label: "Open thread", Value: "open:" + task.ID, Description: "Jump to the thread for this task"})
	}
	return options
}

func (m channelModel) buildRequestActionPickerOptions(req channelui.Interview) []tui.PickerOption {
	dismissDescription := "Cancel this request"
	if req.Blocking || req.Required {
		dismissDescription = "Cancel this request and unblock the team"
	}
	options := []tui.PickerOption{
		{Label: "Focus request", Value: "focus:" + req.ID, Description: "Open this request in the app"},
		{Label: "Answer request", Value: "answer:" + req.ID, Description: "Bring it into the composer"},
		{Label: "Dismiss request", Value: "dismiss:" + req.ID, Description: dismissDescription},
	}
	if req.ReplyTo != "" {
		options = append(options, tui.PickerOption{Label: "Open thread", Value: "open:" + req.ID, Description: "Jump to the related thread"})
	}
	return options
}
