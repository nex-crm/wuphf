// launcher_nex.go — Nex-specific types and functions used by the team launcher.
//
// FORKING NOTE: Everything in this file is Nex CRM-specific. If you are building
// a fork that does not use Nex, delete this file and remove the calls to:
//   - pollNexNotificationsLoop (in launcher.go Launch())
//   - pollNexInsightsLoop      (in launcher.go Launch())
//
// That is the complete surface area; no other files need changes.
package team

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
)

// ---------------------------------------------------------------------------
// Nex feed / notification types
// ---------------------------------------------------------------------------

type nexFeedItemContentItem struct {
	Title         string `json:"title"`
	Context       string `json:"context"`
	EstimatedTime string `json:"estimated_time"`
}

type nexFeedItemContent struct {
	ImportantItems []nexFeedItemContentItem `json:"important_items"`
	EntityChanges  []nexFeedItemContentItem `json:"entity_changes"`
}

type nexFeedItem struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Status    string             `json:"status"`
	AlertTime string             `json:"alert_time"`
	SentAt    string             `json:"sent_at"`
	Content   nexFeedItemContent `json:"content"`
}

type nexFeedResponse struct {
	Items []nexFeedItem `json:"items"`
}

// ---------------------------------------------------------------------------
// Nex insights types
// ---------------------------------------------------------------------------

type nexInsight struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Content         string `json:"content"`
	ConfidenceLevel string `json:"confidence_level"`
	CreatedAt       string `json:"created_at"`
	Target          struct {
		Hint       string `json:"hint"`
		EntityType string `json:"entity_type"`
	} `json:"target"`
}

type nexInsightsEnvelope struct {
	Insights []nexInsight `json:"insights"`
}

type insightTaskPlan struct {
	Owner   string
	Title   string
	Details string
}

// ---------------------------------------------------------------------------
// Nex notification polling
// ---------------------------------------------------------------------------

func (l *Launcher) pollNexNotificationsLoop() {
	if l.broker == nil {
		return
	}
	if !shouldPollNexNotifications() {
		return
	}
	apiKey := config.ResolveAPIKey("")
	if apiKey == "" {
		return
	}
	client := api.NewClient(apiKey)
	interval := notificationPollInterval()

	time.Sleep(10 * time.Second)
	for {
		l.updateSchedulerJob("nex-notifications", "Nex notifications", interval, time.Now().UTC(), "running")
		l.fetchAndIngestNexNotifications(client)
		l.updateSchedulerJob("nex-notifications", "Nex notifications", interval, time.Now().UTC().Add(interval), "sleeping")
		time.Sleep(interval)
	}
}

