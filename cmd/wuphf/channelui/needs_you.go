package channelui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BuildNeedsYouLines renders the "needs your attention" strip for the
// most-deserving open interview from requests, or nil when nothing is
// open. Blocking / required requests get priority over plain pending
// ones; among ties, slice order wins (broker-supplied ordering).
func BuildNeedsYouLines(requests []Interview, contentWidth int) []RenderedLine {
	req, ok := SelectNeedsYouRequest(requests)
	if !ok {
		return nil
	}
	return BuildNeedsYouLinesForRequest(&req, contentWidth)
}

// BuildNeedsYouLinesForRequest renders the strip for a specific request.
// Useful when callers have already selected the request to show (e.g.,
// the recovery view spotlighting the active blocker).
func BuildNeedsYouLinesForRequest(req *Interview, contentWidth int) []RenderedLine {
	if req == nil {
		return nil
	}

	statusLabel := "needs your decision"
	if !(req.Blocking || req.Required) {
		statusLabel = "waiting on you"
	}
	header := AccentPill(statusLabel, "#B45309") + " " +
		lipgloss.NewStyle().Bold(true).Render(req.TitleOrQuestion())
	body := strings.TrimSpace(req.Context)
	if body == "" {
		body = strings.TrimSpace(req.Question)
	}
	extra := []string{"Asked by @" + FallbackString(req.From, "unknown")}
	if req.Blocking || req.Required {
		extra = append(extra, "The team is paused until you answer.")
	}
	if strings.TrimSpace(req.RecommendedID) != "" {
		extra = append(extra, "Recommended: "+req.RecommendedID)
	}
	if due := strings.TrimSpace(req.DueAt); due != "" {
		extra = append(extra, "Due "+PrettyRelativeTime(due))
	}
	extra = append(extra, "/request answer "+req.ID+" · /requests · /recover")

	lines := []RenderedLine{{Text: RenderDateSeparator(contentWidth, "Needs attention")}}
	for _, line := range RenderRuntimeEventCard(contentWidth, header, body, "#D97706", extra) {
		lines = append(lines, RenderedLine{Text: "  " + line, RequestID: req.ID})
	}
	return lines
}

// SelectNeedsYouRequest picks the most deserving open request: a
// blocking one if any exists, else the first plain open request.
func SelectNeedsYouRequest(requests []Interview) (Interview, bool) {
	for _, req := range requests {
		if !IsOpenInterviewStatus(req.Status) {
			continue
		}
		if req.Blocking || req.Required {
			return req, true
		}
	}
	for _, req := range requests {
		if IsOpenInterviewStatus(req.Status) {
			return req, true
		}
	}
	return Interview{}, false
}

// IsOpenInterviewStatus reports whether status reads as "open" by the
// broker. Blank / pending / open / draft are all treated as open since
// the broker uses these interchangeably across versions.
func IsOpenInterviewStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "open", "draft":
		return true
	default:
		return false
	}
}
