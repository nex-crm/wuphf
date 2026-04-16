package operations

import (
	"fmt"
	"sort"
	"strings"
)

func synthesizeGenericBlueprint(input SynthesisInput) Blueprint {
	input = normalizeGenericSynthesisInput(input)
	kind := genericInferOperationKind(input.Directive, input.Profile, input.Name, input.Description, input.Goals, input.Priority)
	name := genericBlueprintName(kind, input)
	objective := genericObjective(input)
	integrations := normalizeGenericIntegrations(input.Integrations)
	capabilities := normalizeGenericCapabilities(input.Capabilities)

	blueprint := Blueprint{
		ID:                 genericBlueprintID(name, input),
		Name:               name,
		Kind:               kind,
		Description:        genericBlueprintDescription(kind, input),
		Objective:          objective,
		Starter:            genericStarterPlan(kind, name, objective, input, integrations, capabilities),
		EmployeeBlueprints: []string{},
		BootstrapConfig:    genericBootstrapConfig(kind, name, objective, input, integrations),
		MonetizationLadder: genericMonetizationLadder(kind, objective),
		QueueSeed:          genericQueueSeed(kind, name, objective, input, integrations),
		Automation:         genericAutomationModules(kind, name, objective, integrations),
		Stages:             genericStageDefinitions(),
		Artifacts:          genericArtifactTypes(kind, integrations),
		Capabilities:       genericCapabilityRequirements(integrations, capabilities),
		ApprovalRules:      genericApprovalRules(kind, objective, integrations),
		Connections:        genericConnectionBlueprints(kind, input.Profile, integrations),
		Workflows:          genericWorkflowTemplates(kind, name, objective, input.Profile, integrations),
	}

	if strings.TrimSpace(blueprint.Starter.LeadSlug) == "" {
		blueprint.Starter.LeadSlug = "operator"
	}
	if len(blueprint.Starter.Channels) == 0 {
		blueprint.Starter.Channels = genericDefaultChannels(integrations)
	}
	if len(blueprint.Starter.Tasks) == 0 {
		blueprint.Starter.Tasks = genericDefaultTasks(objective, integrations)
	}
	return blueprint
}

func normalizeGenericSynthesisInput(input SynthesisInput) SynthesisInput {
	input.Directive = strings.TrimSpace(input.Directive)
	input.Profile.Name = strings.TrimSpace(input.Profile.Name)
	input.Profile.Industry = strings.TrimSpace(input.Profile.Industry)
	input.Profile.Description = strings.TrimSpace(input.Profile.Description)
	input.Profile.Audience = strings.TrimSpace(input.Profile.Audience)
	input.Profile.Website = strings.TrimSpace(input.Profile.Website)
	input.Profile.Geography = strings.TrimSpace(input.Profile.Geography)
	input.Profile.Offer = strings.TrimSpace(input.Profile.Offer)
	for i := range input.Profile.Notes {
		input.Profile.Notes[i] = strings.TrimSpace(input.Profile.Notes[i])
	}
	return input
}

func genericBlueprintID(name string, input SynthesisInput) string {
	if strings.TrimSpace(input.Profile.Name) != "" {
		return normalizeTemplateID(input.Profile.Name)
	}
	if strings.TrimSpace(name) != "" {
		return normalizeTemplateID(name)
	}
	if strings.TrimSpace(input.Directive) != "" {
		return normalizeTemplateID(genericShortTitle(input.Directive))
	}
	return "operation-blueprint"
}

func genericBlueprintName(kind string, input SynthesisInput) string {
	if name := strings.TrimSpace(input.Profile.Name); name != "" {
		if kind == "" || kind == "general" {
			return fmt.Sprintf("%s Operations Blueprint", name)
		}
		return fmt.Sprintf("%s %s Blueprint", name, genericTitleWord(kind))
	}
	if directive := strings.TrimSpace(input.Directive); directive != "" {
		return fmt.Sprintf("%s Blueprint", genericShortTitle(directive))
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		return fmt.Sprintf("%s Blueprint", name)
	}
	return "Operations Blueprint"
}

