package team

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/operations"
)

func buildOperationStarterTemplate(blueprint operations.Blueprint, pack operationChannelPackDoc, backlog operationBacklogDoc, profile operationCompanyProfile) operationStarterTemplate {
	brandName := operationFirstResolvedNonEmpty(profile.Name, blueprint.BootstrapConfig.ChannelName, blueprint.Name, pack.Channel.BrandName, "Autonomous operation")
	niche := operationFirstResolvedNonEmpty(blueprint.BootstrapConfig.Niche, blueprint.Description, profile.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation")
	goals := operationFirstNonEmpty(
		strings.TrimSpace(profile.Goals),
		strings.TrimSpace(blueprint.Objective),
		"Stand up the first repeatable workflow, validate operator demand, and turn it into a durable operating asset.",
	)
	priority := operationFirstNonEmpty(
		strings.TrimSpace(profile.Priority),
		firstBacklogTitle(backlog),
		firstOperationStarterTaskTitle(blueprint.Starter.Tasks),
		"Stand up the first workflow lane and prove the office can run it with the right approvals.",
	)
	size := operationFirstNonEmpty(strings.TrimSpace(profile.Size), strings.TrimSpace(pack.Audience.TeamSize), "2-5")
	id := operationSlug(operationFirstNonEmpty(profile.BlueprintID, blueprint.ID, pack.Metadata.ID, brandName))
	if id == "" {
		id = "autonomous-operation"
	}
	commandSlug := operationSlug(brandName + " command")
	if commandSlug == "" {
		commandSlug = "command"
	}
	replacements := operationBootstrapTemplateReplacements(brandName, commandSlug, niche)
	starter := blueprint.Starter
	leadSlug := operationFirstNonEmpty(starter.LeadSlug, "ceo")
	return operationStarterTemplate{
		ID:     id,
		Kicker: "Starter plan",
		Name:   brandName,
		Badge:  "Operation template",
		Blurb:  operationFirstNonEmpty(blueprint.Description, profile.Description, pack.Channel.ShortBio, pack.Channel.Tagline, niche),
		Points: []operationStarterPoint{
			{Label: "Audience", Value: operationFirstNonEmpty(blueprint.BootstrapConfig.Audience, strings.TrimSpace(profile.Size), strings.Join(pack.Audience.PrimaryICP, ", "), "Operators and stakeholders")},
			{Label: "Cadence", Value: operationFirstNonEmpty(strings.TrimSpace(blueprint.BootstrapConfig.PublishingCadence), operationPublishingCadence(pack), "Weekly operating review")},
			{Label: "Value Capture", Value: operationFirstNonEmpty(strings.Join(blueprint.BootstrapConfig.MonetizationHooks, ", "), strings.Join(pack.OfferDefaults.RevenueLadder, ", "), "Approval-gated value capture")},
		},
		Defaults: operationStarterDefaults{
			Company:     brandName,
			Description: operationFirstNonEmpty(blueprint.Description, profile.Description, pack.Channel.ShortBio, pack.Channel.Tagline, niche),
			Goals:       goals,
			Priority:    priority,
			Size:        size,
		},
		Agents:         operationStarterAgentsFromBlueprint(starter.Agents, replacements),
		Channels:       operationStarterChannelsFromBlueprint(starter.Channels, replacements),
		Tasks:          operationStarterTasksFromBlueprint(starter.Tasks, replacements),
		KickoffTagged:  []string{leadSlug},
		KickoffMessage: operationRenderTemplateString(starter.KickoffPrompt, replacements),
		GeneralDesc:    operationRenderTemplateString(starter.GeneralChannelDescription, replacements),
	}
}

func firstOperationStarterTaskTitle(tasks []operations.StarterTask) string {
	for _, task := range tasks {
		if strings.TrimSpace(task.Title) != "" {
			return strings.TrimSpace(task.Title)
		}
	}
	return ""
}

func operationBootstrapTemplateReplacements(brandName, commandSlug, niche string) map[string]string {
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		brandName = "Autonomous operation"
	}
	commandSlug = operationSlug(commandSlug)
	if commandSlug == "" {
		commandSlug = "command"
	}
	return map[string]string{
		"brand_name":   brandName,
		"brand_slug":   operationSlug(brandName),
		"command_slug": commandSlug,
		"niche":        niche,
	}
}

func operationFirstResolvedNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "{{") && strings.Contains(value, "}}") {
			continue
		}
		return value
	}
	return ""
}

func operationStarterAgentsFromBlueprint(agents []operations.StarterAgent, replacements map[string]string) []operationStarterAgent {
	out := make([]operationStarterAgent, 0, len(agents))
	for _, agent := range agents {
		expertise := make([]string, 0, len(agent.Expertise))
		for _, item := range agent.Expertise {
			expertise = append(expertise, operationRenderTemplateString(item, replacements))
		}
		out = append(out, operationStarterAgent{
			Slug:           operationRenderTemplateString(agent.Slug, replacements),
			Emoji:          operationRenderTemplateString(agent.Emoji, replacements),
			Name:           operationRenderTemplateString(agent.Name, replacements),
			Role:           operationRenderTemplateString(agent.Role, replacements),
			Checked:        agent.Checked,
			Type:           operationRenderTemplateString(agent.Type, replacements),
			PermissionMode: agent.PermissionMode,
			BuiltIn:        agent.BuiltIn,
			Expertise:      expertise,
			Personality:    operationRenderTemplateString(agent.Personality, replacements),
		})
	}
	return out
}

func operationStarterChannelsFromBlueprint(channels []operations.StarterChannel, replacements map[string]string) []operationStarterChannel {
	out := make([]operationStarterChannel, 0, len(channels))
	for _, channel := range channels {
		members := make([]string, 0, len(channel.Members))
		for _, member := range channel.Members {
			members = append(members, operationRenderTemplateString(member, replacements))
		}
		out = append(out, operationStarterChannel{
			Slug:        operationRenderTemplateString(channel.Slug, replacements),
			Name:        operationRenderTemplateString(channel.Name, replacements),
			Description: operationRenderTemplateString(channel.Description, replacements),
			Members:     members,
		})
	}
	return out
}

func operationStarterTasksFromBlueprint(tasks []operations.StarterTask, replacements map[string]string) []operationStarterTask {
	out := make([]operationStarterTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, operationStarterTask{
			Channel: operationRenderTemplateString(task.Channel, replacements),
			Owner:   operationRenderTemplateString(task.Owner, replacements),
			Title:   operationRenderTemplateString(task.Title, replacements),
			Details: operationRenderTemplateString(task.Details, replacements),
		})
	}
	return out
}

func firstBacklogTitle(backlog operationBacklogDoc) string {
	for _, episode := range backlog.Episodes {
		title := strings.TrimSpace(episode.WorkingTitle)
		if title != "" {
			return title
		}
	}
	return ""
}
