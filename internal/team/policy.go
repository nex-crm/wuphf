package team

import (
	"fmt"
	"strings"
)

type officeSignal struct {
	ID            string
	Source        string
	Kind          string
	Title         string
	Content       string
	Confidence    string
	Urgency       string
	Channel       string
	Owner         string
	RequiresHuman bool
	Blocking      bool
}

type officeActionPlan struct {
	Summary  string
	Tagged   []string
	Tasks    []insightTaskPlan
	Requests []humanInterview
}

func buildInsightSignals(insights []nexInsight) []officeSignal {
	important := selectImportantInsights(insights)
	signals := make([]officeSignal, 0, len(important))
	for _, insight := range important {
		content := strings.TrimSpace(insight.Content)
		if content == "" {
			continue
		}
		if hint := strings.TrimSpace(insight.Target.Hint); hint != "" {
			content += " (" + hint + ")"
		}
		requiresHuman, blocking := signalNeedsHuman(content, insight.Type)
		signals = append(signals, officeSignal{
			ID:            insight.ID,
			Source:        "nex_insights",
			Kind:          strings.TrimSpace(insight.Type),
			Title:         "Nex insight",
			Content:       content,
			Confidence:    strings.TrimSpace(insight.ConfidenceLevel),
			Urgency:       inferUrgency(content, insight.Type),
			Channel:       "general",
			Owner:         inferInsightOwner(insight),
			RequiresHuman: requiresHuman,
			Blocking:      blocking,
		})
	}
	return signals
}

func buildNotificationSignals(items []nexFeedItem) []officeSignal {
	signals := make([]officeSignal, 0, len(items))
	for _, item := range items {
		title, content := formatNexFeedItem(item)
		if strings.TrimSpace(content) == "" {
			continue
		}
		requiresHuman, blocking := signalNeedsHuman(content, item.Type)
		signals = append(signals, officeSignal{
			ID:            item.ID,
			Source:        "nex_notifications",
			Kind:          strings.TrimSpace(item.Type),
			Title:         title,
			Content:       content,
			Urgency:       inferUrgency(content, item.Type),
			Channel:       "general",
			Owner:         inferOwnerFromText(title + " " + content),
			RequiresHuman: requiresHuman,
			Blocking:      blocking,
		})
	}
	return signals
}

func planOfficeActions(signals []officeSignal) officeActionPlan {
	plan := officeActionPlan{}
	if len(signals) == 0 {
		return plan
	}

	lines := []string{"Nex surfaced a few things worth acting on:"}
	seenOwners := map[string]struct{}{}
	seenTasks := map[string]struct{}{}
	for _, signal := range signals {
		line := "- " + signal.Content
		if signal.Urgency != "" {
			line += " [" + signal.Urgency + "]"
		}
		lines = append(lines, line)
		if signal.Owner != "" {
			if _, ok := seenOwners[signal.Owner]; !ok {
				plan.Tagged = append(plan.Tagged, signal.Owner)
				seenOwners[signal.Owner] = struct{}{}
			}
			key := strings.ToLower(strings.TrimSpace(signal.Owner + "::" + truncate(signal.Content, 80)))
			if _, exists := seenTasks[key]; !exists {
				seenTasks[key] = struct{}{}
				plan.Tasks = append(plan.Tasks, insightTaskPlan{
					Owner:   signal.Owner,
					Title:   fmt.Sprintf("Follow up on Nex signal: %s", truncate(strings.TrimSpace(signal.Content), 72)),
					Details: buildSignalTaskDetails(signal),
				})
			}
		}
		if signal.RequiresHuman {
			plan.Requests = append(plan.Requests, humanInterview{
				Kind:      requestKindForSignal(signal),
				Status:    "pending",
				From:      "ceo",
				Channel:   signal.Channel,
				Title:     signalRequestTitle(signal),
				Question:  signalQuestion(signal),
				Context:   signal.Content,
				Blocking:  signal.Blocking,
				Required:  true,
				Options:   signalRequestOptions(signal),
				CreatedAt: "",
			})
		}
	}
	if len(plan.Tasks) > 0 {
		lines = append(lines, "", "I opened tasks for the right owners so we do not dogpile this.")
	}
	if len(plan.Requests) > 0 {
		lines = append(lines, "Some of this needs a human call, so I also opened a request instead of guessing.")
	}
	plan.Summary = strings.Join(lines, "\n")
	return plan
}