func genericBlueprintDescription(kind string, input SynthesisInput) string {
	base := firstOperationValue(input.Directive, input.Profile.Description, input.Description, input.Goals, input.Priority)
	if base == "" {
		base = fmt.Sprintf("A %s operations blueprint.", kind)
	}
	return genericTruncateText(base, 220)
}

func genericObjective(input SynthesisInput) string {
	base := firstOperationValue(input.Directive, input.Profile.Description, input.Goals, input.Priority, input.Description, input.Profile.Offer)
	if base != "" {
		return genericTruncateText(base, 260)
	}
	if input.Profile.Audience != "" {
		return fmt.Sprintf("Serve %s with a repeatable autonomous operating loop.", input.Profile.Audience)
	}
	if input.Profile.Name != "" {
		return fmt.Sprintf("Build a repeatable autonomous operating loop for %s.", input.Profile.Name)
	}
	return "Build a repeatable autonomous operating loop from a blank directive."
}

func genericInferOperationKind(directive string, profile CompanyProfile, name, description, goals, priority string) string {
	text := strings.ToLower(strings.Join([]string{
		directive,
		profile.Name,
		profile.Industry,
		profile.Description,
		profile.Audience,
		profile.Offer,
		name,
		description,
		goals,
		priority,
		strings.Join(profile.Notes, " "),
	}, " "))

	switch {
	case containsAny(text, "lead gen", "lead-generation", "pipeline", "prospect", "outreach", "gtm", "sales", "demand gen"):
		return "gtm"
	case containsAny(text, "content", "creator", "audience", "newsletter", "media", "podcast", "video", "youtube"):
		return "content"
	case containsAny(text, "ecommerce", "commerce", "shop", "store", "checkout", "cart", "retail"):
		return "commerce"
	case containsAny(text, "support", "success", "helpdesk", "customer care", "cs"):
		return "support"
	case containsAny(text, "research", "analysis", "insight", "investigate", "synthesis"):
		return "research"
	case containsAny(text, "product", "build", "shipping", "software", "code", "engineering", "app"):
		return "product"
	case containsAny(text, "ops", "operations", "automation", "workflow", "process"):
		return "operations"
	default:
		return "general"
	}
}

func genericStarterPlan(kind, name, objective string, input SynthesisInput, integrations []RuntimeIntegration, capabilities []RuntimeCapability) StarterPlan {
	leadSlug := "operator"
	channels := genericDefaultChannels(integrations)
	tasks := genericDefaultTasks(objective, integrations)
	plannerName, executorName, reviewerName := genericKindAgentNames(kind)
	agents := []StarterAgent{
		{Slug: leadSlug, Name: "Operator", Role: "lead", PermissionMode: "plan", Checked: true, Type: "human", BuiltIn: true, Expertise: []string{"scope-setting", "execution", "approvals"}},
		{Slug: "planner", Name: plannerName, Role: "planning", PermissionMode: "plan", Checked: true, Type: "assistant", BuiltIn: true, Expertise: []string{"decomposition", "sequencing", "risks"}},
		{Slug: "executor", Name: executorName, Role: "execution", PermissionMode: "auto", Checked: true, Type: "assistant", BuiltIn: true, Expertise: []string{"delivery", "instrumentation", "evidence"}},
		{Slug: "reviewer", Name: reviewerName, Role: "review", PermissionMode: "plan", Checked: true, Type: "assistant", BuiltIn: true, Expertise: []string{"quality", "approval", "handoff"}},
	}
	for _, integration := range integrations {
		provider := genericIntegrationKey(integration)
		if provider == "" {
			continue
		}
		agents = append(agents, StarterAgent{
			Slug:           provider,
			Name:           genericIntegrationLabel(integration),
			Role:           "integration-owner",
			PermissionMode: "auto",
			Checked:        integration.Connected,
			Type:           "integration",
			BuiltIn:        false,
			Expertise:      []string{provider, kind, genericIntegrationPurpose(integration, kind)},
		})
	}
	if len(capabilities) > 0 {
		agents = append(agents, StarterAgent{
			Slug:           "capability-scout",
			Name:           "Capability Scout",
			Role:           "capability-discovery",
			PermissionMode: "plan",
			Checked:        true,
			Type:           "assistant",
			BuiltIn:        true,
			Expertise:      []string{"runtime-capabilities", "setup", "availability"},
		})
	}
	return StarterPlan{
		LeadSlug:                  leadSlug,
		GeneralChannelDescription: genericGeneralChannelDescription(kind, input.Profile, objective),
		KickoffPrompt:             genericKickoffPrompt(kind, name, objective, input.Profile, integrations),
		Agents:                    agents,
		Channels:                  channels,
		Tasks:                     tasks,
	}
}

