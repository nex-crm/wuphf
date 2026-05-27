package team

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/company"
)

type slackCommandContext struct {
	UserID      string
	UserName    string
	ChannelID   string
	ChannelName string
	ChannelSlug string
	ThreadTS    string
}

func (s *SlackTransport) dispatchSlackbotCommand(ctx context.Context, c slackCommandContext, text string) string {
	if s.Broker == nil {
		return "WUPHF broker is not available."
	}
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) > 0 && strings.EqualFold(fields[0], "wuphf") {
		fields = fields[1:]
	}
	if len(fields) == 0 || fields[0] == "help" {
		return slackHelpText()
	}
	switch fields[0] {
	case "agent", "agents":
		return s.dispatchSlackAgentCommand(ctx, fields[1:])
	case "channel", "channels":
		return s.dispatchSlackChannelCommand(fields[1:], c)
	case "wiki":
		return s.dispatchSlackWikiCommand(ctx, fields[1:], c)
	case "inbox", "task", "tasks":
		return s.dispatchSlackTaskCommand(fields)
	case "config", "status":
		return s.dispatchSlackStatusCommand()
	default:
		return "Unknown WUPHF command. Try `/wuphf help`."
	}
}

func slackHelpText() string {
	return strings.Join([]string{
		"*WUPHF commands*",
		"`/wuphf agent list`",
		"`/wuphf agent create <slug> <name...>`",
		"`/wuphf agent slack-app <slug>`",
		"`/wuphf channel list`",
		"`/wuphf channel mirror <wuphf-channel>`",
		"`/wuphf wiki search <query>`",
		"`/wuphf wiki read <team/path.md>`",
		"`/wuphf inbox`",
		"`/wuphf status`",
	}, "\n")
}

