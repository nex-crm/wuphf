package operations

import (
	"fmt"
	"sort"
	"strings"
)

type SynthesisInput struct {
	Directive    string
	Profile      CompanyProfile
	Integrations []RuntimeIntegration
	Capabilities []RuntimeCapability

	Name        string
	Description string
	Goals       string
	Size        string
	Priority    string
	Connections []string
}

func HasDirective(input SynthesisInput) bool {
	return strings.TrimSpace(strings.Join([]string{
		input.Directive,
		input.Profile.Name,
		input.Profile.Description,
		input.Profile.Industry,
		input.Profile.Audience,
		input.Profile.Offer,
		strings.Join(func() []string {
			values := make([]string, 0, len(input.Integrations)+len(input.Capabilities))
			for _, integration := range input.Integrations {
				values = append(values, integration.Name, integration.Provider)
			}
			for _, capability := range input.Capabilities {
				values = append(values, capability.Key, capability.Name, capability.Category)
			}
			return values
		}(), " "),
		input.Name,
		input.Description,
		input.Goals,
		input.Priority,
		strings.Join(input.Connections, " "),
	}, " ")) != ""
}

func SynthesizeBlueprint(input SynthesisInput) Blueprint {
	if hasGenericSynthesisInput(input) || !hasLegacySynthesisInput(input) {
		return synthesizeGenericBlueprint(input)
	}
	return synthesizeLegacyBlueprint(input)
}

func hasGenericSynthesisInput(input SynthesisInput) bool {
	return strings.TrimSpace(input.Directive) != "" ||
		strings.TrimSpace(input.Profile.Name) != "" ||
		strings.TrimSpace(input.Profile.Description) != "" ||
		strings.TrimSpace(input.Profile.Industry) != "" ||
		strings.TrimSpace(input.Profile.Audience) != "" ||
		strings.TrimSpace(input.Profile.Offer) != "" ||
		len(input.Integrations) > 0 ||
		len(input.Capabilities) > 0
}

func hasLegacySynthesisInput(input SynthesisInput) bool {
	return strings.TrimSpace(strings.Join([]string{
		input.Name,
		input.Description,
		input.Goals,
		input.Priority,
		strings.Join(input.Connections, " "),
	}, " ")) != ""
}

func synthesizeLegacyBlueprint(input SynthesisInput) Blueprint {
	name := firstOperationValue(input.Name, inferOperationName(input), "Autonomous Operation")
	slug := normalizeTemplateID(name)
	if slug == "" {
		slug = "autonomous-operation"
	}
	text := strings.ToLower(strings.Join([]string{input.Name, input.Description, input.Goals, input.Priority}, " "))
	kind := inferOperationKind(text)
	workstreams := inferOperationWorkstreams(kind)
	connectionIDs := normalizeConnectionIDs(input.Connections)
	artifactDefs := synthesizeArtifacts(kind)
	approvalRules := synthesizeApprovalRules()

	return Blueprint{
		ID:                 slug,
		Name:               name,
		Kind:               kind,
		Description:        firstOperationValue(input.Description, fmt.Sprintf("Synthesized operation blueprint for %s.", name)),
		Objective:          firstOperationValue(input.Goals, input.Priority, fmt.Sprintf("Stand up and run %s with durable workflows, tracked artifacts, and explicit approvals.", name)),
		Starter:            synthesizeStarterPlan(name, input, workstreams),
		EmployeeBlueprints: []string{},
		BootstrapConfig:    synthesizeBootstrapConfig(name, input, workstreams),
		MonetizationLadder: synthesizeMonetizationLadder(kind),
		QueueSeed:          synthesizeQueueSeed(input, workstreams),
		Automation:         synthesizeAutomationModules(connectionIDs),
		Stages:             synthesizeStages(workstreams),
		Artifacts:          artifactDefs,
		Capabilities:       synthesizeCapabilities(workstreams, connectionIDs),
		ApprovalRules:      approvalRules,
		Connections:        synthesizeConnectionBlueprints(connectionIDs),
		Workflows:          synthesizeWorkflows(name, connectionIDs),
	}
}

func inferOperationName(input SynthesisInput) string {
	if value := strings.TrimSpace(input.Priority); value != "" {
		return truncateWords(value, 5)
	}
	if value := strings.TrimSpace(input.Goals); value != "" {
		return truncateWords(value, 5)
	}
	if value := strings.TrimSpace(input.Description); value != "" {
		return truncateWords(value, 5)
	}
	return ""
}