func genericGeneralChannelDescription(kind string, profile CompanyProfile, objective string) string {
	switch {
	case profile.Name != "" && objective != "":
		return fmt.Sprintf("Primary coordination channel for %s.", profile.Name)
	case profile.Name != "":
		return fmt.Sprintf("Primary coordination channel for the %s operation.", profile.Name)
	default:
		return fmt.Sprintf("Primary coordination channel for the %s operation.", kind)
	}
}

func genericKickoffPrompt(kind, name, objective string, profile CompanyProfile, integrations []RuntimeIntegration) string {
	parts := []string{
		"Start from the directive and turn it into a concrete operating plan.",
		fmt.Sprintf("Objective: %s", objective),
	}
	if profile.Name != "" {
		parts = append(parts, fmt.Sprintf("Company profile: %s", profile.Name))
	}
	if len(integrations) > 0 {
		names := make([]string, 0, len(integrations))
		for _, integration := range integrations {
			names = append(names, genericIntegrationLabel(integration))
		}
		parts = append(parts, fmt.Sprintf("Available integrations: %s", strings.Join(names, ", ")))
	}
	parts = append(parts, "Surface approval points before any live external side effects and leave durable evidence for each completed step.")
	if kind != "" {
		parts = append(parts, fmt.Sprintf("Operate as a %s blueprint, not as a product-specific template.", kind))
	}
	return strings.Join(parts, " ")
}

func genericBootstrapConfig(kind, name, objective string, input SynthesisInput, integrations []RuntimeIntegration) BootstrapConfig {
	channelName := firstOperationValue(input.Profile.Name, name, "Operations")
	monetizationHooks := []string{"convert completed work into repeatable value"}
	if kind == "gtm" || containsAny(strings.ToLower(objective), "revenue", "sales", "lead", "pipeline", "outreach") {
		monetizationHooks = []string{"turn outbound work into qualified pipeline", "capture and convert demand signals"}
	}
	leadMagnet := LeadMagnet{}
	if kind == "gtm" || containsAny(strings.ToLower(objective), "lead", "pipeline", "prospect", "outreach") {
		leadMagnet = LeadMagnet{Name: "Operating brief", CTA: "Request the execution brief", Path: "/brief"}
	}
	monetizationAssets := []MonetizationAsset{}
	if kind == "gtm" || containsAny(strings.ToLower(objective), "revenue", "sales", "outreach", "pipeline") {
		monetizationAssets = append(monetizationAssets, MonetizationAsset{
			Stage: "convert",
			Name:  "first value exchange",
			Slot:  "primary",
			CTA:   "Move the first qualified outcome into a revenue path",
		})
	}
	return BootstrapConfig{
		ChannelName:       channelName,
		ChannelSlug:       normalizeTemplateID(channelName),
		Niche:             firstOperationValue(input.Profile.Industry, kind, "operations"),
		Audience:          firstOperationValue(input.Profile.Audience, genericAudienceFromObjective(objective)),
		Positioning:       genericPositioning(kind, input.Profile, objective),
		ContentPillars:    []string{"strategy", "execution", "evidence"},
		ContentSeries:     []string{"intake-to-plan", "plan-to-execution", "review-and-adapt"},
		MonetizationHooks: monetizationHooks,
		PublishingCadence: "as needed",
		LeadMagnet:        leadMagnet,
		MonetizationAsset: monetizationAssets,
		KPITracking: []KPI{
			{Name: "time_to_first_plan", Target: "under one turn", Why: "Shows synthesis is working"},
			{Name: "time_to_first_execution", Target: "under one loop", Why: "Shows the plan becomes action"},
			{Name: "approval_latency", Target: "as low as possible", Why: "Shows humans are only in the loop where needed"},
		},
	}
}