func buildSignalTaskDetails(signal officeSignal) string {
	parts := []string{strings.TrimSpace(signal.Content)}
	if signal.Source != "" {
		parts = append(parts, "Source: "+signal.Source)
	}
	if signal.Kind != "" {
		parts = append(parts, "Kind: "+signal.Kind)
	}
	if signal.Confidence != "" {
		parts = append(parts, "Confidence: "+signal.Confidence)
	}
	if signal.Urgency != "" {
		parts = append(parts, "Urgency: "+signal.Urgency)
	}
	return strings.Join(parts, "\n")
}

func signalRequestTitle(signal officeSignal) string {
	switch requestKindForSignal(signal) {
	case "approval":
		return "Approval needed"
	case "choice":
		return "Decision needed"
	default:
		return "Human input needed"
	}
}

func signalQuestion(signal officeSignal) string {
	switch requestKindForSignal(signal) {
	case "approval":
		return "Should the office act on this now?"
	case "choice":
		return "What should the office optimize for here?"
	default:
		return "How should the office proceed?"
	}
}

func signalRequestOptions(signal officeSignal) []interviewOption {
	switch requestKindForSignal(signal) {
	case "approval":
		return []interviewOption{
			{ID: "act_now", Label: "Act now", Description: "Proceed and let the team handle it immediately."},
			{ID: "hold", Label: "Hold", Description: "Pause action until there is more context."},
		}
	case "choice":
		return []interviewOption{
			{ID: "speed", Label: "Move fast", Description: "Bias toward momentum and follow-up speed."},
			{ID: "careful", Label: "Be careful", Description: "Bias toward caution and a tighter review loop."},
		}
	default:
		return nil
	}
}

func requestKindForSignal(signal officeSignal) string {
	text := strings.ToLower(strings.TrimSpace(signal.Content + " " + signal.Kind))
	switch {
	case signal.Blocking:
		return "approval"
	case strings.Contains(text, "choose"), strings.Contains(text, "decision"), strings.Contains(text, "which"), strings.Contains(text, "priorit"):
		return "choice"
	default:
		return "approval"
	}
}

func signalNeedsHuman(content, kind string) (requiresHuman bool, blocking bool) {
	text := strings.ToLower(strings.TrimSpace(content + " " + kind))
	switch {
	case strings.Contains(text, "approval"), strings.Contains(text, "approve"), strings.Contains(text, "legal"), strings.Contains(text, "security review"), strings.Contains(text, "permission"), strings.Contains(text, "contract"):
		return true, true
	case strings.Contains(text, "should we"), strings.Contains(text, "choose"), strings.Contains(text, "decision"), strings.Contains(text, "confirm"):
		return true, false
	default:
		return false, false
	}
}

func inferUrgency(content, kind string) string {
	text := strings.ToLower(strings.TrimSpace(content + " " + kind))
	switch {
	case strings.Contains(text, "urgent"), strings.Contains(text, "critical"), strings.Contains(text, "blocked"), strings.Contains(text, "freeze"), strings.Contains(text, "outage"):
		return "high"
	case strings.Contains(text, "risk"), strings.Contains(text, "budget"), strings.Contains(text, "delay"), strings.Contains(text, "follow up"), strings.Contains(text, "action"):
		return "medium"
	default:
		return "normal"
	}
}

func inferOwnerFromText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(text, "sales"), strings.Contains(text, "pipeline"), strings.Contains(text, "pricing"), strings.Contains(text, "revenue"), strings.Contains(text, "budget"):
		return "cro"
	case strings.Contains(text, "campaign"), strings.Contains(text, "positioning"), strings.Contains(text, "brand"), strings.Contains(text, "marketing"), strings.Contains(text, "launch"):
		return "cmo"
	case strings.Contains(text, "design"), strings.Contains(text, "hero"), strings.Contains(text, "landing"), strings.Contains(text, "ui"):
		return "designer"
	case strings.Contains(text, "frontend"), strings.Contains(text, "signup"), strings.Contains(text, "web"):
		return "fe"
	case strings.Contains(text, "backend"), strings.Contains(text, "database"), strings.Contains(text, "api"), strings.Contains(text, "integration"):
		return "be"
	case strings.Contains(text, "ai"), strings.Contains(text, "llm"), strings.Contains(text, "model"), strings.Contains(text, "transcript"):
		return "ai"
	default:
		return "pm"
	}
}