func inferOperationKind(text string) string {
	switch {
	case containsAny(text, "youtube", "podcast", "newsletter", "content", "media", "audience"):
		return "content_operation"
	case containsAny(text, "sales", "gtm", "pipeline", "campaign", "outbound", "crm", "lead", "marketing"):
		return "gtm_operation"
	case containsAny(text, "product", "engineering", "platform", "repo", "application", "software", "code", "api"):
		return "product_delivery"
	case containsAny(text, "finance", "support", "operations", "workflow", "back office", "backoffice", "claims", "compliance"):
		return "business_operation"
	default:
		return "general_operation"
	}
}

func inferOperationWorkstreams(kind string) []string {
	switch kind {
	case "content_operation":
		return []string{"Research", "Production", "Distribution", "Monetization", "Analytics"}
	case "gtm_operation":
		return []string{"Positioning", "Pipeline", "Campaigns", "Enablement", "Analytics"}
	case "product_delivery":
		return []string{"Discovery", "Build", "Quality", "Release", "Feedback"}
	case "business_operation":
		return []string{"Intake", "Execution", "Controls", "Automation", "Review"}
	default:
		return []string{"Discovery", "Planning", "Execution", "Operations", "Review"}
	}
}

func synthesizeStarterPlan(name string, input SynthesisInput, workstreams []string) StarterPlan {
	leadSlug := "ceo"
	return StarterPlan{
		LeadSlug:                  leadSlug,
		GeneralChannelDescription: fmt.Sprintf("Command deck for %s. Use this room to steer the operation, approve risky actions, and unblock the specialists.", name),
		KickoffPrompt:             fmt.Sprintf("Stand up %s from scratch. Use the starter lanes, turn the objective into a repeatable operating system, create the first durable tasks, and keep external side effects behind human approval.", name),
		Agents: []StarterAgent{
			{Slug: "ceo", Emoji: "👔", Name: "CEO", Role: "Owns priorities, approvals, and escalation decisions", PermissionMode: "plan", Checked: true, Type: "lead", BuiltIn: true},
			{Slug: "planner", Emoji: "🧭", Name: "Planner", Role: "Turns objectives into workstreams, milestones, and task plans", PermissionMode: "plan", Checked: true, Type: "specialist", BuiltIn: true, Expertise: workstreams, Personality: "Clarifies scope quickly and turns ambiguity into an operating plan."},
			{Slug: "builder", Emoji: "🛠️", Name: "Builder", Role: "Implements the first deliverables and workflow assets", PermissionMode: "auto", Checked: true, Type: "specialist", BuiltIn: true, Expertise: []string{"Execution packets", "Workflow setup", "Deliverable assembly"}, Personality: "Biases toward shipping real artifacts, not commentary."},
			{Slug: "operator", Emoji: "⚙️", Name: "Operator", Role: "Owns operational handoffs, runbooks, and automation hygiene", PermissionMode: "plan", Checked: true, Type: "specialist", BuiltIn: true, Expertise: []string{"Runbooks", "Approvals", "Automation loops"}, Personality: "Keeps the system durable and visible."},
			{Slug: "analyst", Emoji: "📊", Name: "Analyst", Role: "Owns scorecards, metrics, and review loops", PermissionMode: "plan", Checked: true, Type: "specialist", BuiltIn: true, Expertise: []string{"KPIs", "Review cadence", "Operational feedback"}, Personality: "Turns activity into measurable outcomes and next-step signals."},
		},
		Channels: []StarterChannel{
			{Slug: "command", Name: "command", Description: fmt.Sprintf("Executive control room for %s.", name), Members: []string{"planner", "analyst", "operator"}},
			{Slug: "planning", Name: "planning", Description: "Scope the workstreams, milestones, and artifact plan.", Members: []string{"planner", "analyst"}},
			{Slug: "delivery", Name: "delivery", Description: "Build the first deliverables and execution bundles.", Members: []string{"builder", "operator"}},
			{Slug: "systems", Name: "systems", Description: "Wire integrations, approvals, and repeatable workflow loops.", Members: []string{"operator", "builder"}},
			{Slug: "review", Name: "review", Description: "Track metrics, review outcomes, and update the scorecard.", Members: []string{"analyst", "planner"}},
		},
		Tasks: []StarterTask{
			{Channel: "planning", Owner: "planner", Title: "Turn the directive into an operating plan", Details: firstOperationValue(input.Priority, input.Goals, input.Description, "Clarify scope, outcomes, constraints, and workstreams.")},
			{Channel: "delivery", Owner: "builder", Title: "Ship the first execution packet", Details: fmt.Sprintf("Create the first durable artifact bundle for %s.", name)},
			{Channel: "systems", Owner: "operator", Title: "Define the approval and automation loop", Details: "Map what can run automatically, what requires approval, and where workflow evidence should persist."},
			{Channel: "review", Owner: "analyst", Title: "Set the scorecard and review cadence", Details: "Define success metrics, review rhythm, and feedback loops for the first iteration."},
		},
	}
}