func genericPositioning(kind string, profile CompanyProfile, objective string) string {
	if profile.Description != "" {
		return genericTruncateText(profile.Description, 120)
	}
	if profile.Offer != "" {
		return genericTruncateText(profile.Offer, 120)
	}
	if profile.Name != "" {
		return fmt.Sprintf("Autonomous %s execution for %s.", kind, profile.Name)
	}
	return fmt.Sprintf("Autonomous %s execution from a blank directive.", kind)
}

func genericAudienceFromObjective(objective string) string {
	lowered := strings.ToLower(objective)
	switch {
	case containsAny(lowered, "lead", "pipeline", "sales", "outreach"):
		return "buyers and prospects"
	case containsAny(lowered, "content", "audience", "video", "newsletter"):
		return "the target audience"
	default:
		return "the intended stakeholders"
	}
}

func genericMonetizationLadder(kind, objective string) []MonetizationStep {
	steps := []string{"free diagnostic", "pilot", "retainer", "productized asset"}
	if kind == "content" {
		steps = []string{"owned audience", "affiliate offer", "digital product", "sponsor lane"}
	}
	out := make([]MonetizationStep, 0, len(steps))
	for i, step := range steps {
		out = append(out, MonetizationStep{
			Kicker: fmt.Sprintf("Step %d", i+1),
			Title:  step,
			Copy:   fmt.Sprintf("Turn %s into a reusable value-capture lane for the operation.", step),
			Footer: "Synthesized from the directive rather than loaded from a fixed pack.",
		})
	}
	if strings.TrimSpace(objective) == "" {
		return out[:1]
	}
	return out
}

func genericQueueSeed(kind, name, objective string, input SynthesisInput, integrations []RuntimeIntegration) []QueueItem {
	items := []QueueItem{
		{ID: "work-01", Title: firstOperationValue(input.Priority, input.Goals, "Define the first operating loop"), Format: "Operational work item", StageIndex: 0, Score: 92, UnitCost: 8, Eta: "Iteration 1", Monetization: "approval-gated value capture", State: "active"},
		{ID: "work-02", Title: "Stand up the planning lane", Format: "Operational work item", StageIndex: 1, Score: 86, UnitCost: 10, Eta: "Iteration 2", Monetization: "approval-gated value capture", State: "active"},
		{ID: "work-03", Title: "Launch the first execution loop", Format: "Operational work item", StageIndex: 2, Score: 80, UnitCost: 12, Eta: "Iteration 3", Monetization: "approval-gated value capture", State: "active"},
	}
	if name != "" {
		items[0].Title = genericTruncateText(name+" "+items[0].Title, 80)
	}
	for _, integration := range integrations {
		if integration.Connected {
			continue
		}
		items = append(items, QueueItem{
			ID:           "connect-" + genericIntegrationKey(integration),
			Title:        fmt.Sprintf("Connect %s before live use", genericIntegrationLabel(integration)),
			Format:       "integration",
			StageIndex:   1,
			Score:        70,
			UnitCost:     1,
			Eta:          "before live use",
			Monetization: kind,
			State:        "open",
		})
	}
	return items
}

func genericAutomationModules(kind, name, objective string, integrations []RuntimeIntegration) []AutomationModule {
	modules := []AutomationModule{
		{ID: "control-plane", Kicker: "Build now", Title: "Control plane + operating defaults", Copy: "Persist the objective, starter lanes, workflows, approvals, and scorecard in the broker-backed operation state.", Status: "build_now", Footer: "The operation should be replayable from durable state, not browser memory."},
		{ID: "artifact-engine", Kicker: "Build now", Title: "Artifact generation", Copy: "Turn work items into reusable artifact bundles that can be reviewed, handed off, and reused by future runs.", Status: "build_now", Footer: "Artifact definitions come from the synthesized blueprint."},
		{ID: "workflow-harness", Kicker: "Build now", Title: "Workflow smoke harness", Copy: "Create dry-run or mock workflows for the active integrations and persist evidence for each run.", Status: "build_now", Footer: "Smoke tests should be derived from the blueprint, not hardcoded to a domain."},
		{ID: "approval-plane", Kicker: "Build now", Title: "Approval boundaries", Copy: "Keep account linking, public sends, destructive changes, and spend behind explicit human approvals.", Status: "build_now", Footer: "The policy layer should stay generic across operations."},
		{ID: "review-loop", Kicker: "Build now", Title: "Metrics and review loop", Copy: "Track throughput, output quality, and approval latency so the operation improves each iteration.", Status: "build_now", Footer: "A weekly review loop is seeded by default."},
	}
	if len(integrations) > 0 {
		modules[2].Status = "ready_for_auth"
		modules[2].Kicker = "Connection-aware"
		modules[2].Footer = "Connected systems were detected. Dry-run and mock paths can be wired immediately, but live mutations still require approval."
	}
	if name != "" {
		modules[0].Title = fmt.Sprintf("Coordinate %s", name)
	}
	if strings.TrimSpace(objective) != "" {
		modules[0].Copy = genericTruncateText(objective, 120)
	}
	return modules
}

