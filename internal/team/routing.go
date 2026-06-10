package team

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/orchestration"
)

const officeRoutingMatchThreshold = 0.28

func (l *Launcher) scoreMessageForAgent(msg channelMessage, slug string) float64 {
	member := l.officeMemberBySlug(slug)
	if strings.TrimSpace(member.Slug) == "" {
		return 0
	}
	return orchestration.ScoreMessageAgainstTerms(messageRoutingText(msg), officeMemberRoutingTerms(member))
}

func (l *Launcher) messageTargetsAgent(msg channelMessage, slug string) bool {
	return l.scoreMessageForAgent(msg, slug) >= officeRoutingMatchThreshold
}

func (l *Launcher) taskOwnerForMessage(msg channelMessage) string {
	if l == nil || l.broker == nil {
		return ""
	}
	// Channel-per-task: in the post-restructure model every task owns its own
	// channel (`task-<id>`), so the message's channel deterministically names
	// the task. Match on it first — a short human chat ("actually, target
	// SMBs") rarely clears the content-similarity threshold, but it must still
	// wake the owning task's owner. The content-scoring pass below stays for
	// legacy shared channels (e.g. #general) where several tasks share one.
	if rawMsgCh := strings.TrimSpace(msg.Channel); rawMsgCh != "" {
		msgChannel := normalizeChannelSlug(rawMsgCh)
		matchedOwner := ""
		ambiguous := false
		for _, task := range l.broker.AllTasks() {
			st := strings.ToLower(strings.TrimSpace(task.status))
			// Skip terminal tasks: an unset channel normalizes to "general",
			// and #general is owned by the archived Backup & Migration task —
			// general office chat there must keep routing normally, not wake
			// that task's owner.
			if st == "done" || st == "archived" {
				continue
			}
			taskOwner := strings.TrimSpace(task.Owner)
			if taskOwner == "" {
				continue
			}
			// Guard on the RAW task channel being non-empty so a channel-less
			// task does not bind to every #general message.
			if rawTaskCh := strings.TrimSpace(task.Channel); rawTaskCh != "" &&
				normalizeChannelSlug(rawTaskCh) == msgChannel {
				// Accumulate matches instead of returning the first one: a
				// legacy shared channel can hold tasks owned by DIFFERENT
				// agents, and binding to whichever was scanned first wakes the
				// wrong owner. Trust the channel binding only when every match
				// resolves to the same owner; otherwise mark it ambiguous and
				// fall through to content-scoring below.
				if matchedOwner == "" {
					matchedOwner = taskOwner
				} else if matchedOwner != taskOwner {
					ambiguous = true
				}
			}
		}
		if matchedOwner != "" && !ambiguous {
			return matchedOwner
		}
	}
	var owner string
	bestScore := 0.0
	for _, task := range l.broker.AllTasks() {
		if strings.EqualFold(strings.TrimSpace(task.status), "done") {
			continue
		}
		taskOwner := strings.TrimSpace(task.Owner)
		if taskOwner == "" {
			continue
		}
		score := l.scoreMessageForTaskCandidate(msg, task)
		if score < officeRoutingMatchThreshold {
			continue
		}
		if owner == "" || score > bestScore {
			owner = taskOwner
			bestScore = score
		}
	}
	return owner
}

func messageRoutingText(msg channelMessage) string {
	return strings.TrimSpace(msg.Title + " " + msg.Content)
}

func officeMemberRoutingTerms(member officeMember) []string {
	return orchestration.RoutingTerms(member.Slug, member.Expertise, officeMemberRoleTerms(member), nil)
}

func officeMemberRoleTerms(member officeMember) []string {
	terms := make([]string, 0, 4)
	if role := strings.TrimSpace(member.Role); role != "" {
		terms = append(terms, role)
	}
	if name := strings.TrimSpace(member.Name); name != "" {
		terms = append(terms, name)
	}
	return terms
}

func taskRoutingTerms(task teamTask) []string {
	return orchestration.RoutingTerms(task.Owner, nil, nil, []string{task.Title, task.Details, task.Channel})
}

func (l *Launcher) scoreMessageForTask(msg channelMessage, task teamTask) float64 {
	return orchestration.ScoreMessageAgainstTerms(messageRoutingText(msg), taskRoutingTerms(task))
}

func (l *Launcher) scoreMessageForTaskCandidate(msg channelMessage, task teamTask) float64 {
	score := l.scoreMessageForTask(msg, task)
	if ownerScore := l.scoreMessageForAgent(msg, strings.TrimSpace(task.Owner)); ownerScore > score {
		return ownerScore
	}
	return score
}