func synthesizeBootstrapConfig(name string, input SynthesisInput, workstreams []string) BootstrapConfig {
	slug := normalizeTemplateID(name)
	if slug == "" {
		slug = "autonomous-operation"
	}
	hooks := []string{"owned_audience", "services", "pilot", "retainer"}
	return BootstrapConfig{
		ChannelName:       name,
		ChannelSlug:       slug,
		Niche:             firstOperationValue(input.Description, "Autonomous operation design and execution"),
		Audience:          firstOperationValue(input.Description, "Operators, stakeholders, and the humans who approve risky changes"),
		Positioning:       firstOperationValue(input.Goals, input.Priority, "A repeatable operation with durable workflows and explicit approvals."),
		ContentPillars:    append([]string(nil), workstreams...),
		ContentSeries:     []string{"Discovery loop", "Execution loop", "Review loop", "Automation loop"},
		MonetizationHooks: hooks,
		PublishingCadence: "Weekly operating review",
		LeadMagnet: LeadMagnet{
			Name: fmt.Sprintf("%s operating brief", name),
			CTA:  "Open the operating brief",
			Path: "/" + slug + "/operating-brief",
		},
		MonetizationAsset: []MonetizationAsset{
			{Stage: "Day 0", Name: fmt.Sprintf("%s operating brief", name), Slot: "command_room", CTA: "Open the operating brief"},
			{Stage: "Review", Name: "Execution scorecard", Slot: "review_lane", CTA: "Open the scorecard"},
		},
		KPITracking: []KPI{
			{Name: "First durable workflow", Target: "Within 7 days", Why: "Proves the operation can move from planning into repeatable execution."},
			{Name: "Task throughput", Target: "4 closed tasks/week", Why: "Shows the office is producing output instead of only conversation."},
			{Name: "Approval latency", Target: "< 1 business day", Why: "Keeps humans in the loop without freezing the system."},
			{Name: "Repeatable wins", Target: "2 validated loops", Why: "Marks the transition from one-off heroics to a durable operation."},
		},
	}
}

