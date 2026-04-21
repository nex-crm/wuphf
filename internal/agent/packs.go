package agent

// PackSkillSpec defines a skill to pre-seed when a pack is first launched.
type PackSkillSpec struct {
	Name        string
	Title       string
	Description string
	Tags        []string
	Trigger     string
	Content     string
}

// PackDefinition defines a team of agents that work together.
type PackDefinition struct {
	Slug          string
	Name          string
	Description   string
	LeadSlug      string
	Agents        []AgentConfig
	DefaultSkills []PackSkillSpec
}

// revopsDriveConnection is the shared prelude for RevOps skills. It tells the
// agent not to fabricate data and to walk the user through connecting the
// required tool end-to-end before doing any work.
//
// Skills are provider-agnostic by design. Which backend actually serves a
// call (One CLI or Composio) is decided by the action Registry in
// internal/action/registry.go, not by skill content. One is preferred because
// it is local and personal; Composio is the fallback for tools One does not
// cover. If you need to change provider priority, change it in the Registry,
// not here.
const revopsDriveConnection = `## Step 0: Drive the connection before you start

This skill acts on real company data. Never fabricate deals, contacts, pipeline numbers, or activity. If the required integration is not connected, DRIVE the user through connecting it end-to-end before you do any work.

1. Call **team_action_connections** to see what is already connected. The framework picks the right backend automatically — you do not need to reason about One versus Composio.
2. If what you need is already connected, skip to step 4.
3. The integration is missing. Drive the user through connecting it:
   a. Ask via **human_interview** which tool they use. Give concrete options:
      - CRM: HubSpot, Salesforce, Attio, Pipedrive, Zoho, Close, Copper, Other
      - If the skill also needs email / calendar / outbound, ask for that tool too: Gmail, Outlook, Google Calendar, Apollo, Outreach, Salesloft, SendGrid, or manual
   b. Call **team_action_guide** with the tool name. The guide returns step-by-step setup instructions for the backend the framework selected (set a config key, authorize an account, run a CLI command, etc.). Walk the user through each step. Wait for confirmation after each.
   c. Re-call **team_action_connections** to verify. Iterate on failures until connected.
   d. If **team_action_guide** reports that no configured backend supports the tool, offer three options via **human_interview**:
      1. Pick a supported tool instead — list what the guide surfaces for common categories.
      2. Ask you to propose a dedicated skill for the tool. Draft an instruction-based skill that wraps its public API and save it for review.
      3. Provide an API key and base URL for the tool. Save it via ` + "`/config set <tool>_api_key <value>`" + ` and make direct HTTP calls. Gate every write on **human_interview**.
4. Once connected, use **team_action_search** to discover the action slug and **team_action_execute** to run it.

If the user explicitly says "skip" or "work from context only", proceed using Nex and the thread alone. Flag "Data source: thread + Nex only, no live data" at the top of your output so the gap is visible.

`

