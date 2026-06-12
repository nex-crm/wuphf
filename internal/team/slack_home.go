package team

// slack_home.go renders the wuphf Slack app's App Home tab: a native overview
// of the office — task board by lane, the team wiki index, and links to every
// web-app surface — published via views.publish whenever a user opens the
// app's Home tab (app_home_opened). Slack cannot embed the web app, so the
// Home tab is a glanceable summary with deep links into the real surfaces.
//
// Requires the Slack app config to have the Home Tab enabled (App Home →
// Show Tabs → Home) and the app_home_opened bot event subscribed.

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/slack-go/slack"
)

// slackHomeMaxTasksPerLane caps each board lane so the Home view stays
// glanceable (and under Slack's 100-block view limit).
const slackHomeMaxTasksPerLane = 5

// slackHomeWikiIndexCap bounds how much of the wiki index markdown is shown.
// Slack section text tops out at 3000 chars; stay safely below.
const slackHomeWikiIndexCap = 2200

// publishHomeTab builds and publishes the App Home view for userID.
// Best-effort: failures are logged, never fatal — the Home tab is a viewport,
// not a control surface.
func (t *SlackTransport) publishHomeTab(ctx context.Context, userID string) {
	if t.api == nil || t.Broker == nil || strings.TrimSpace(userID) == "" {
		return
	}
	blocks := buildSlackHomeBlocks(t.Broker)
	view := slack.HomeTabViewRequest{
		Type:   slack.VTHomeTab,
		Blocks: slack.Blocks{BlockSet: blocks},
	}
	if err := t.api.PublishViewContext(ctx, userID, view); err != nil {
		log.Printf("[slack] home tab publish failed for %s: %v", userID, err)
	}
}

// buildSlackHomeBlocks renders the office overview: web-app surface links,
// the task board grouped by lane, and the wiki index. Every dynamic field is
// escaped; links are built only from the broker's own WebURL.
func buildSlackHomeBlocks(b *Broker) []slack.Block {
	var blocks []slack.Block
	section := func(md string) {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, md, false, false), nil, nil))
	}
	header := func(text string) {
		blocks = append(blocks, slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, text, false, false)))
	}

	header("WUPHF Office")

	// Web-app surfaces. Self-hosted: the URL is loopback, reachable on the
	// machine running the office.
	if base := b.WebURL(); base != "" {
		section(fmt.Sprintf(
			"*Open in the app:* <%s/tasks|Task board> · <%s/wiki|Wiki> · <%s/inbox|Inbox> · <%s/agents|Agents> · <%s/notebooks|Notebooks> · <%s/reviews|Reviews> · <%s/routines|Routines> · <%s|Settings & more>",
			base, base, base, base, base, base, base, base))
	}
	blocks = append(blocks, slack.NewDividerBlock())

	// Task board by lane.
	header("Task board")
	lanes := []struct {
		title    string
		statuses map[string]bool
	}{
		{"In progress", map[string]bool{"in_progress": true, "running": true}},
		{"Planning / review", map[string]bool{"planning": true, "review": true, "pending": true, "open": true, "assigned": true}},
		{"Done (recent)", map[string]bool{"done": true, "completed": true}},
	}
	tasks := b.AllTasks()
	// Newest first so "recent" lanes show the latest work.
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].CreatedAt > tasks[j].CreatedAt })
	for _, lane := range lanes {
		var lines []string
		overflow := 0
		for i := range tasks {
			task := &tasks[i]
			status := strings.ToLower(strings.TrimSpace(task.status))
			if !lane.statuses[status] {
				continue
			}
			if len(lines) >= slackHomeMaxTasksPerLane {
				overflow++
				continue
			}
			owner := strings.TrimSpace(task.Owner)
			if owner == "" {
				owner = "unassigned"
			}
			lines = append(lines, fmt.Sprintf("• `%s` %s — _%s_",
				slackEscape(task.ID), slackEscape(task.Title), slackEscape(owner)))
		}
		body := "_empty_"
		if len(lines) > 0 {
			body = strings.Join(lines, "\n")
			if overflow > 0 {
				body += fmt.Sprintf("\n_…and %d more_", overflow)
			}
		}
		section(fmt.Sprintf("*%s*\n%s", lane.title, body))
	}
	blocks = append(blocks, slack.NewDividerBlock())

	// Wiki: the curated index is already human-readable markdown.
	header("Wiki")
	if worker := b.WikiWorker(); worker != nil {
		if indexMD, err := readIndexAll(worker.Repo()); err == nil {
			section(slackHomeTrim(string(indexMD), slackHomeWikiIndexCap))
		} else {
			section("_wiki index unavailable_")
		}
	} else {
		section("_wiki backend is not active_")
	}

	return blocks
}

// slackHomeTrim caps s at max runes-ish (byte cap is fine for display) and
// marks the truncation.
func slackHomeTrim(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "_empty_"
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n_…truncated — open the Wiki in the app for the rest_"
}