func synthesizeMonetizationLadder(kind string) []MonetizationStep {
	steps := []string{"free diagnostic", "pilot", "retainer", "productized asset"}
	if kind == "content_operation" {
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
	return out
}

func synthesizeQueueSeed(input SynthesisInput, workstreams []string) []QueueItem {
	items := make([]QueueItem, 0, len(workstreams))
	firstTitle := firstOperationValue(input.Priority, input.Goals, "Define the first operating loop")
	for i, workstream := range workstreams {
		title := fmt.Sprintf("Stand up the %s lane", strings.ToLower(workstream))
		if i == 0 && strings.TrimSpace(firstTitle) != "" {
			title = firstTitle
		}
		items = append(items, QueueItem{
			ID:           fmt.Sprintf("work-%02d", i+1),
			Title:        title,
			Format:       "Operational work item",
			StageIndex:   minInt(i, 3),
			Score:        92 - i*6,
			UnitCost:     8 + i*2,
			Eta:          fmt.Sprintf("Iteration %d", i+1),
			Monetization: "approval-gated value capture",
			State:        "active",
		})
	}
	return items
}

func synthesizeAutomationModules(connectionIDs []string) []AutomationModule {
	connectionFooter := "No live external systems detected yet; keep execution internal until credentials are approved."
	status := "build_now"
	mode := "Internal first"
	if len(connectionIDs) > 0 {
		status = "ready_for_auth"
		mode = "Connection-aware"
		connectionFooter = "Connected systems were detected. Dry-run and mock paths can be wired immediately, but live mutations still require approval."
	}
	return []AutomationModule{
		{ID: "control-plane", Kicker: "Build now", Title: "Control plane + operating defaults", Copy: "Persist the objective, starter lanes, workflows, approvals, and scorecard in the broker-backed operation state.", Status: "build_now", Footer: "The operation should be replayable from durable state, not browser memory."},
		{ID: "artifact-engine", Kicker: "Build now", Title: "Artifact generation", Copy: "Turn work items into reusable artifact bundles that can be reviewed, handed off, and reused by future runs.", Status: "build_now", Footer: "Artifact definitions come from the synthesized blueprint."},
		{ID: "workflow-harness", Kicker: mode, Title: "Workflow smoke harness", Copy: "Create dry-run or mock workflows for the active integrations and persist evidence for each run.", Status: status, Footer: connectionFooter},
		{ID: "approval-plane", Kicker: "Build now", Title: "Approval boundaries", Copy: "Keep account linking, public sends, destructive changes, and spend behind explicit human approvals.", Status: "build_now", Footer: "The policy layer should stay generic across operations."},
		{ID: "review-loop", Kicker: "Build now", Title: "Metrics and review loop", Copy: "Track throughput, output quality, and approval latency so the operation improves each iteration.", Status: "build_now", Footer: "A weekly review loop is seeded by default."},
	}
}

func synthesizeStages(workstreams []string) []StageDefinition {
	stages := []StageDefinition{
		{ID: "discover", Name: "Discover", Engine: "planning", Description: "Clarify the objective, inputs, and workstreams.", ExitCriteria: "Objective and first work items are durable."},
		{ID: "plan", Name: "Plan", Engine: "planning", Description: "Turn the directive into an operating plan and artifact map.", ExitCriteria: "Starter tasks, channels, and artifact definitions exist."},
		{ID: "execute", Name: "Execute", Engine: "execution", Description: "Produce the first deliverables and workflow assets.", ExitCriteria: "The first execution bundle is reviewable."},
		{ID: "validate", Name: "Validate", Engine: "review", Description: "Review outputs, smoke-test workflows, and capture evidence.", ExitCriteria: "Evidence is persisted and approvals are explicit."},
		{ID: "operate", Name: "Operate", Engine: "operations", Description: "Run the loop repeatedly with scorecards and approvals.", ExitCriteria: "The operation has a repeatable rhythm."},
	}
	if len(workstreams) > 0 {
		stages[2].Description = fmt.Sprintf("Produce the first deliverables across %s.", strings.Join(workstreams[:minInt(len(workstreams), 3)], ", "))
	}
	return stages
}

func synthesizeArtifacts(kind string) []ArtifactType {
	artifacts := []ArtifactType{
		{ID: "objective_brief", Name: "Objective brief", Description: "Problem statement, scope, constraints, and success criteria."},
		{ID: "operating_plan", Name: "Operating plan", Description: "Workstreams, milestones, owners, and approval map."},
		{ID: "execution_packet", Name: "Execution packet", Description: "The current run's deliverables, checklist, and handoff context."},
		{ID: "workflow_run", Name: "Workflow run", Description: "Durable evidence for one dry-run, mock, or live execution."},
	}
	if kind == "content_operation" {
		artifacts[2] = ArtifactType{ID: "launch_packet", Name: "Launch packet", Description: "Ready-to-review bundle for the next outward-facing release."}
	}
	return artifacts
}

func synthesizeCapabilities(workstreams, connectionIDs []string) []CapabilityRequirement {
	capabilities := []CapabilityRequirement{
		{ID: "planning", Name: "Planning and decomposition", Kind: "planning", Description: "Turn the directive into workstreams, stages, and tasks."},
		{ID: "delivery", Name: "Execution and delivery", Kind: "execution", Description: "Build the first deliverables and execution bundles."},
		{ID: "operations", Name: "Operations and approvals", Kind: "execution", Description: "Run the workflows, enforce approvals, and keep state durable."},
		{ID: "analytics", Name: "Metrics and review", Kind: "analysis", Description: "Track outcomes, bottlenecks, and loop improvements."},
	}
	for _, id := range connectionIDs {
		capabilities = append(capabilities, CapabilityRequirement{
			ID:           id + "-integration",
			Name:         prettyOperationIdentifier(id) + " integration",
			Kind:         "integration",
			Integrations: []string{id},
			Description:  fmt.Sprintf("Use %s as an execution surface for the operation.", prettyOperationIdentifier(id)),
		})
	}
	return capabilities
}

func synthesizeApprovalRules() []ApprovalRule {
	return []ApprovalRule{
		{ID: "external-accounts", Trigger: "connect_account", Description: "Human approval required before linking or authorizing a real external account."},
		{ID: "public-send", Trigger: "public_send_or_publish", Description: "Human approval required before any public send, publish, or externally visible update."},
		{ID: "spend", Trigger: "spend_money", Description: "Human approval required before vendor spend, ad spend, or budget commitments."},
		{ID: "destructive-change", Trigger: "destructive_mutation", Description: "Human approval required before destructive external mutations or irreversible operational changes."},
		{ID: "commercial-commitment", Trigger: "contract_or_legal_change", Description: "Human approval required before legal or commercial commitments."},
	}
}

func synthesizeConnectionBlueprints(connectionIDs []string) []ConnectionBlueprint {
	out := make([]ConnectionBlueprint, 0, len(connectionIDs))
	for _, id := range connectionIDs {
		title := prettyOperationIdentifier(id)
		out = append(out, ConnectionBlueprint{
			Name:        title + " lane",
			Integration: id,
			Owner:       inferConnectionOwner(id),
			Priority:    inferConnectionPriority(id),
			Purpose:     fmt.Sprintf("Use %s as a connected execution surface for the active operation.", title),
			SmokeTest:   fmt.Sprintf("Run a dry-run or mock workflow against %s and persist durable evidence.", title),
			Blocker:     fmt.Sprintf("Needs approved %s credentials before any live external mutation.", title),
		})
	}
	return out
}

func synthesizeWorkflows(name string, connectionIDs []string) []WorkflowTemplate {
	if len(connectionIDs) == 0 {
		return []WorkflowTemplate{
			{
				ID:          "internal-kickoff-dry-run",
				Name:        "Internal kickoff dry-run",
				Trigger:     "manual",
				Mode:        "dry_run",
				Description: fmt.Sprintf("Validate the %s bootstrap path without touching external systems.", name),
				Checklist:   []string{"Generate the first artifact bundle", "Persist workflow evidence", "Confirm approval boundaries"},
				Definition:  map[string]any{"provider": "one", "key": "internal-kickoff-dry-run"},
				SmokeTest: WorkflowSmokeTest{
					Name:  "Internal kickoff dry-run",
					Mode:  "dry_run",
					Proof: "Workflow evidence persisted without requiring external credentials.",
					Inputs: map[string]any{
						"operation_name": name,
					},
				},
			},
		}
	}
	out := make([]WorkflowTemplate, 0, len(connectionIDs))
	for _, id := range connectionIDs {
		key := normalizeTemplateID(id + "-smoke-test")
		title := prettyOperationIdentifier(id)
		out = append(out, WorkflowTemplate{
			ID:           key,
			Name:         title + " smoke test",
			Trigger:      "manual",
			Mode:         "dry_run",
			Integrations: []string{id},
			Description:  fmt.Sprintf("Validate the %s integration path for %s without irreversible side effects.", title, name),
			Checklist:    []string{"Confirm connection health", "Execute a dry-run or mock step", "Persist workflow evidence", "Route any blockers to the owner"},
			Definition: map[string]any{
				"provider": "one",
				"key":      key,
			},
			SmokeTest: WorkflowSmokeTest{
				Name:  title + " smoke test",
				Mode:  "dry_run",
				Proof: fmt.Sprintf("%s workflow evidence persisted successfully.", title),
				Inputs: map[string]any{
					"operation_name": name,
					"integration":    id,
				},
			},
		})
	}
	return out
}

func normalizeConnectionIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		id := normalizeTemplateID(value)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func inferConnectionOwner(id string) string {
	switch {
	case containsAny(id, "github", "gitlab", "linear", "jira"):
		return "Engineering"
	case containsAny(id, "gmail", "outlook", "slack", "calendar", "teams"):
		return "Operations"
	case containsAny(id, "salesforce", "hubspot", "attio"):
		return "Revenue"
	case containsAny(id, "drive", "notion", "dropbox"):
		return "Knowledge"
	default:
		return "Operations"
	}
}

func inferConnectionPriority(id string) string {
	switch {
	case containsAny(id, "gmail", "slack", "github", "salesforce", "hubspot"):
		return "critical"
	default:
		return "important"
	}
}

func prettyOperationIdentifier(value string) string {
	parts := strings.Fields(strings.NewReplacer("-", " ", "_", " ", ".", " ").Replace(strings.TrimSpace(value)))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func firstOperationValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, strings.ToLower(value)) {
			return true
		}
	}
	return false
}

func truncateWords(value string, limit int) string {
	parts := strings.Fields(strings.TrimSpace(value))
	if len(parts) <= limit {
		return strings.Join(parts, " ")
	}
	return strings.Join(parts[:limit], " ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