func (s *SlackTransport) dispatchSlackAgentCommand(ctx context.Context, args []string) string {
	if len(args) == 0 || args[0] == "list" {
		members := sortedOfficeMembers(s.Broker.OfficeMembers())
		var lines []string
		lines = append(lines, "*Agents*")
		for _, member := range members {
			if member.Slug == "" || isHumanMessageSender(member.Slug) {
				continue
			}
			role := member.Role
			if role == "" {
				role = member.Name
			}
			lines = append(lines, fmt.Sprintf("• `@%s` — %s", member.Slug, role))
		}
		return strings.Join(lines, "\n")
	}
	if args[0] == "slack-app" {
		if len(args) < 2 {
			return "Usage: `/wuphf agent slack-app <slug>`."
		}
		result, err := s.Broker.CreateSlackAgentApp(ctx, args[1], "")
		if err != nil {
			return "Slack agent app creation failed: " + err.Error()
		}
		if result.OAuthAuthorizeURL != "" {
			return fmt.Sprintf("Created Slack AI app for `@%s`: %s", result.Slug, result.OAuthAuthorizeURL)
		}
		return fmt.Sprintf("Created Slack AI app for `@%s` (`%s`).", result.Slug, result.AppID)
	}
	if args[0] != "create" {
		return "Usage: `/wuphf agent list`, `/wuphf agent create <slug> <name...>`, or `/wuphf agent slack-app <slug>`."
	}
	if len(args) < 3 {
		return "Usage: `/wuphf agent create <slug> <name...>`."
	}
	slug := normalizeActorSlug(args[1])
	name := strings.TrimSpace(strings.Join(args[2:], " "))
	if slug == "" || name == "" {
		return "Agent slug and name are required."
	}
	body := officeMemberMutationBody{
		Action:    "create",
		Slug:      slug,
		Name:      name,
		Role:      name,
		CreatedBy: "slack",
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/slack/agent-create", nil)
	if err != nil {
		return "Failed to create request: " + err.Error()
	}
	s.Broker.officeMemberMutationMu.Lock()
	result, mutationErr := s.Broker.createOfficeMember(req, slug, body)
	s.Broker.officeMemberMutationMu.Unlock()
	if mutationErr != nil {
		return mutationErr.message
	}
	if err := s.Broker.writeBrokerState(result.write); err != nil {
		return "Failed to persist agent: " + err.Error()
	}
	s.Broker.publishOfficeChanges(result.events)
	if result.ensureNotebookDirs {
		s.Broker.ensureNotebookDirsForRoster()
	}
	return fmt.Sprintf("Created agent `@%s`.", slug)
}

func (s *SlackTransport) dispatchSlackChannelCommand(args []string, c slackCommandContext) string {
	if len(args) == 0 || args[0] == "list" {
		channels := s.Broker.Channels()
		var lines []string
		lines = append(lines, "*WUPHF channels*")
		for _, ch := range channels {
			if ch.isDM() {
				continue
			}
			suffix := ""
			if ch.Surface != nil && ch.Surface.Provider == slackAdapterName {
				suffix = " — mirrored to Slack"
			}
			lines = append(lines, fmt.Sprintf("• `#%s`%s", ch.Slug, suffix))
		}
		return strings.Join(lines, "\n")
	}
	if args[0] != "mirror" {
		return "Usage: `/wuphf channel list` or `/wuphf channel mirror <wuphf-channel>`."
	}
	if c.ChannelID == "" {
		return "Slack channel id is missing; cannot mirror this channel."
	}
	slug := ""
	if len(args) > 1 {
		slug = args[1]
	}
	if strings.TrimSpace(slug) == "" {
		slug = c.ChannelName
	}
	ch, err := s.Broker.createOrUpdateSlackChannel(slug, firstNonEmpty(c.ChannelName, c.ChannelID), c.ChannelID)
	if err != nil {
		return "Mirror failed: " + err.Error()
	}
	s.setSlackChannelMap(c.ChannelID, ch.Slug)
	return fmt.Sprintf("Mirroring this Slack channel to WUPHF `#%s`.", ch.Slug)
}

func (s *SlackTransport) dispatchSlackWikiCommand(ctx context.Context, args []string, c slackCommandContext) string {
	if len(args) < 2 {
		return "Usage: `/wuphf wiki search <query>` or `/wuphf wiki read <team/path.md>`."
	}
	switch args[0] {
	case "search":
		query := strings.TrimSpace(strings.Join(args[1:], " "))
		idx := s.Broker.WikiIndex()
		if idx == nil {
			return "Wiki index is not ready."
		}
		hits, err := idx.Search(ctx, query, 5)
		if err != nil {
			return "Wiki search failed: " + err.Error()
		}
		if len(hits) == 0 {
			return "No wiki results."
		}
		lines := []string{"*Wiki results*"}
		for _, hit := range hits {
			label := firstNonEmpty(hit.Entity, hit.FactID)
			if hit.Snippet != "" {
				lines = append(lines, fmt.Sprintf("• `%s` — %.2f\n  %s", label, hit.Score, truncateSlackText(hit.Snippet, 180)))
			} else {
				lines = append(lines, fmt.Sprintf("• `%s` — %.2f", label, hit.Score))
			}
		}
		return strings.Join(lines, "\n")
	case "read":
		path := strings.TrimSpace(strings.Join(args[1:], " "))
		if err := validateArticlePath(path); err != nil {
			return "Invalid wiki path: " + err.Error()
		}
		worker := s.Broker.WikiWorker()
		if worker == nil {
			return "Wiki backend is not active."
		}
		bytes, err := readArticle(worker.Repo(), filepath.ToSlash(path))
		if err != nil {
			return "Wiki read failed: " + err.Error()
		}
		_ = c
		return "```" + truncateSlackText(string(bytes), 2600) + "```"
	default:
		return "Usage: `/wuphf wiki search <query>` or `/wuphf wiki read <team/path.md>`."
	}
}

func (s *SlackTransport) dispatchSlackTaskCommand(_ []string) string {
	tasks := s.Broker.AllTasks()
	if len(tasks) == 0 {
		return "No active WUPHF tasks."
	}
	lines := []string{"*Tasks*"}
	for i, task := range tasks {
		if i >= 10 {
			lines = append(lines, "…")
			break
		}
		status := (&task).Status()
		if status == "" {
			status = "pending"
		}
		lines = append(lines, fmt.Sprintf("• `%s` — %s (%s)", task.ID, task.Title, status))
	}
	return strings.Join(lines, "\n")
}

func (s *SlackTransport) dispatchSlackStatusCommand() string {
	channels := s.Broker.SurfaceChannels(slackAdapterName)
	cfg, _ := company.SnapshotManifest()
	lead := strings.TrimSpace(cfg.Lead)
	if lead == "" {
		lead = "ceo"
	}
	return fmt.Sprintf("WUPHF Slack is connected. Mirrored channels: %d. Team lead: `@%s`.", len(channels), lead)
}