func notificationPollInterval() time.Duration {
	if raw := os.Getenv("WUPHF_NOTIFY_INTERVAL_MINUTES"); raw != "" {
		if minutes, err := strconv.Atoi(raw); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	if raw := os.Getenv("NEX_NOTIFY_INTERVAL_MINUTES"); raw != "" {
		if minutes, err := strconv.Atoi(raw); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	return defaultNotificationPollInterval
}

func (l *Launcher) fetchAndIngestNexNotifications(client *api.Client) {
	if l.broker == nil {
		return
	}
	if l.broker.NotificationCursor() == "" {
		// Cold starts should not replay old feed history into a fresh office.
		// Seed the cursor at "now" and only surface notifications that arrive after launch.
		_ = l.broker.SetNotificationCursor(time.Now().UTC().Format(time.RFC3339Nano))
		return
	}

	params := url.Values{}
	params.Set("limit", "10")
	if since := l.broker.NotificationCursor(); since != "" {
		params.Set("since", since)
	}

	result, err := api.Get[nexFeedResponse](client, "/v1/notifications/feed?"+params.Encode(), 15*time.Second)
	if err != nil {
		return
	}
	if len(result.Items) == 0 {
		return
	}

	latest := l.broker.NotificationCursor()
	for _, item := range result.Items {
		if item.SentAt != "" && (latest == "" || item.SentAt > latest) {
			latest = item.SentAt
		}
	}
	if latest != "" {
		_ = l.broker.SetNotificationCursor(latest)
	}
}

func formatNexFeedItem(item nexFeedItem) (string, string) {
	title := humanizeNotificationType(item.Type)
	var lines []string

	for _, important := range item.Content.ImportantItems {
		line := strings.TrimSpace(important.Title)
		if important.Context != "" {
			line += " — " + strings.TrimSpace(important.Context)
		}
		if important.EstimatedTime != "" {
			line += " (" + strings.TrimSpace(important.EstimatedTime) + ")"
		}
		if line != "" {
			lines = append(lines, "Important: "+line)
		}
	}
	for _, change := range item.Content.EntityChanges {
		line := strings.TrimSpace(change.Title)
		if change.Context != "" {
			line += " — " + strings.TrimSpace(change.Context)
		}
		if line != "" {
			lines = append(lines, "Change: "+line)
		}
	}

	if title == "" && len(lines) > 0 {
		title = "Context alert"
	}

	return title, strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Nex insights polling
// ---------------------------------------------------------------------------

func (l *Launcher) pollNexInsightsLoop() {
	if l.broker == nil {
		return
	}
	apiKey := config.ResolveAPIKey("")
	if apiKey == "" {
		return
	}
	client := api.NewClient(apiKey)
	interval := time.Duration(config.ResolveInsightsPollInterval()) * time.Minute

	time.Sleep(20 * time.Second)
	for {
		l.updateSchedulerJob("nex-insights", "Nex insights", interval, time.Now().UTC(), "running")
		l.fetchAndPostNexInsights(client)
		l.updateSchedulerJob("nex-insights", "Nex insights", interval, time.Now().UTC().Add(interval), "sleeping")
		time.Sleep(interval)
	}
}

func (l *Launcher) fetchAndPostNexInsights(client *api.Client) {
	if l.broker == nil {
		return
	}
	now := time.Now().UTC()
	if l.broker.InsightsCursor() == "" {
		_ = l.broker.SetInsightsCursor(now.Format(time.RFC3339Nano))
		return
	}

	params := url.Values{}
	params.Set("from", l.broker.InsightsCursor())
	params.Set("to", now.Format(time.RFC3339Nano))
	params.Set("limit", "20")

	raw, err := client.GetRaw("/v1/insights?"+params.Encode(), 20*time.Second)
	if err != nil {
		return
	}
	_ = parseInsightsResponse(raw) // parse and discard — CEO sees Nex context via MCP, not signal machinery
	_ = l.broker.SetInsightsCursor(now.Format(time.RFC3339Nano))
}

func parseInsightsResponse(raw string) []nexInsight {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var envelope nexInsightsEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && len(envelope.Insights) > 0 {
		return envelope.Insights
	}
	var direct []nexInsight
	if err := json.Unmarshal([]byte(raw), &direct); err == nil {
		return direct
	}
	return nil
}

func selectImportantInsights(insights []nexInsight) []nexInsight {
	var important []nexInsight
	for _, insight := range insights {
		confidence := strings.ToLower(strings.TrimSpace(insight.ConfidenceLevel))
		kind := strings.ToLower(strings.TrimSpace(insight.Type))
		if confidence == "high" || confidence == "very_high" || strings.Contains(kind, "risk") || strings.Contains(kind, "opportun") {
			important = append(important, insight)
		}
	}
	if len(important) == 0 && len(insights) > 0 {
		important = append(important, insights...)
	}
	if len(important) > 3 {
		important = important[:3]
	}
	return important
}

func summarizeInsightsForCEO(insights []nexInsight) (string, []string, []insightTaskPlan) {
	lines := []string{"Nex surfaced a few things that look worth acting on:"}
	var tagged []string
	var tasks []insightTaskPlan
	seenOwners := map[string]struct{}{}
	for _, insight := range insights {
		text := strings.TrimSpace(insight.Content)
		if text == "" {
			continue
		}
		if hint := strings.TrimSpace(insight.Target.Hint); hint != "" {
			text += " (" + hint + ")"
		}
		lines = append(lines, "- "+text)
		owner := inferInsightOwner(insight)
		if owner != "" {
			if _, ok := seenOwners[owner]; !ok {
				tagged = append(tagged, owner)
				seenOwners[owner] = struct{}{}
			}
			tasks = append(tasks, insightTaskPlan{
				Owner:   owner,
				Title:   fmt.Sprintf("Follow up on Nex insight: %s", truncate(strings.TrimSpace(insight.Content), 72)),
				Details: text,
			})
		}
	}
	if len(tasks) > 0 {
		lines = append(lines, "", "I opened tasks for the right owners so we do not dogpile this.")
	}
	return strings.Join(lines, "\n"), tagged, tasks
}

func inferInsightOwner(insight nexInsight) string {
	text := strings.ToLower(strings.TrimSpace(insight.Content + " " + insight.Type + " " + insight.Target.Hint + " " + insight.Target.EntityType))
	switch {
	case strings.Contains(text, "pipeline"), strings.Contains(text, "deal"), strings.Contains(text, "revenue"), strings.Contains(text, "budget"), strings.Contains(text, "pricing"):
		return "cro"
	case strings.Contains(text, "campaign"), strings.Contains(text, "brand"), strings.Contains(text, "position"), strings.Contains(text, "marketing"), strings.Contains(text, "launch"):
		return "cmo"
	case strings.Contains(text, "design"), strings.Contains(text, "landing"), strings.Contains(text, "hero"), strings.Contains(text, "ui"):
		return "designer"
	case strings.Contains(text, "frontend"), strings.Contains(text, "web"), strings.Contains(text, "signup"):
		return "fe"
	case strings.Contains(text, "backend"), strings.Contains(text, "api"), strings.Contains(text, "database"), strings.Contains(text, "integration"):
		return "be"
	case strings.Contains(text, "ai"), strings.Contains(text, "llm"), strings.Contains(text, "transcript"), strings.Contains(text, "notes"), strings.Contains(text, "retrieval"):
		return "ai"
	default:
		return "pm"
	}
}
