package team

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
)

// onboardingCompleteFn is invoked by the onboarding package when the user
// finishes the wizard. It seeds the default team (idempotent — no-op if a
// team already exists), posts the user's first task to #general as a human
// message tagged to the office lead, and lets the existing launcher trigger
// the lead's delegate turn.
//
// blueprintID and selectedAgents are threaded from the wizard via the POST
// body. blueprintID is the curated blueprint the user picked (empty for
// "from scratch"). selectedAgents is the user's agent-toggle state (nil for
// back-compat clients that don't send it, or internal synthesis callers).
// Cycle 3 of the plan will consume these to seed the real blueprint team;
// for now they are captured but not yet used — the seeding path is
// unchanged from previous behavior.
//
// Side effects happen BEFORE the onboarding package writes the completion
// flag to disk, so a crash between this call returning and the flag write
// re-enters the wizard — and the dedupe guard below prevents double-posting.
func (b *Broker) onboardingCompleteFn(task string, skipTask bool, blueprintID string, selectedAgents []string) error {
	_ = blueprintID
	_ = selectedAgents
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.blankSlateLaunch {
		if err := b.seedBlankSlateOperationLocked(task, skipTask); err != nil {
			return err
		}
		return b.saveLocked()
	}

	// Seed default team if none is configured yet. This mirrors the path
	// taken on first boot (see ensureDefaultOfficeMembersLocked).
	b.ensureDefaultOfficeMembersLocked()

	// Skip-task path: team seeded, no first message. Caller marks onboarded.
	if skipTask {
		return b.saveLocked()
	}

	task = strings.TrimSpace(task)
	if task == "" {
		return fmt.Errorf("onboarding: task is required when skip_task=false")
	}

	// Dedupe: if a prior onboarding-complete already posted this exact task
	// (recognized via the onboarding_origin marker in Kind), skip re-posting.
	for _, existing := range b.messages {
		if existing.Channel == "general" && existing.Kind == "onboarding_origin" && existing.Content == task {
			return b.saveLocked()
		}
	}

	lead := officeLeadSlugFrom(b.members)
	if lead == "" {
		lead = defaultOfficeLeadForLaunchMode()
	}

	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "human",
		Channel:   "general",
		Kind:      "onboarding_origin",
		Content:   task,
		Tagged:    []string{lead},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)

	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}
	b.lastTaggedAt[lead] = time.Now()

	return b.saveLocked()
}

func defaultOfficeLeadForLaunchMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WUPHF_START_FROM_SCRATCH"))) {
	case "1", "true", "yes":
		return "founder"
	default:
		return "ceo"
	}
}

func (b *Broker) seedBlankSlateOperationLocked(task string, skipTask bool) error {
	state, err := onboarding.Load()
	if err != nil {
		return err
	}
	profile := operationCompanyProfile{
		Name:        strings.TrimSpace(state.CompanyName),
		Description: onboardingPartialString(state.Partial, "welcome", "desc"),
		Goals:       strings.TrimSpace(task),
		Size:        onboardingPartialString(state.Partial, "welcome", "size"),
		Priority:    onboardingPartialString(state.Partial, "welcome", "priority"),
	}
	blueprint := operations.SynthesizeBlueprint(operations.SynthesisInput{
		Directive:    profile.Goals,
		Profile:      operations.CompanyProfile{Name: profile.Name, Description: profile.Description, Audience: profile.Size, Offer: profile.Goals},
		Description:  profile.Description,
		Goals:        profile.Goals,
		Size:         profile.Size,
		Priority:     profile.Priority,
		Integrations: nil,
		Capabilities: nil,
	})
	b.members = blankSlateOfficeMembersFromBlueprint(blueprint)
	if len(b.members) == 0 {
		b.members = defaultOfficeMembers()
	}
	b.channels = blankSlateOfficeChannelsFromBlueprint(blueprint, b.members)
	b.tasks = blankSlateOfficeTasksFromBlueprint(blueprint)
	if len(b.channels) == 0 {
		b.channels = []teamChannel{{
			Slug:        "general",
			Name:        "general",
			Description: "Primary coordination channel for the blank-slate office.",
			Members:     memberSlugsFromMembers(b.members),
		}}
	}
	b.messages = nil
	b.counter = 0
	b.lastTaggedAt = make(map[string]time.Time)
	return b.postBlankSlateKickoffLocked(profile, blueprint, task, skipTask)
}

func (b *Broker) postBlankSlateKickoffLocked(profile operationCompanyProfile, blueprint operations.Blueprint, task string, skipTask bool) error {
	if skipTask {
		return nil
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return fmt.Errorf("onboarding: task is required when skip_task=false")
	}
	lead := officeLeadSlugFromMembers(b.members)
	if lead == "" {
		lead = "operator"
	}
	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "human",
		Channel:   "general",
		Kind:      "onboarding_origin",
		Content:   task,
		Tagged:    []string{lead},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}
	b.lastTaggedAt[lead] = time.Now()
	if strings.TrimSpace(blueprint.Name) != "" {
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "system",
			Channel:   "general",
			Kind:      "synthesized_blueprint",
			Content:   fmt.Sprintf("Synthesized operation: %s (%s)", blueprint.Name, blueprint.Kind),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   "general",
		Kind:      "from_scratch_contract",
		Content:   "Run this as a real business workflow. If a needed specialist, channel, skill, or tooling path is missing, create it and keep going. Local proof packets, review bundles, and other internal substitute artifacts do not count when a live business step is possible.",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	_ = profile
	return nil
}