func genericStageDefinitions() []StageDefinition {
	return []StageDefinition{
		{ID: "discover", Name: "Discover", Engine: "planning", Description: "Clarify the objective, inputs, and workstreams.", ExitCriteria: "Objective and first work items are durable."},
		{ID: "plan", Name: "Plan", Engine: "planning", Description: "Turn the directive into an operating plan and artifact map.", ExitCriteria: "Starter tasks, channels, and artifact definitions exist."},
		{ID: "execute", Name: "Execute", Engine: "execution", Description: "Produce the first deliverables and workflow assets.", ExitCriteria: "The first execution bundle is reviewable."},
		{ID: "validate", Name: "Validate", Engine: "review", Description: "Review outputs, smoke-test workflows, and capture evidence.", ExitCriteria: "Evidence is persisted and approvals are explicit."},
		{ID: "operate", Name: "Operate", Engine: "operations", Description: "Run the loop repeatedly with scorecards and approvals.", ExitCriteria: "The operation has a repeatable rhythm."},
	}
}

func genericArtifactTypes(kind string, integrations []RuntimeIntegration) []ArtifactType {
	artifacts := []ArtifactType{
		{ID: "objective_brief", Name: "Objective brief", Description: "Problem statement, scope, constraints, and success criteria."},
		{ID: "operating_plan", Name: "Operating plan", Description: "Workstreams, milestones, owners, and approval map."},
		{ID: "execution_packet", Name: "Execution packet", Description: "The current run's deliverables, checklist, and handoff context."},
		{ID: "workflow_run", Name: "Workflow run", Description: "Durable evidence for one dry-run, mock, or live execution."},
	}
	if kind == "content" {
		artifacts[2] = ArtifactType{ID: "launch_packet", Name: "Launch packet", Description: "Ready-to-review bundle for the next outward-facing release."}
	}
	for _, integration := range integrations {
		artifacts = append(artifacts, ArtifactType{
			ID:          "evidence-" + genericIntegrationKey(integration),
			Name:        fmt.Sprintf("%s Evidence", genericIntegrationLabel(integration)),
			Description: fmt.Sprintf("Proof that %s can be used safely in the current operation.", genericIntegrationLabel(integration)),
		})
	}
	return artifacts
}

func genericCapabilityRequirements(integrations []RuntimeIntegration, capabilities []RuntimeCapability) []CapabilityRequirement {
	requirements := []CapabilityRequirement{
		{ID: "runtime-workspace", Name: "Workspace runtime", Kind: "runtime", Description: "The runtime used to execute and persist the operation."},
		{ID: "human-approval", Name: "Human approval", Kind: "governance", Description: "A human must approve live side effects, spend, or publishing when needed."},
		{ID: "planning", Name: "Planning and decomposition", Kind: "planning", Description: "Turn the directive into workstreams, stages, and tasks."},
		{ID: "delivery", Name: "Execution and delivery", Kind: "execution", Description: "Build the first deliverables and execution bundles."},
		{ID: "operations", Name: "Operations and approvals", Kind: "execution", Description: "Run the workflows, enforce approvals, and keep state durable."},
		{ID: "analytics", Name: "Metrics and review", Kind: "analysis", Description: "Track outcomes, bottlenecks, and loop improvements."},
	}
	for _, capability := range capabilities {
		key := capability.Key
		if strings.TrimSpace(key) == "" {
			key = normalizeTemplateID(capability.Name)
		}
		if key == "" {
			continue
		}
		requirements = append(requirements, CapabilityRequirement{
			ID:          "cap-" + key,
			Name:        firstOperationValue(capability.Name, key),
			Kind:        firstOperationValue(capability.Category, "runtime"),
			Description: genericTruncateText(firstOperationValue(capability.Detail, capability.Lifecycle), 180),
		})
	}
	for _, integration := range integrations {
		key := genericIntegrationKey(integration)
		if key == "" {
			continue
		}
		requirements = append(requirements, CapabilityRequirement{
			ID:           "integration-" + key,
			Name:         genericIntegrationLabel(integration) + " integration",
			Kind:         "integration",
			Integrations: []string{key},
			Description:  genericIntegrationPurpose(integration, "operations"),
		})
	}
	return genericDedupeCapabilityRequirements(requirements)
}

