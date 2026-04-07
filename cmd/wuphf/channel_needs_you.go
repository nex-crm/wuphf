package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func buildNeedsYouLines(requests []channelInterview, contentWidth int) []renderedLine {
	req, ok := selectNeedsYouRequest(requests)
	if !ok {
		return nil
	}

	statusLabel := "needs your decision"
	if !(req.Blocking || req.Required) {
		statusLabel = "waiting on you"
	}
	header := accentPill(statusLabel, "#B45309") + " " +
		lipgloss.NewStyle().Bold(true).Render(req.TitleOrQuestion())
	body := strings.TrimSpace(req.Context)
	if body == "" {
		body = strings.TrimSpace(req.Question)
	}
	extra := []string{"Asked by @" + fallbackString(req.From, "unknown")}
	if req.Blocking || req.Required {
		extra = append(extra, "The team is paused until you answer.")
	}
	if strings.TrimSpace(req.RecommendedID) != "" {
		extra = append(extra, "Recommended: "+req.RecommendedID)
	}
	if due := strings.TrimSpace(req.DueAt); due != "" {
		extra = append(extra, "Due "+prettyRelativeTime(due))
	}
	extra = append(extra, "/request answer "+req.ID+" · /requests · /recover")

	lines := []renderedLine{{Text: renderDateSeparator(contentWidth, "Needs attention")}}
	for _, line := range renderRuntimeEventCard(contentWidth, header, body, "#D97706", extra) {
		lines = append(lines, renderedLine{Text: "  " + line, RequestID: req.ID})
	}
	return lines
}

func selectNeedsYouRequest(requests []channelInterview) (channelInterview, bool) {
	for _, req := range requests {
		if !isOpenInterviewStatus(req.Status) {
			continue
		}
		if req.Blocking || req.Required {
			return req, true
		}
	}
	for _, req := range requests {
		if isOpenInterviewStatus(req.Status) {
			return req, true
		}
	}
	return channelInterview{}, false
}

func isOpenInterviewStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "open", "draft":
		return true
	default:
		return false
	}
}
