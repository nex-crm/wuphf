package team

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
)

// onboardingCompleteFn is invoked by the onboarding package when the user
// finishes the wizard. It seeds the team from the user's picked blueprint
// (or synthesizes one if blueprintID is empty — the "from scratch" path),
// honors the wizard's per-agent checkbox filter, and posts the kickoff
// task to #general tagged to the blueprint's lead agent.
//
// Contract:
//   - blueprintID is the curated blueprint the user selected. Empty means
//     "from scratch" — the broker synthesizes a blueprint from the
//     onboarding-state goals.
//   - selectedAgents mirrors the wizard's toggle state:
//     nil   → no filtering (internal / synthesis callers, legacy client);
//     []    → user unchecked every agent; seed lead only + system notice;
//     [...] → keep only those slugs (plus the lead, which is unremovable).
//
// Side effects happen BEFORE the onboarding package writes the completion
// flag to disk, so a crash between this call returning and the flag write
// re-enters the wizard. The dedupe guard below (onboarding_origin by task
// content) prevents double-posting on crash recovery.
//
// The DefaultManifest roster (ceo/planner/executor/reviewer) is NEVER
// reached via this path. It remains only as a true-recovery fallback in
// ensureDefaultOfficeMembersLocked for corrupted/zero-member state.
func (b *Broker) onboardingCompleteFn(task string, skipTask bool, blueprintID string, selectedAgents []string) error {
	task = strings.TrimSpace(task)
	if !skipTask && task == "" {
		return fmt.Errorf("onboarding: task is required when skip_task=false")
	}

	blueprintID = strings.TrimSpace(blueprintID)
	synthesized := blueprintID == ""

	// Resolve the blueprint OUTSIDE the broker lock. LoadBlueprint reads YAML
	// from disk and runs validation; holding b.mu during that blocks every
	// other goroutine that needs the broker. Synthesis for the from-scratch
	// path reads onboarding state (another file) inside
	// synthesizeBlueprintFromState — also moved out of the critical section.
	var bp operations.Blueprint
	if blueprintID != "" {
		loaded, err := operations.LoadBlueprint(onboarding.ResolveTemplatesRepoRoot(""), blueprintID)
		if err != nil {
			return fmt.Errorf("onboarding: load blueprint %q: %w", blueprintID, err)
		}
		bp = loaded
	} else {
		bp = synthesizeBlueprintFromState(task)
	}

	seedErr := func() error {
		b.mu.Lock()
		defer b.mu.Unlock()

		// Dedupe after we're inside the lock so the messages slice is stable.
		// If a prior call already posted this exact task as an onboarding_origin
		// message (crash-recovery scenario), skip re-seeding and preserve the
		// earlier team.
		if !skipTask && task != "" {
			for _, existing := range b.messages {
				if existing.Channel == "general" && existing.Kind == "onboarding_origin" && existing.Content == task {
					return b.saveLocked()
				}
			}
		}

		return b.seedFromBlueprintLocked(bp, selectedAgents, task, skipTask, synthesized)
	}()
	if seedErr != nil {
		return seedErr
	}

	// Materialize the blueprint's LLM wiki outside the broker lock. Lane A
	// owns the git repo at ~/.wuphf/wiki; we write the skeleton files, commit
	// them under the reserved `wuphf-bootstrap` author, then regenerate the
	// index. Wiki materialization is best-effort: a failure here should NOT
	// fail onboarding (the user should land on an empty-but-functional wiki
	// rather than a broken onboarding flow). Log and move on.
	b.materializeBlueprintWiki(bp)
	return nil
}