func genericApprovalRules(kind, objective string, integrations []RuntimeIntegration) []ApprovalRule {
	rules := []ApprovalRule{
		{ID: "external-accounts", Trigger: "connect_account", Description: "Human approval required before linking or authorizing a real external account."},
		{ID: "public-send", Trigger: "public_send_or_publish", Description: "Human approval required before any public send, publish, or externally visible update."},
		{ID: "spend", Trigger: "spend_money", Description: "Human approval required before vendor spend, ad spend, or budget commitments."},
		{ID: "destructive-change", Trigger: "destructive_mutation", Description: "Human approval required before destructive external mutations or irreversible operational changes."},
		{ID: "commercial-commitment", Trigger: "contract_or_legal_change", Description: "Human approval required before legal or commercial commitments."},
	}
	lowered := strings.ToLower(objective)
	if kind == "commerce" || containsAny(lowered, "sell", "purchase", "checkout") {
		rules = append(rules, ApprovalRule{ID: "transactional", Trigger: "transactional_action", Description: "Human approval required before transactional or order-affecting actions."})
	}
	if len(integrations) == 0 {
		return rules[:1]
	}
	return rules
}

func genericConnectionBlueprints(kind string, profile CompanyProfile, integrations []RuntimeIntegration) []ConnectionBlueprint {
	connections := make([]ConnectionBlueprint, 0, len(integrations))
	for _, integration := range integrations {
		label := genericIntegrationLabel(integration)
		provider := genericIntegrationKey(integration)
		if provider == "" {
			continue
		}
		connections = append(connections, ConnectionBlueprint{
			Name:        label,
			Integration: provider,
			Owner:       firstOperationValue(profile.Name, "operator"),
			Priority:    genericTernaryString(integration.Connected, "high", "medium"),
			Purpose:     genericIntegrationPurpose(integration, kind),
			SmokeTest:   fmt.Sprintf("Verify %s can perform the first planned action.", label),
			Blocker:     genericTernaryString(integration.Connected, "", fmt.Sprintf("Connect %s before live use.", label)),
		})
	}
	return connections
}

