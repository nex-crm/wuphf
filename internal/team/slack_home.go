package team

// slack_home.go renders the wuphf Slack app's App Home tab: a native Block
// Kit overview of the office — surface buttons into the web app, the task
// board by lane (one card-like section per task, mirroring the web board),
// and the wiki rendered as real links instead of raw markdown — published via
// views.publish whenever a user opens the app's Home tab (app_home_opened).
//
// Requires the Slack app config to have the Home Tab enabled (App Home →
// Show Tabs → Home) and the app_home_opened bot event subscribed.

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slackHomeMaxTasksPerLane caps each board lane so the Home view stays
// glanceable (and under Slack's 100-block view limit).
const slackHomeMaxTasksPerLane = 5

// Wiki preview card bounds. slackHomeMaxWikiCards: how many of the freshest
// articles get a card. slackHomeWikiTeaserLen: teaser length cap.
// slackHomeWikiNewWindow: an article updated within this window wears a 🆕 New
// badge — these articles only change when the office learns something, so a
// recent update is a genuine "fresh knowledge" signal.
const (
	slackHomeMaxWikiCards  = 4
	slackHomeWikiTeaserLen = 150
	slackHomeWikiNewWindow = 48 * time.Hour
)

// slackHomeLinksBlockID names the surface-buttons actions block. It MUST stay
// distinct from slackGateActionBlock: handleInteractive ignores foreign block
// ids, which is what makes URL buttons here safe no-ops on click.
const slackHomeLinksBlockID = "wuphf_home_links"

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
	// Bound the network call: this runs on the inbound event goroutine, so a
	// hung Slack call without a local deadline would stall event processing.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := t.api.PublishViewContext(ctx, userID, view); err != nil {
		log.Printf("[slack] home tab publish failed for %s: %v", userID, err)
	}
}

// buildSlackHomeBlocks renders the office overview. Every dynamic field is
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
	contextLine := func(md string) {
		blocks = append(blocks, slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType, md, false, false)))
	}
	divider := func() { blocks = append(blocks, slack.NewDividerBlock()) }

	base := b.WebURL()

	header("WUPHF Office")
	contextLine("One office, every agent — a live overview that refreshes each time you open this tab.")

	// Primary surfaces as native buttons; secondary surfaces as a link line.
	// Self-hosted: the URL is loopback, reachable on the machine running the
	// office.
	if base != "" {
		button := func(actionID, label, url string) *slack.ButtonBlockElement {
			el := slack.NewButtonBlockElement(actionID, "",
				slack.NewTextBlockObject(slack.PlainTextType, label, true, false))
			el.URL = url
			return el
		}
		blocks = append(blocks, slack.NewActionBlock(slackHomeLinksBlockID,
			button("wuphf_home_tasks", "📋 Task board", base+"/tasks"),
			button("wuphf_home_wiki", "📚 Wiki", base+"/wiki"),
			button("wuphf_home_inbox", "📥 Inbox", base+"/inbox"),
			button("wuphf_home_agents", "🤖 Agents", base+"/agents"),
		))
		contextLine(fmt.Sprintf("More: <%s/notebooks|Notebooks> · <%s/reviews|Reviews> · <%s/routines|Routines> · <%s|Everything else>",
			base, base, base, base))
	}
	divider()

	// Task board — one card-like section per task, grouped by lane.
	header("Task board")
	lanes := []struct {
		title    string
		statuses map[string]bool
	}{
		{"🔨 In progress", map[string]bool{
			"running": true, "in_progress": true,
		}},
		{"🗂 Up next & in review", map[string]bool{
			"intake": true, "ready": true, "planning": true, "pending": true,
			"open": true, "assigned": true, "drafting": true,
			"review": true, "decision": true, "changes_requested": true,
			"blocked_on_pr_merge": true, "queued_behind_owner": true,
		}},
		{"✅ Done recently", map[string]bool{
			"done": true, "completed": true, "approved": true,
		}},
	}
	tasks := b.AllTasks()
	// Newest first so "recent" lanes show the latest work.
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].CreatedAt > tasks[j].CreatedAt })
	for _, lane := range lanes {
		var shown, overflow int
		var lines []string
		for i := range tasks {
			task := &tasks[i]
			// Seeded/system work (legacy "task-…" ids) is office plumbing,
			// not business work — keep it off the glanceable board.
			if strings.HasPrefix(task.ID, "task-") {
				continue
			}
			state := slackTaskCardState(task)
			if !lane.statuses[state] {
				continue
			}
			if shown >= slackHomeMaxTasksPerLane {
				overflow++
				continue
			}
			shown++
			owner := strings.TrimSpace(task.Owner)
			if owner == "" {
				owner = "unassigned"
			}
			id := slackEscape(task.ID)
			ref := id
			if base != "" {
				ref = fmt.Sprintf("<%s/tasks/%s|%s>", base, task.ID, id)
			}
			lines = append(lines, fmt.Sprintf("*%s*  %s\n        `%s` · %s",
				ref, slackEscape(task.Title), slackEscape(state), slackEscape(owner)))
		}
		section("*" + lane.title + "*")
		if len(lines) == 0 {
			contextLine("Nothing here right now.")
			continue
		}
		section(strings.Join(lines, "\n"))
		if overflow > 0 {
			if base != "" {
				contextLine(fmt.Sprintf("…and %d more on the <%s/tasks|full board>.", overflow, base))
			} else {
				contextLine(fmt.Sprintf("…and %d more on the full board.", overflow))
			}
		}
	}
	divider()

	// Wiki — preview cards for the freshest articles (title + a teaser
	// snippet from the body), never the raw index markdown (Slack cannot
	// resolve its relative links).
	header("Wiki")
	contextLine("The office brain — agents read and write these articles as they work.")
	var entries []slackWikiIndexEntry
	var repo *Repo
	if worker := b.WikiWorker(); worker != nil {
		repo = worker.Repo()
		if indexMD, err := readIndexAll(repo); err == nil {
			entries = parseSlackWikiIndex(string(indexMD))
		}
	}
	if len(entries) == 0 {
		contextLine("_No articles yet._")
	} else {
		for _, card := range renderSlackWikiCards(entries, repo, base, time.Now().UTC()) {
			section(card)
		}
		footer := fmt.Sprintf("%s in the wiki.", countNoun(len(entries), "article", "articles"))
		if base != "" {
			footer = fmt.Sprintf("%s · <%s/wiki|Browse them all>.", countNoun(len(entries), "article", "articles"), base)
		}
		contextLine(footer)
	}

	return blocks
}