func onboardingPartialString(partial *onboarding.PartialProgress, step, key string) string {
	if partial == nil {
		return ""
	}
	answers := partial.Answers[strings.TrimSpace(step)]
	if len(answers) == 0 {
		return ""
	}
	if value, ok := answers[strings.TrimSpace(key)]; ok {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func blankSlateOfficeMembersFromBlueprint(blueprint operations.Blueprint) []officeMember {
	agents := blueprint.Starter.Agents
	members := make([]officeMember, 0, len(agents))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, agent := range agents {
		slug := normalizeChannelSlug(operationFirstNonEmpty(agent.Slug, agent.EmployeeBlueprint, operationSlug(agent.Name)))
		if slug == "" {
			continue
		}
		name := strings.TrimSpace(agent.Name)
		if name == "" {
			name = humanizeSlug(slug)
		}
		role := strings.TrimSpace(agent.Role)
		if role == "" {
			role = name
		}
		members = append(members, officeMember{
			Slug:           slug,
			Name:           name,
			Role:           role,
			Expertise:      normalizeStringList(agent.Expertise),
			Personality:    strings.TrimSpace(agent.Personality),
			PermissionMode: blankSlatePermissionMode(agent.Type),
			AllowedTools:   nil,
			CreatedBy:      "wuphf",
			CreatedAt:      now,
			BuiltIn:        agent.BuiltIn || slug == "operator" || slug == "founder" || slug == "ceo",
		})
	}
	if len(members) > 0 {
		return members
	}
	return []officeMember{
		{Slug: "founder", Name: "Founder", Role: "Founder", PermissionMode: "plan", BuiltIn: true, CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "operator", Name: "Operator", Role: "Operator", PermissionMode: "auto", BuiltIn: true, CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "builder", Name: "Builder", Role: "Builder", PermissionMode: "auto", CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "reviewer", Name: "Reviewer", Role: "Reviewer", PermissionMode: "plan", CreatedBy: "wuphf", CreatedAt: now},
	}
}

func blankSlateOfficeChannelsFromBlueprint(blueprint operations.Blueprint, members []officeMember) []teamChannel {
	replacements := map[string]string{
		"brand_name": operationFirstNonEmpty(blueprint.Name, "New operation"),
		"brand_slug": operationSlug(operationFirstNonEmpty(blueprint.Name, "new-operation")),
	}
	now := time.Now().UTC().Format(time.RFC3339)
	lead := officeLeadSlugFromMembers(members)
	channels := []teamChannel{{
		Slug:        "general",
		Name:        "general",
		Description: operationRenderTemplateString(blueprint.Starter.GeneralChannelDescription, replacements),
		Members:     memberSlugsFromMembers(members),
		CreatedBy:   "wuphf",
		CreatedAt:   now,
		UpdatedAt:   now,
	}}
	for _, starter := range blueprint.Starter.Channels {
		slug := normalizeChannelSlug(operationRenderTemplateString(starter.Slug, replacements))
		if slug == "" || slug == "general" {
			continue
		}
		membersList := make([]string, 0, len(starter.Members))
		for _, member := range starter.Members {
			memberSlug := normalizeChannelSlug(operationRenderTemplateString(member, replacements))
			if memberSlug != "" {
				membersList = append(membersList, memberSlug)
			}
		}
		channels = append(channels, teamChannel{
			Slug:        slug,
			Name:        operationRenderTemplateString(starter.Name, replacements),
			Description: operationRenderTemplateString(starter.Description, replacements),
			Members:     uniqueSlugs(append([]string{lead}, membersList...)),
			CreatedBy:   "wuphf",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	return channels
}

func blankSlateOfficeTasksFromBlueprint(blueprint operations.Blueprint) []teamTask {
	now := time.Now().UTC().Format(time.RFC3339)
	tasks := make([]teamTask, 0, len(blueprint.Starter.Tasks))
	for i, starter := range blueprint.Starter.Tasks {
		channel := normalizeChannelSlug(starter.Channel)
		if channel == "" {
			channel = "general"
		}
		owner := normalizeChannelSlug(starter.Owner)
		tasks = append(tasks, teamTask{
			ID:        fmt.Sprintf("blank-slate-%d", i+1),
			Channel:   channel,
			Title:     strings.TrimSpace(starter.Title),
			Details:   strings.TrimSpace(starter.Details),
			Owner:     owner,
			Status:    "open",
			CreatedBy: "wuphf",
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	return tasks
}

func blankSlatePermissionMode(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "lead", "human":
		return "plan"
	default:
		return "auto"
	}
}

func memberSlugsFromMembers(members []officeMember) []string {
	out := make([]string, 0, len(members))
	for _, member := range members {
		if slug := strings.TrimSpace(member.Slug); slug != "" {
			out = append(out, slug)
		}
	}
	return uniqueSlugs(out)
}

func officeLeadSlugFromMembers(members []officeMember) string {
	for _, member := range members {
		if member.BuiltIn {
			return strings.TrimSpace(member.Slug)
		}
	}
	if len(members) > 0 {
		return strings.TrimSpace(members[0].Slug)
	}
	return ""
}