// legacyPacks retains the old hard-coded pack registry strictly as a
// compatibility fallback for callers that have not yet moved to operation
// blueprints.
var legacyPacks = []PackDefinition{
	{
		Slug:        "starter",
		Name:        "Starter Team",
		Description: "CEO, engineer, and GTM — the three roles that actually ship and sell",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy", "decision-making", "prioritization", "delegation", "orchestration"}, Personality: "Strategic leader who breaks down directives into clear specialist assignments", PermissionMode: "plan"},
			{Slug: "eng", Name: "Founding Engineer", Expertise: []string{"full-stack", "backend", "frontend", "APIs", "databases", "architecture", "DevOps"}, Personality: "Scrappy full-stack engineer who ships fast and keeps the system simple until it needs to be complex", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(go*,git*,npm*,make*)"}},
			{Slug: "gtm", Name: "GTM Lead", Expertise: []string{"go-to-market", "sales", "outreach", "positioning", "content", "pipeline", "ICP", "growth"}, Personality: "Revenue-focused generalist who handles the full GTM motion from messaging to closed deals", PermissionMode: "plan"},
		},
	},
	{
		Slug:        "founding-team",
		Name:        "Founding Team",
		Description: "Full autonomous company — CEO delegates to specialists",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy", "decision-making", "prioritization", "delegation", "orchestration"}, Personality: "Strategic leader who breaks down complex directives into clear specialist assignments", PermissionMode: "plan"},
			{Slug: "pm", Name: "Product Manager", Expertise: []string{"roadmap", "user-stories", "requirements", "prioritization", "specs"}, Personality: "Detail-oriented PM who translates business needs into actionable specs", PermissionMode: "plan"},
			{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React", "CSS", "UI-UX", "components"}, Personality: "Frontend specialist focused on clean, accessible implementations", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(npm*)"}},
			{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend", "APIs", "databases", "infrastructure", "architecture"}, Personality: "Backend engineer focused on reliable, scalable systems", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(go*,git*)"}},
			{Slug: "ai", Name: "AI Engineer", Expertise: []string{"LLMs", "AI-product-design", "retrieval", "evaluations", "agents", "model-integration"}, Personality: "AI engineer focused on making model-powered features reliable, useful, and actually shippable", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(curl*,python*,pip*)"}},
			{Slug: "designer", Name: "Designer", Expertise: []string{"UI-UX-design", "branding", "visual-systems", "prototyping"}, Personality: "Creative designer who balances aesthetics with usability", PermissionMode: "plan"},
			{Slug: "cmo", Name: "CMO", Expertise: []string{"marketing", "content", "brand", "growth", "analytics", "campaigns"}, Personality: "Growth-focused marketer who drives awareness and engagement", PermissionMode: "plan"},
			{Slug: "cro", Name: "CRO", Expertise: []string{"sales", "pipeline", "revenue", "partnerships", "outreach", "closing"}, Personality: "Revenue-driven closer who builds pipeline and converts deals", PermissionMode: "plan"},
		},
	},
	{
		Slug:        "coding-team",
		Name:        "Coding Team",
		Description: "High-velocity software development team",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"architecture", "code-review", "technical-decisions", "planning"}, Personality: "Senior technical leader who makes sound architectural decisions and coordinates the team"},
			{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React", "CSS", "components", "accessibility"}, Personality: "Frontend specialist focused on clean, accessible implementations"},
			{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend", "APIs", "databases", "DevOps", "infrastructure"}, Personality: "Backend engineer focused on reliable, scalable systems"},
			{Slug: "qa", Name: "QA Engineer", Expertise: []string{"testing", "automation", "quality", "edge-cases", "CI-CD"}, Personality: "Quality-focused engineer who catches issues before they reach production"},
		},
	},
	{
		Slug:        "lead-gen-agency",
		Name:        "Lead Gen Agency",
		Description: "Quiet outbound systems and automated GTM",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"prospecting", "outreach", "pipeline", "closing", "negotiation", "revenue-leadership"}, Personality: "Seasoned closer who builds relationships, converts opportunities, and sets the outbound strategy"},
			{Slug: "sdr", Name: "SDR", Expertise: []string{"cold-outreach", "qualification", "booking-meetings", "sequences"}, Personality: "Persistent SDR who opens doors and qualifies opportunities"},
			{Slug: "research", Name: "Research Analyst", Expertise: []string{"market-research", "competitive-analysis", "ICP-profiling", "trends"}, Personality: "Analytical researcher who surfaces actionable intelligence"},
			{Slug: "content", Name: "Content Strategist", Expertise: []string{"SEO", "copywriting", "nurture-sequences", "thought-leadership"}, Personality: "Strategic writer who creates content that drives engagement"},
		},
	},
	{
		Slug:        "revops",
		Name:        "RevOps Team",
		Description: "Revenue operations team — CRM hygiene, pipeline health, and GTM execution",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{
				Slug:           "ceo",
				Name:           "Chief Revenue Officer",
				Expertise:      []string{"revenue-leadership", "GTM-strategy", "prioritization", "delegation", "orchestration", "forecasting"},
				Personality:    "Revenue-obsessed leader who breaks down GTM directives into clear specialist assignments. Routes CRM hygiene to the analyst, deal work to the AE, outbound to the SDR, and keeps ops-lead focused on pipeline mechanics.",
				PermissionMode: "plan",
			},
			{
				Slug:           "ops-lead",
				Name:           "Revenue Operations Lead",
				Expertise:      []string{"revenue-operations", "GTM-strategy", "pipeline-management", "forecasting", "CRM", "data-quality", "process-design"},
				Personality:    "Data-driven RevOps lead who spots pipeline leaks, enforces CRM discipline, and keeps the GTM machine humming. Reports to the CRO on pipeline health and process improvements.",
				PermissionMode: "plan",
			},
			{
				Slug:           "ae",
				Name:           "Account Executive",
				Expertise:      []string{"pipeline", "deal-management", "closing", "negotiation", "stakeholder-mapping", "discovery", "objection-handling"},
				Personality:    "Seasoned AE focused on moving deals forward. Keeps detailed notes, flags stalled opportunities early, and knows when to escalate vs. push through.",
				PermissionMode: "plan",
			},
			{
				Slug:           "sdr",
				Name:           "SDR",
				Expertise:      []string{"outbound", "cold-outreach", "prospecting", "sequences", "qualification", "re-engagement", "ICP-targeting"},
				Personality:    "High-output SDR who writes sharp, relevant outreach. Understands that personalization beats volume and always ties messaging to business context.",
				PermissionMode: "plan",
			},
			{
				Slug:           "analyst",
				Name:           "Revenue Analyst",
				Expertise:      []string{"CRM-hygiene", "data-quality", "lead-scoring", "reporting", "funnel-analysis", "attribution", "forecasting"},
				Personality:    "Methodical analyst who treats the CRM as a source of truth, not a filing cabinet. Flags data gaps, builds scoring models, and turns pipeline data into decisions.",
				PermissionMode: "plan",
			},
		},
		DefaultSkills: []PackSkillSpec{
			{
				Name:        "CRM Hygiene Audit",
				Title:       "CRM Hygiene Audit",
				Description: "Audit CRM for stale contacts, missing fields, and data quality issues",
				Tags:        []string{"crm", "data-quality", "ops"},
				Trigger:     "When asked to audit the CRM, check data quality, or find stale records",
				Content: `You are performing a CRM hygiene audit. Your goal is to surface data quality issues so the team can keep the CRM accurate and actionable.

` + revopsDriveConnection + `## What to do

1. **Query Nex for context first**: Ask "What contacts, companies, or deals in our CRM are most at risk from data gaps?" Nex may surface patterns before you hit the CRM directly.

2. **Pull live CRM data** via **team_action_execute** using the actions you discovered in Step 0. Audit across:
   - Contacts missing email, phone, or job title
   - Companies missing industry, size, or website
   - Open deals with no activity in 14+ days
   - Deals missing close date or next step
   - Leads with no owner assigned

3. **Prioritize by revenue impact**: a stale $200k opportunity matters more than a missing phone number on a cold contact.

4. **Structure your output** as a prioritized list:
   - Critical (blocks forecasting): deals missing close date, stage, or ARR
   - High (degrades pipeline quality): open opps with no recent activity
   - Medium (reduces signal): contacts missing key fields
   - Low (nice-to-have): cosmetic or optional fields

5. **For each issue**, include: what is missing, how many records, and a recommended fix action.

6. **Propose fixes** where you can automate them. Gate destructive changes (bulk delete, stage resets) on human approval via **human_interview** with the exact change count.

## Output format

Post findings to #general as a summary table with issue, count, impact, and recommended action.`,
			},
			{
				Name:        "Meeting Prep Brief",
				Title:       "Meeting Prep Brief",
				Description: "Prepare reps with a concise brief before customer or prospect meetings",
				Tags:        []string{"sales", "meetings", "prep", "crm"},
				Trigger:     "When asked to prep for a meeting, generate a brief, or summarize a prospect before a call",
				Content: `You are preparing a meeting brief for an upcoming sales or customer call. Your goal is to give the rep everything they need to walk in sharp.

` + revopsDriveConnection + `## What to do

1. **Identify the meeting**: Who is it with? What company? What stage?

2. **Query Nex for context**:
   - "What do we know about [company name] — their business, buying signals, and recent activity?"
   - "What is the current status of our relationship or deal with [company name]?"
   - "Are there any open action items, blockers, or commitments from previous interactions?"

3. **Pull live data** via **team_action_execute** against whatever CRM / calendar / email is connected:
   - Last interaction date and type
   - Deal stage, ARR, close date
   - Key stakeholders and their roles
   - Any recorded objections or concerns

4. **Research the prospect** (if first meeting): company size, industry, recent news, tech stack.

5. **Build the brief** with:
   - **Who you're meeting**: name, title, company, decision-maker status
   - **Where we are**: deal stage, last touch, next step on file
   - **Context from Nex**: buying signals, known pain points, priorities
   - **Your agenda**: 2-3 suggested talking points for the current stage
   - **Watch-outs**: open objections, competitor mentions, red flags, data gaps
   - **Ask**: the one clear ask for this call (demo booked, POC scoped, legal introduced)

6. **Keep it under one page**. If the rep has to scroll past the fold, it is too long.

Post the brief to #general tagged with the rep's name and meeting time.`,
			},
			{
				Name:        "Closed-Lost Re-engagement",
				Title:       "Closed-Lost Re-engagement",
				Description: "Re-engage closed-lost deals that may be ready to revisit",
				Tags:        []string{"sales", "re-engagement", "closed-lost", "outbound"},
				Trigger:     "When asked to find re-engagement opportunities, surface closed-lost deals, or run a win-back campaign",
				Content: `You are running a closed-lost re-engagement motion. Your goal is to find deals that went cold but may now be worth revisiting, and draft outreach that is worth opening.

` + revopsDriveConnection + `## What to do

1. **Query Nex for signal**:
   - "Are there closed-lost deals where the company has since had a leadership change, funding event, or relevant trigger?"
   - "Which lost deals had the most positive engagement before they closed-lost?"
   - "What were the most common reasons we lost deals in the last 6 months?"

2. **Pull closed-lost deals** via **team_action_execute** against the CRM. Filter by:
   - Lost 3-18 months ago (not too fresh, not too stale)
   - Lost reason: timing, budget, or internal priority — not product fit
   - Company has had a trigger event: new funding, new exec, product launch, hiring surge
   - Deal size was meaningful (above your ACV floor)

3. **Score each opportunity** (1-5):
   - 5: strong fit, timing trigger, positive prior relationship
   - 3: good fit, no clear trigger, but worth a touch
   - 1: bad fit or hard no, skip entirely

4. **Draft re-engagement messages** for each scored 4+:
   - Reference the specific trigger event ("I saw you just raised a Series B...")
   - Acknowledge the prior conversation briefly without being weird about it
   - Lead with what has changed on your side
   - One clear ask: 20-minute catch-up, not a full demo

5. **Gate sending** on human approval via **human_interview**. Present the draft list with scores and messages. Never send outbound email without approval.

Output a re-engagement queue: company, deal size, lost reason, trigger event, score, draft message, and which channel will send it. Post to #general.`,
			},
			{
				Name:        "Deals Going Dark",
				Title:       "Deals Going Dark",
				Description: "Surface active pipeline deals with no recent activity before they go cold",
				Tags:        []string{"pipeline", "sales", "alerts", "crm"},
				Trigger:     "When asked to check pipeline health, find stalled deals, or surface deals with no recent activity",
				Content: `You are running a pipeline health check to surface deals at risk of going dark before they are formally lost.

` + revopsDriveConnection + `## What to do

1. **Query Nex for deal context**:
   - Ask "Which of our open deals have had no recent activity or contact?"
   - Ask "Are there any deals where the champion has gone quiet or changed roles?"
   - Ask "What deals are approaching their close date without a clear next step?"

2. **Pull open pipeline** via **team_action_execute** against the connected CRM:
   - Filter deals with no logged activity (call, email, meeting) in 10+ days
   - Flag deals where close date is within 30 days but no next step is set
   - Flag deals where the primary contact hasn't responded to the last 2 touches

3. **Assess risk by stage**:
   - Late stage (proposal/negotiation) going dark: critical — flag immediately
   - Mid stage (demo/evaluation) going dark: high — needs a nudge within 48 hours
   - Early stage (discovery) going dark: medium — assess fit before re-investing

4. **For each at-risk deal**, diagnose the likely cause:
   - Champion went quiet: internal blocker, competing priority, or lost sponsor
   - No next step: last meeting ended without a commitment
   - Approaching close date: artificial deadline that wasn't real, or deal is slipping

5. **Recommend a specific action** for each deal:
   - "Send a 'just checking in' with a clear ask"
   - "Reach out to a second stakeholder to triangulate"
   - "Propose a 2-week extension and reset the close date"
   - "Mark at-risk in CRM and flag for forecast call"

6. **Post a pipeline health report** to #general:
   - Red (critical, needs action today): deal name, stage, days dark, recommended action
   - Amber (needs attention this week): same format
   - Green (healthy): summary count only

Gate any CRM stage updates on human review via human_interview.`,
			},
			{
				Name:        "Lead Scoring",
				Title:       "Lead Scoring",
				Description: "Score and prioritize inbound leads by fit and buying intent",
				Tags:        []string{"leads", "scoring", "qualification", "crm"},
				Trigger:     "When asked to score leads, prioritize inbound, or identify best-fit prospects",
				Content: `You are scoring inbound leads to help the team focus time on the prospects most likely to convert.

` + revopsDriveConnection + `## What to do

1. **Query Nex for ICP and playbook context**:
   - Ask "What does our ideal customer profile look like — industry, size, buying signals?"
   - Ask "What are the strongest indicators that a lead is ready to buy?"
   - Ask "Which lead sources have historically converted best?"

2. **Pull the lead list** via **team_action_execute** against the connected CRM:
   - Unworked leads added in the last 14 days
   - Leads that re-engaged (opened email, visited pricing page, booked a demo)
   - MQLs that haven't been contacted within 48 hours

3. **Score each lead** across two dimensions:

   **Fit score (1-5)** — how well do they match the ICP?
   - 5: Perfect match on industry, size, and use case
   - 3: Partial match — one dimension off
   - 1: Clear mismatch — wrong segment

   **Intent score (1-5)** — how ready are they to buy?
   - 5: Demo request, pricing page visit, or inbound inquiry
   - 3: Content download, webinar attendance, or re-engagement
   - 1: Cold list import or conference badge scan

4. **Tier the leads**:
   - Tier 1 (fit 4-5 + intent 3-5): route to AE immediately
   - Tier 2 (fit 3+ + intent 2+): SDR sequence within 24 hours
   - Tier 3 (fit ≤2 or intent 1): nurture or disqualify

5. **For each Tier 1 lead**, include a suggested opening line for the AE based on the lead's company, role, and most recent action.

6. **Post the scored lead list** to #general with: lead name, company, fit score, intent score, tier, and suggested action. Flag any Tier 1 leads that have been sitting more than 24 hours without contact.

Gate any CRM field updates (score, tier, owner assignment) on human approval via human_interview.`,
			},
		},
	},
}