func genericWorkflowTemplates(kind, name, objective string, profile CompanyProfile, integrations []RuntimeIntegration) []WorkflowTemplate {
	workflows := []WorkflowTemplate{
		{
			ID:      "intake-plan",
			Name:    "Intake and plan",
			Trigger: "manual",
			Mode:    "manual",
			Checklist: []string{
				"Confirm the directive and profile",
				"Choose the first lane",
				"Identify approval gates",
			},
			Description: "Convert the brief into a minimal execution plan.",
			Definition: map[string]any{
				"version": 1,
				"kind":    "intake",
				"steps": []map[string]any{
					{"id": "capture", "kind": "input"},
					{"id": "classify", "kind": "transform"},
					{"id": "approve", "kind": "approval"},
				},
			},
			SmokeTest: WorkflowSmokeTest{Name: "Intake smoke test", Mode: "dry-run", Proof: "A directive can be captured and translated into a minimal plan."},
		},
	}
	for _, integration := range integrations {
		provider := genericIntegrationKey(integration)
		if provider == "" {
			continue
		}
		label := genericIntegrationLabel(integration)
		workflows = append(workflows, WorkflowTemplate{
			ID:           "use-" + provider,
			Name:         fmt.Sprintf("Use %s", label),
			Trigger:      "manual",
			Mode:         genericTernaryString(integration.Connected, "live", "dry-run"),
			Integrations: []string{provider},
			Checklist: []string{
				fmt.Sprintf("Confirm %s access", label),
				"Run the first allowed action",
				"Capture durable evidence",
			},
			Description: genericIntegrationPurpose(integration, kind),
			Definition: map[string]any{
				"version":  1,
				"kind":     "integration",
				"provider": provider,
				"steps": []map[string]any{
					{"id": "prepare", "kind": "input"},
					{"id": "execute", "kind": "action", "provider": provider},
					{"id": "evidence", "kind": "output"},
				},
			},
			SmokeTest: WorkflowSmokeTest{
				Name:   fmt.Sprintf("%s smoke test", label),
				Mode:   "dry-run",
				Proof:  fmt.Sprintf("%s can be exercised before going live.", label),
				Inputs: map[string]any{"provider": provider},
			},
		})
	}
	if len(workflows) == 1 {
		workflows[0].Description = genericTruncateText(objective, 160)
		if profile.Name != "" {
			workflows[0].Checklist = append(workflows[0].Checklist, fmt.Sprintf("Align the plan for %s", profile.Name))
		}
		if name != "" {
			workflows[0].Checklist = append(workflows[0].Checklist, fmt.Sprintf("Validate the %s objective", name))
		}
	}
	return workflows
}

func genericDefaultChannels(integrations []RuntimeIntegration) []StarterChannel {
	channels := []StarterChannel{
		{Slug: "general", Name: "general", Description: "Primary coordination channel.", Members: []string{"operator", "planner", "executor", "reviewer"}},
		{Slug: "planning", Name: "planning", Description: "Scope, decomposition, and approvals.", Members: []string{"operator", "planner", "reviewer"}},
		{Slug: "execution", Name: "execution", Description: "Active work lane for the current operation.", Members: []string{"operator", "executor"}},
		{Slug: "review", Name: "review", Description: "Evidence, decisions, and handoff.", Members: []string{"operator", "reviewer"}},
	}
	if len(integrations) > 0 {
		members := []string{"operator", "executor"}
		for _, integration := range integrations {
			provider := genericIntegrationKey(integration)
			if provider == "" {
				continue
			}
			members = append(members, provider)
		}
		channels = append(channels, StarterChannel{
			Slug:        "integrations",
			Name:        "integrations",
			Description: "Integration-specific work and evidence.",
			Members:     genericDedupeStrings(members),
		})
	}
	return channels
}

func genericDefaultTasks(objective string, integrations []RuntimeIntegration) []StarterTask {
	tasks := []StarterTask{
		{Channel: "general", Owner: "operator", Title: "Translate the directive into the first execution plan", Details: genericTruncateText(objective, 160)},
		{Channel: "planning", Owner: "planner", Title: "Inventory capabilities and approvals", Details: "List the available runtime integrations and the gates required before live action."},
		{Channel: "execution", Owner: "executor", Title: "Launch the first execution loop", Details: "Run the first concrete step and record evidence."},
		{Channel: "review", Owner: "reviewer", Title: "Review evidence and pick the next loop", Details: "Confirm what happened and whether the next step is approved."},
	}
	for _, integration := range integrations {
		if integration.Connected {
			continue
		}
		label := genericIntegrationLabel(integration)
		tasks = append(tasks, StarterTask{
			Channel: "planning",
			Owner:   "operator",
			Title:   fmt.Sprintf("Connect %s before live use", label),
			Details: fmt.Sprintf("The blueprint can only use %s live after it is connected.", label),
		})
	}
	return tasks
}

func genericIntegrationPurpose(integration RuntimeIntegration, kind string) string {
	if strings.TrimSpace(integration.Purpose) != "" {
		return integration.Purpose
	}
	if strings.TrimSpace(integration.Description) != "" {
		return genericTruncateText(integration.Description, 120)
	}
	switch genericIntegrationKey(integration) {
	case "gmail":
		return "Support inbox-driven execution and outbound communication."
	case "slack":
		return "Publish updates and coordination evidence."
	case "google-drive", "drive":
		return "Store durable operating documents and evidence."
	case "google-calendar", "calendar":
		return "Schedule and track execution windows."
	default:
		return fmt.Sprintf("Use %s to support the %s operation.", genericIntegrationLabel(integration), kind)
	}
}