// materializeBlueprintWiki resolves ~/.wuphf/wiki, runs the skeleton
// materializer, commits any newly-written skeletons as `wuphf-bootstrap`,
// then regenerates the index so a fresh install has both the files AND the
// audit trail from day 1.
//
// Errors are logged, never returned — onboarding succeeds regardless. A
// blueprint without a WikiSchema (e.g. a synthesized from-scratch
// blueprint) is silently skipped.
//
// Important: this runs OUTSIDE the broker lock (see caller), and uses the
// wiki worker's Repo for the commit so we go through the same
// per-commit-identity plumbing as regular agent writes. If the worker is
// not yet live (memory backend != markdown), we log and return — the
// skeletons stay on disk untracked and will be folded into the next
// RecoverDirtyTree pass. That's not ideal but it's the honest fallback.
func (b *Broker) materializeBlueprintWiki(bp operations.Blueprint) {
	if bp.WikiSchema == nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		log.Printf("onboarding: resolve home for wiki materialization: %v", err)
		return
	}
	wikiRoot := filepath.Join(home, ".wuphf", "wiki")
	result, err := operations.MaterializeWiki(context.Background(), wikiRoot, bp.WikiSchema)
	if err != nil {
		log.Printf("onboarding: wiki materialize failed (wiki left empty): %v", err)
		return
	}
	if len(result.ArticlesCreated) > 0 || len(result.DirsCreated) > 0 {
		log.Printf("onboarding: wiki materialized blueprint=%s dirs=%d articles_created=%d articles_skipped=%d",
			bp.ID, len(result.DirsCreated), len(result.ArticlesCreated), len(result.ArticlesSkipped))
	}
	// Nothing to commit if only existing articles were observed.
	if len(result.ArticlesCreated) == 0 && len(result.DirsCreated) == 0 {
		return
	}
	worker := b.WikiWorker()
	if worker == nil || worker.Repo() == nil {
		// Non-markdown backend — skeletons stay on disk, will surface via
		// RecoverDirtyTree on the next markdown-backend launch.
		return
	}
	repo := worker.Repo()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Regenerate the index FIRST so CommitBootstrap picks up index/all.md in
	// the same commit as the skeletons. Leaving it untracked would cause
	// RecoverDirtyTree on the next launch to fold it into a `wuphf-recovery`
	// commit, which misattributes a derived artefact.
	if err := repo.IndexRegen(ctx); err != nil {
		log.Printf("onboarding: wiki index regen failed (continuing): %v", err)
	}
	bootstrapMsg := fmt.Sprintf("wuphf: materialize %s blueprint skeletons", bp.ID)
	sha, err := repo.CommitBootstrap(ctx, bootstrapMsg)
	if err != nil {
		log.Printf("onboarding: wiki commit-bootstrap failed: %v", err)
		return
	}
	if sha != "" {
		log.Printf("onboarding: wiki bootstrap committed %s (blueprint=%s)", sha, bp.ID)
	}
}

// synthesizeBlueprintFromState builds a blueprint from whatever the user
// typed into the wizard (company name, description, size, priority, plus
// the task text as directive). Reads onboarding state from disk, so it
// must be called OUTSIDE the broker mutex. Unlike the old
// seedBlankSlateOperationLocked it does not mutate broker state — the
// caller feeds the returned Blueprint to seedFromBlueprintLocked.
func synthesizeBlueprintFromState(task string) operations.Blueprint {
	state, err := onboarding.Load()
	if err != nil {
		// Best-effort: fall through with empty profile. A Load failure is
		// logged by the onboarding package; SynthesizeBlueprint tolerates
		// sparse input by producing a generic blueprint.
		log.Printf("onboarding: load state for synthesis: %v", err)
		state = &onboarding.State{}
	}
	profile := operationCompanyProfile{
		Name:        strings.TrimSpace(state.CompanyName),
		Description: onboardingPartialString(state.Partial, "welcome", "desc"),
		Goals:       strings.TrimSpace(task),
		Size:        onboardingPartialString(state.Partial, "welcome", "size"),
		Priority:    onboardingPartialString(state.Partial, "welcome", "priority"),
	}
	return operations.SynthesizeBlueprint(operations.SynthesisInput{
		Directive: profile.Goals,
		Profile: operations.CompanyProfile{
			Name:        profile.Name,
			Description: profile.Description,
			Audience:    profile.Size,
			Offer:       profile.Goals,
		},
		Description: profile.Description,
		Goals:       profile.Goals,
		Size:        profile.Size,
		Priority:    profile.Priority,
	})
}

