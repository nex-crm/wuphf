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