// ListLegacyPacks returns a copy of the compatibility pack registry.
func ListLegacyPacks() []PackDefinition {
	out := make([]PackDefinition, 0, len(legacyPacks))
	for _, pack := range legacyPacks {
		cloned := pack
		cloned.Agents = append([]AgentConfig(nil), pack.Agents...)
		cloned.DefaultSkills = append([]PackSkillSpec(nil), pack.DefaultSkills...)
		out = append(out, cloned)
	}
	return out
}

// LookupLegacyPack returns the compatibility pack with the given slug, or nil
// if not found.
func LookupLegacyPack(slug string) *PackDefinition {
	for i := range legacyPacks {
		if legacyPacks[i].Slug == slug {
			pack := legacyPacks[i]
			pack.Agents = append([]AgentConfig(nil), legacyPacks[i].Agents...)
			pack.DefaultSkills = append([]PackSkillSpec(nil), legacyPacks[i].DefaultSkills...)
			return &pack
		}
	}
	return nil
}

// GetPack is a deprecated compatibility alias for LookupLegacyPack.
func GetPack(slug string) *PackDefinition { return LookupLegacyPack(slug) }

// PackSlugs returns the list of all registered pack slugs, in registration order.
func PackSlugs() []string {
	slugs := make([]string, 0, len(legacyPacks))
	for i := range legacyPacks {
		slugs = append(slugs, legacyPacks[i].Slug)
	}
	return slugs
}