// seedFromBlueprintLocked is the single seed path used by both picked-
// blueprint and from-scratch flows. It replaces the prior dual-path code
// (seedBlankSlateOperationLocked + ensureDefaultOfficeMembersLocked+manual
// kickoff). selectedAgents filters the blueprint's starter roster; see the
// onboardingCompleteFn doc comment for the three-mode contract.
func (b *Broker) seedFromBlueprintLocked(bp operations.Blueprint, selectedAgents []string, task string, skipTask bool, synthesized bool) error {
	b.members = blankSlateOfficeMembersFromBlueprint(bp, selectedAgents)
	if len(b.members) == 0 {
		// Defensive: blueprint had no parseable agents AND no lead fallback
		// kicked in. Seed the DefaultManifest so the user has SOMETHING.
		b.members = defaultOfficeMembers()
	}
	b.channels = blankSlateOfficeChannelsFromBlueprint(bp, b.members)
	b.tasks = blankSlateOfficeTasksFromBlueprint(bp)
	if len(b.channels) == 0 {
		b.channels = []teamChannel{{
			Slug:        "general",
			Name:        "general",
			Description: "Primary coordination channel.",
			Members:     memberSlugsFromMembers(b.members),
		}}
	}
	b.messages = nil
	b.counter = 0
	b.lastTaggedAt = make(map[string]time.Time)
	if err := b.postKickoffLocked(bp, selectedAgents, task, skipTask, synthesized); err != nil {
		return err
	}
	// Signal subscribers (the launcher) that the office roster was replaced
	// wholesale. Individual member_created events aren't emitted by this path
	// — seedFromBlueprintLocked rewrites b.members directly — so without this
	// the launcher never learns the interactive tmux panes are out of sync
	// with the new team. Subscribers should treat this as "respawn panes".
	b.publishOfficeChangeLocked(officeChangeEvent{Kind: "office_reseeded"})
	return nil
}

func (b *Broker) postKickoffLocked(bp operations.Blueprint, selectedAgents []string, task string, skipTask bool, synthesized bool) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Lead-only warning: the wizard sent agents=[] (explicit empty = every
	// toggle unchecked). The seed helper fell back to lead-only; surface
	// that via a system message so the user knows the team is minimal.
	if selectedAgents != nil && len(selectedAgents) == 0 && len(b.members) == 1 {
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "system",
			Channel:   "general",
			Kind:      "system",
			Content:   "Team seeded with lead only. Add specialists from Team settings.",
			Timestamp: now,
		})
	}

	if skipTask {
		// seedFromBlueprintLocked mutated b.members/channels/tasks above; we
		// must persist that even when the user skipped the kickoff task.
		// Returning early without saveLocked() silently loses the seeded team
		// on the next broker Load.
		return b.saveLocked()
	}

	task = strings.TrimSpace(task)
	if task == "" {
		return fmt.Errorf("onboarding: task is required when skip_task=false")
	}

	lead := officeLeadSlugFromMembers(b.members)
	if lead == "" {
		// Every shipped blueprint declares ceo as lead (guarded by
		// TestAllOperationBlueprintsUseCEOLead). The fallback here only fires
		// for malformed/synthesized blueprints with no identifiable lead.
		lead = "ceo"
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "human",
		Channel:   "general",
		Kind:      "onboarding_origin",
		Content:   task,
		Tagged:    []string{lead},
		Timestamp: now,
	})
	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}
	b.lastTaggedAt[lead] = time.Now()

	// Synthesized blueprints (from-scratch path) post two extra markers so
	// the downstream agents know they are running against a just-invented
	// operation rather than a curated one.
	if synthesized {
		if strings.TrimSpace(bp.Name) != "" {
			b.counter++
			b.appendMessageLocked(channelMessage{
				ID:        fmt.Sprintf("msg-%d", b.counter),
				From:      "system",
				Channel:   "general",
				Kind:      "synthesized_blueprint",
				Content:   fmt.Sprintf("Synthesized operation: %s (%s)", bp.Name, bp.Kind),
				Timestamp: now,
			})
		}
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "system",
			Channel:   "general",
			Kind:      "from_scratch_contract",
			Content:   "Run this as a real business workflow. If a needed specialist, channel, skill, or tooling path is missing, create it and keep going. Local proof packets, review bundles, and other internal substitute artifacts do not count when a live business step is possible.",
			Timestamp: now,
		})
	}

	return b.saveLocked()
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