func genericIntegrationLabel(integration RuntimeIntegration) string {
	if strings.TrimSpace(integration.Name) != "" {
		return strings.TrimSpace(integration.Name)
	}
	if strings.TrimSpace(integration.Provider) != "" {
		return strings.TrimSpace(integration.Provider)
	}
	return "integration"
}

func genericIntegrationKey(integration RuntimeIntegration) string {
	if key := normalizeTemplateID(integration.Provider); key != "" {
		return key
	}
	return normalizeTemplateID(integration.Name)
}

func normalizeGenericIntegrations(integrations []RuntimeIntegration) []RuntimeIntegration {
	out := make([]RuntimeIntegration, 0, len(integrations))
	seen := map[string]struct{}{}
	for _, integration := range integrations {
		integration.Provider = strings.TrimSpace(strings.ToLower(integration.Provider))
		integration.Name = strings.TrimSpace(integration.Name)
		integration.Status = strings.TrimSpace(integration.Status)
		integration.Purpose = strings.TrimSpace(integration.Purpose)
		integration.Description = strings.TrimSpace(integration.Description)
		if integration.Provider == "" && integration.Name == "" {
			continue
		}
		key := genericIntegrationKey(integration)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, integration)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return genericIntegrationKey(out[i]) < genericIntegrationKey(out[j])
	})
	return out
}

func normalizeGenericCapabilities(capabilities []RuntimeCapability) []RuntimeCapability {
	out := make([]RuntimeCapability, 0, len(capabilities))
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		capability.Key = strings.TrimSpace(strings.ToLower(capability.Key))
		capability.Name = strings.TrimSpace(capability.Name)
		capability.Category = strings.TrimSpace(capability.Category)
		capability.Lifecycle = strings.TrimSpace(capability.Lifecycle)
		capability.Detail = strings.TrimSpace(capability.Detail)
		if capability.Key == "" && capability.Name == "" {
			continue
		}
		key := firstOperationValue(capability.Key, normalizeTemplateID(capability.Name))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, capability)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return firstOperationValue(out[i].Key, normalizeTemplateID(out[i].Name)) < firstOperationValue(out[j].Key, normalizeTemplateID(out[j].Name))
	})
	return out
}

func genericDedupeCapabilityRequirements(requirements []CapabilityRequirement) []CapabilityRequirement {
	out := make([]CapabilityRequirement, 0, len(requirements))
	seen := map[string]struct{}{}
	for _, requirement := range requirements {
		requirement.ID = strings.TrimSpace(requirement.ID)
		if requirement.ID == "" {
			requirement.ID = normalizeTemplateID(requirement.Name)
		}
		if requirement.ID == "" {
			continue
		}
		if _, ok := seen[requirement.ID]; ok {
			continue
		}
		seen[requirement.ID] = struct{}{}
		out = append(out, requirement)
	}
	return out
}

func genericTitleWord(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(value))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func genericShortTitle(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) > 8 {
		parts = parts[:8]
	}
	return strings.Join(parts, " ")
}

func genericTruncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return strings.TrimSpace(value[:max-3]) + "..."
}

func genericTernaryString(ok bool, whenTrue, whenFalse string) string {
	if ok {
		return whenTrue
	}
	return whenFalse
}

func genericDedupeStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func genericKindAgentNames(kind string) (planner, executor, reviewer string) {
	switch kind {
	case "content":
		return "Research Lead", "Content Producer", "Analytics Lead"
	case "gtm":
		return "Pipeline Lead", "Campaign Builder", "Revenue Analyst"
	case "product":
		return "Discovery Lead", "Builder", "QA Lead"
	case "commerce":
		return "Catalog Lead", "Fulfillment Builder", "Conversion Analyst"
	case "support":
		return "Triage Lead", "Resolution Builder", "Quality Analyst"
	case "research":
		return "Research Lead", "Synthesis Builder", "Evidence Analyst"
	default:
		return "Planner", "Executor", "Reviewer"
	}
}