// slackWikiIndexEntry is one parsed line of index/all.md.
type slackWikiIndexEntry struct {
	Group   string // the "## team/people" heading the entry sits under
	Label   string
	Path    string // wiki-relative path, e.g. "team/people/nazz.md"
	Updated string // RFC3339 from the index's "(updated …)" suffix; sortable lexically
}

// slackWikiIndexLine matches the index's "- [Label](../path) _(updated …)_"
// entry lines; the timestamp group is optional.
var slackWikiIndexLine = regexp.MustCompile(`^- \[(.+?)\]\((.+?)\)(?:.*?updated ([0-9TZ:.+-]+))?`)

// parseSlackWikiIndex extracts structured entries from the auto-generated
// index/all.md markdown.
func parseSlackWikiIndex(md string) []slackWikiIndexEntry {
	var out []slackWikiIndexEntry
	group := "articles"
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if h, ok := strings.CutPrefix(line, "## "); ok {
			group = strings.TrimSpace(h)
			continue
		}
		m := slackWikiIndexLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, slackWikiIndexEntry{
			Group:   group,
			Label:   strings.TrimSpace(m[1]),
			Path:    strings.TrimLeft(strings.TrimSpace(m[2]), "./"),
			Updated: strings.TrimSpace(m[3]),
		})
	}
	return out
}

// renderSlackWikiCards renders the freshest articles as preview cards built to
// pull a reader in: a linked title with a 🆕 New badge when fresh, a meta line
// that promises substance (fact count · freshness · group), and a teaser drawn
// from the article's real observations rather than its generated boilerplate.
// With a web URL the title deep-links into the app's wiki viewer; without one
// it renders plain. now is injected so freshness is deterministic in tests.
func renderSlackWikiCards(entries []slackWikiIndexEntry, repo *Repo, base string, now time.Time) []string {
	sorted := append([]slackWikiIndexEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Updated > sorted[j].Updated })
	if len(sorted) > slackHomeMaxWikiCards {
		sorted = sorted[:slackHomeMaxWikiCards]
	}
	var out []string
	for _, e := range sorted {
		title := slackEscape(e.Label)
		if base != "" && e.Path != "" {
			title = fmt.Sprintf("<%s/wiki/%s|%s>", base, e.Path, slackEscape(e.Label))
		}
		head := "📄 *" + title + "*"
		if slackWikiIsNew(e.Updated, now) {
			head += "   🆕 *New*"
		}

		// Read the body once for both the fact count and the teaser.
		var body string
		if repo != nil && e.Path != "" {
			if raw, err := readArticle(repo, e.Path); err == nil {
				body = string(raw)
			}
		}

		var meta []string
		if facts := slackWikiFactCount(body); facts > 0 {
			meta = append(meta, "🧠 "+countNoun(facts, "fact", "facts"))
		}
		if rel := slackRelativeTime(e.Updated, now); rel != "" {
			meta = append(meta, "updated "+rel)
		}
		if e.Group != "" {
			meta = append(meta, "`"+slackEscape(e.Group)+"`")
		}

		card := head
		if len(meta) > 0 {
			card += "\n" + strings.Join(meta, "  ·  ")
		}
		if teaser := slackWikiTeaser(body); teaser != "" {
			card += "\n" + teaser
		}
		out = append(out, card)
	}
	return out
}