// blankSlateOfficeMembersFromBlueprint projects a blueprint's starter
// agent list into broker officeMembers, applying the wizard's
// selectedAgents filter. See onboardingCompleteFn doc for the nil / empty
// / populated contract.
//
// The lead agent (from blueprint.Starter.LeadSlug) is always kept,
// regardless of the filter — removing the lead leaves downstream code with
// no one to tag for kickoff and no BuiltIn member for channel ownership.
func blankSlateOfficeMembersFromBlueprint(blueprint operations.Blueprint, selectedAgents []string) []officeMember {
	agents := blueprint.Starter.Agents
	leadSlug := normalizeChannelSlug(blueprint.Starter.LeadSlug)
	filter := agentSelectionFilter(selectedAgents, leadSlug)

	members := make([]officeMember, 0, len(agents))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, agent := range agents {
		slug := normalizeChannelSlug(operationFirstNonEmpty(agent.Slug, agent.EmployeeBlueprint, operationSlug(agent.Name)))
		if slug == "" {
			continue
		}
		if filter != nil && !filter(slug) {
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
			BuiltIn:        agent.BuiltIn || slug == leadSlug || slug == "operator" || slug == "founder" || slug == "ceo",
		})
	}
	if len(members) > 0 {
		return members
	}
	// Defensive fallback used only when the blueprint had zero parseable
	// agents. Keeps the broker from crashing on empty rosters.
	return []officeMember{
		{Slug: "founder", Name: "Founder", Role: "Founder", PermissionMode: "plan", BuiltIn: true, CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "operator", Name: "Operator", Role: "Operator", PermissionMode: "auto", BuiltIn: true, CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "builder", Name: "Builder", Role: "Builder", PermissionMode: "auto", CreatedBy: "wuphf", CreatedAt: now},
		{Slug: "reviewer", Name: "Reviewer", Role: "Reviewer", PermissionMode: "plan", CreatedBy: "wuphf", CreatedAt: now},
	}
}

// agentSelectionFilter returns a membership predicate for the wizard's
// selectedAgents array. nil input disables filtering (keep all); empty
// array keeps only the lead so the team isn't empty (the caller relies on
// len(members) == 1 to emit the lead-only system message); a populated
// array keeps only those slugs, always including the lead.
func agentSelectionFilter(selectedAgents []string, leadSlug string) func(string) bool {
	if selectedAgents == nil {
		return nil
	}
	allowed := make(map[string]bool, len(selectedAgents)+1)
	for _, s := range selectedAgents {
		if slug := normalizeChannelSlug(s); slug != "" {
			allowed[slug] = true
		}
	}
	if leadSlug != "" {
		allowed[leadSlug] = true
	}
	return func(slug string) bool { return allowed[slug] }
}

func blankSlateOfficeChannelsFromBlueprint(blueprint operations.Blueprint, members []officeMember) []teamChannel {
	brandName := operationFirstNonEmpty(blueprint.Name, "New operation")
	commandSlug := operationSlug(brandName + " command")
	if commandSlug == "" {
		commandSlug = "command"
	}
	replacements := map[string]string{
		"brand_name":   brandName,
		"brand_slug":   operationSlug(operationFirstNonEmpty(blueprint.Name, "new-operation")),
		"command_slug": commandSlug,
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
	prefix := taskIDPrefix(blueprint)
	tasks := make([]teamTask, 0, len(blueprint.Starter.Tasks))
	for i, starter := range blueprint.Starter.Tasks {
		channel := normalizeChannelSlug(starter.Channel)
		if channel == "" {
			channel = "general"
		}
		owner := normalizeChannelSlug(starter.Owner)
		tasks = append(tasks, teamTask{
			ID:        fmt.Sprintf("%s-%d", prefix, i+1),
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

// taskIDPrefix returns a slug usable as a prefix for seeded task IDs.
// Curated blueprints (niche-crm, youtube-factory, etc.) have an ID field
// set by the loader; synthesized blueprints have an inferred ID too, but
// if for any reason the blueprint has no ID we fall back to "blank-slate"
// to preserve the legacy id shape.
func taskIDPrefix(bp operations.Blueprint) string {
	if id := normalizeChannelSlug(bp.ID); id != "" {
		return id
	}
	return "blank-slate"
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