// slackWikiIsNew reports whether an article's last update is recent enough to
// wear a New badge. A parse failure or future timestamp is treated as not new.
func slackWikiIsNew(updated string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		return false
	}
	d := now.Sub(t)
	return d >= -time.Minute && d < slackHomeWikiNewWindow
}

// slackRelativeTime renders an article's update time as a human "… ago" phrase
// ("just now", "3h ago", "yesterday", "5d ago"). Empty when unparseable.
func slackRelativeTime(updated string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		// Future or clock-skewed timestamp — "just now" would mislabel a
		// genuinely-newer article. Treat it as the freshest state plainly.
		return "just updated"
	case d < time.Hour:
		return "just now"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// slackWikiFactCount reports how many facts back an article: the entity-article
// frontmatter count when present, else a fallback bullet count. Zero when the
// article carries no countable substance.
func slackWikiFactCount(body string) int {
	if m := slackWikiFactCountRe.FindStringSubmatch(body); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	n := 0
	for _, line := range strings.Split(stripFrontmatter(body), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			n++
		}
	}
	return n
}

// slackWikiTeaser extracts a concrete teaser to entice a click: the article's
// real observations (the curated facts), never the generated intro line ("X is
// a person in the team knowledge graph"). Falls back to the first substantive
// prose paragraph for human-authored articles with no Observations section.
func slackWikiTeaser(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	lines := strings.Split(stripFrontmatter(body), "\n")

	var observations []string
	inObservations := false
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "## ") {
			inObservations = strings.EqualFold(l, "## Observations")
			continue
		}
		if inObservations && strings.HasPrefix(l, "- ") {
			observations = append(observations, cleanSlackWikiTeaserLine(strings.TrimPrefix(l, "- ")))
			if len(observations) >= 2 {
				break
			}
		}
	}
	teaser := strings.Join(observations, " · ")

	if teaser == "" {
		for _, line := range lines {
			l := strings.TrimSpace(line)
			switch {
			case l == "", strings.HasPrefix(l, "#"), strings.HasPrefix(l, "<!--"),
				strings.HasPrefix(l, ":"), strings.HasPrefix(l, "|"),
				strings.HasPrefix(l, "["), slackWikiBoilerplateLine(l):
				continue
			}
			teaser = cleanSlackWikiTeaserLine(l)
			break
		}
	}
	if teaser == "" {
		return ""
	}
	if runes := []rune(teaser); len(runes) > slackHomeWikiTeaserLen {
		teaser = strings.TrimSpace(string(runes[:slackHomeWikiTeaserLen])) + "…"
	}
	return "> " + teaser
}

// cleanSlackWikiTeaserLine strips footnote markers and markdown emphasis that
// read as noise in a one-line teaser.
func cleanSlackWikiTeaserLine(s string) string {
	s = slackWikiFootnoteRef.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "*", "")
	return strings.TrimSpace(s)
}

// slackWikiBoilerplateLine flags the generated entity-article intro sentence so
// the prose fallback skips it ("Nazz is a person in the team knowledge graph,
// with 4 recorded facts.").
func slackWikiBoilerplateLine(l string) bool {
	low := strings.ToLower(l)
	return strings.Contains(low, "in the team knowledge graph") ||
		strings.Contains(low, "recorded fact")
}

// slackWikiFootnoteRef matches markdown footnote markers like [^1].
var slackWikiFootnoteRef = regexp.MustCompile(`\[\^\d+\]`)

// slackWikiFactCountRe pulls the entity-article frontmatter fact count.
var slackWikiFactCountRe = regexp.MustCompile(`(?m)^fact_count_at_synthesis:\s*(\d+)`)
