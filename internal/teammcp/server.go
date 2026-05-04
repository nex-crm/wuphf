package teammcp

import (
	"context"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

var reconfigureOfficeSessionFn = reconfigureLiveOffice

func boolPtr(v bool) *bool { return &v }

func readOnlyTool(name, description string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(false),
		},
	}
}

func officeWriteTool(name, description string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}
}

func officeDestructiveTool(name, description string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{
			DestructiveHint: boolPtr(true),
			OpenWorldHint:   boolPtr(false),
		},
	}
}

func Run(ctx context.Context) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "wuphf-team",
		Version: "0.1.0",
	}, nil)

	server.AddReceivingMiddleware(agentToolEventMiddleware)
	configureServerTools(server, resolveSlugOptional(""), strings.TrimSpace(os.Getenv("WUPHF_CHANNEL")), isOneOnOneMode())
	return server.Run(ctx, &mcp.StdioTransport{})
}

// registerSharedMemoryTools registers the active shared-memory / wiki tool
// set on the server. Markdown-backend installs expose team_wiki_* tools;
// nex/gbrain installs expose the legacy team_memory_* tools; `none` skips
// them entirely. Both tool sets NEVER coexist — agents see exactly one.
func registerSharedMemoryTools(server *mcp.Server) {
	switch config.ResolveMemoryBackend("") {
	case config.MemoryBackendMarkdown:
		mcp.AddTool(server, officeWriteTool(
			"team_wiki_write",
			"Write directly to the canonical team wiki git repo. Use this for already-approved canonical edits, bootstrap/admin updates, or explicit human requests. For agent-authored working notes, observations, draft playbooks, and proposed new wiki knowledge, write to notebook_write first and submit with notebook_promote so the review gate runs. The content you pass becomes the article bytes; this tool does not rewrite for you. Picks author identity from my_slug so git log shows which agent wrote each article. Images are supported via standard markdown: embed a remote URL with `![alt text](https://example.com/diagram.png)` and the wiki renderer will show it inline. Use images you found on the web while researching the article; do not upload bytes — only reference URLs.",
		), handleTeamWikiWrite)
		mcp.AddTool(server, readOnlyTool(
			"team_wiki_read",
			"Read an article from the team wiki. Call this when the index lists an article relevant to your task.",
		), handleTeamWikiRead)
		mcp.AddTool(server, readOnlyTool(
			"team_wiki_search",
			"Literal substring search across the team wiki. Use for lookups the index does not surface.",
		), handleTeamWikiSearch)
		mcp.AddTool(server, readOnlyTool(
			"team_wiki_list",
			"Return the auto-regenerated catalog (index/all.md) of every article in the team wiki.",
		), handleTeamWikiList)
		mcp.AddTool(server, readOnlyTool(
			"wuphf_wiki_lookup",
			"Cited-answer lookup against the team wiki. Returns a structured JSON answer with sources and inline citations. Use when you need a verified, sourced answer rather than a raw search.",
		), handleTeamWikiLookup)
		// Notebook tools ride on the same markdown backend. Registered here
		// so they share the WUPHF_MEMORY_BACKEND gate with team_wiki_*.
		registerNotebookTools(server)
		// Entity brief tools (v1.2) — fact log + broker-level synthesis.
		// Same backend gate: entity briefs live in the wiki subtree.
		registerEntityTools(server)
		// Master Inbox tools — draft reply, add label, get prospect.
		// Gated on WUPHF_MASTERINBOX_API_KEY so they only appear when
		// the Master Inbox bridge is configured.
		if os.Getenv("WUPHF_MASTERINBOX_API_KEY") != "" {
			registerMasterInboxTools(server)
		}
		// Playbook compilation tools (v1.3) — compile team/playbooks/*.md
		// into invokable skills + record execution outcomes. Same markdown
		// substrate, so the backend gate is unchanged.
		registerPlaybookTools(server)
		// Team learnings — typed reusable memory stored in the wiki.
		registerLearningTools(server)
		// Lint tools (Slice 1 wiki intelligence) — daily health check +
		// contradiction resolution. Same markdown substrate.
		mcp.AddTool(server, readOnlyTool(
			"run_lint",
			"Run the wiki lint check. Flags contradictions (critical), orphans (warning), stale claims (warning), missing cross-refs (info), and dedup review (info). Returns LintReport JSON with findings and resolve actions.",
		), handleRunLint)
		mcp.AddTool(server, officeWriteTool(
			"resolve_contradiction",
			"Resolve a contradiction finding from a prior run_lint call. winner must be A (first fact wins), B (second fact wins), or Both (acknowledge both as valid).",
		), handleResolveContradiction)
	case config.MemoryBackendNone:
		// Nothing — user explicitly disabled shared memory.
	default:
		// nex / gbrain (default): legacy tool set unchanged.
		mcp.AddTool(server, readOnlyTool(
			"team_memory_query",
			"Query your private notes and, when configured, shared organizational memory. Results may suggest which teammate to ask for fresher working context.",
		), handleTeamMemoryQuery)
		mcp.AddTool(server, officeWriteTool(
			"team_memory_write",
			"Store a private note by default, or write directly to shared durable memory when the result is real. Durable private notes may be flagged as promotion candidates.",
		), handleTeamMemoryWrite)
		mcp.AddTool(server, officeWriteTool(
			"team_memory_promote",
			"Promote one of your private notes into shared durable memory after it becomes canonical.",
		), handleTeamMemoryPromote)
	}
}

func configureServerTools(server *mcp.Server, slug string, channel string, oneOnOne bool) {
	if oneOnOne {
		mcp.AddTool(server, officeWriteTool(
			"reply",
			"Send your reply to the human in the direct 1:1 conversation.",
		), handleTeamBroadcast)

		mcp.AddTool(server, readOnlyTool(
			"read_conversation",
			"LAST RESORT: Read recent 1:1 messages only when the pushed notification is missing context you genuinely need. Do NOT call this before every reply.",
		), handleTeamPoll)

		mcp.AddTool(server, officeWriteTool(
			"human_interview",
			"Ask the human an interview question. If they dismiss it, or send another message in this channel/thread, the interview is canceled.",
		), handleHumanInterview)

		mcp.AddTool(server, officeWriteTool(
			"human_message",
			"Send a direct human-facing note into the chat when you need to present completion, recommend a decision, or tell the human what they should do next.",
		), handleHumanMessage)

		registerContextTools(server)
		registerSharedMemoryTools(server)

		registerSkillAuthoringTools(server)

		mcp.AddTool(server, readOnlyTool(
			"team_runtime_state",
			"Return the canonical runtime snapshot for this direct session, including tasks, pending human requests, recovery summary, and runtime capabilities.",
		), handleTeamRuntimeState)

		if hasActionProvider() {
			registerActionTools(server)
		}
		return
	}

	// ─── Role-based tool registration ───
	// Each role gets only the tools it needs. Cuts MCP schema overhead
	// from ~125k tokens (27 tools) down to ~15k (4 tools in DM mode).
	isDM := strings.HasPrefix(channel, "dm-")
	isLead := slug == "" || slug == "ceo"

	// DM mode: minimal tool set (same as 1:1 mode)
	if isDM {
		mcp.AddTool(server, officeWriteTool(
			"team_broadcast",
			"Reply in the conversation.",
		), handleTeamBroadcast)
		mcp.AddTool(server, readOnlyTool(
			"team_poll",
			"Read recent messages.",
		), handleTeamPoll)
		mcp.AddTool(server, officeWriteTool(
			"human_message",
			"Send a direct note to the human.",
		), handleHumanMessage)
		mcp.AddTool(server, officeWriteTool(
			"human_interview",
			"Ask the human an interview question. If they dismiss it, or send another message in this channel/thread, the interview is canceled.",
		), handleHumanInterview)
		registerContextTools(server)
		registerSharedMemoryTools(server)
		mcp.AddTool(server, officeWriteTool(
			"team_skill_run",
			"Invoke a named team skill. When the human's request matches an available skill, call this BEFORE replying — do not freelance. Bumps the skill's usage, logs a skill_invocation to the channel, and returns the skill's canonical step-by-step content for you to follow.",
		), handleTeamSkillRun)
		registerSkillAuthoringTools(server)
		if hasActionProvider() {
			registerActionTools(server)
		}
		return
	}

	// Office mode: core tools for all agents
	mcp.AddTool(server, officeWriteTool(
		"team_broadcast",
		"Post a message to the channel.",
	), handleTeamBroadcast)
	mcp.AddTool(server, readOnlyTool(
		"team_poll",
		"Read recent channel messages. Only when pushed context is insufficient.",
	), handleTeamPoll)
	mcp.AddTool(server, readOnlyTool(
		"team_inbox",
		"Read only the messages that currently belong in your agent inbox: human asks, CEO guidance, tags to you, and replies in your threads.",
	), handleTeamInbox)
	mcp.AddTool(server, readOnlyTool(
		"team_outbox",
		"Read only the messages you authored, so you can review what you already told the office.",
	), handleTeamOutbox)

	mcp.AddTool(server, officeWriteTool(
		"team_status",
		"Share a short status update in the team channel. This is rendered as lightweight activity in the channel UI.",
	), handleTeamStatus)

	mcp.AddTool(server, readOnlyTool(
		"team_members",
		"List active participants in the shared team channel with their latest visible activity.",
	), handleTeamMembers)

	mcp.AddTool(server, readOnlyTool(
		"team_office_members",
		"List the office-wide roster, including members who are not in the current channel.",
	), handleTeamOfficeMembers)

	mcp.AddTool(server, readOnlyTool(
		"team_channels",
		"List available office channels, their descriptions, and their memberships. Agents can see channel metadata even when they are not members.",
	), handleTeamChannels)

	mcp.AddTool(server, officeWriteTool(
		"team_dm_open",
		"Open or find a direct message channel with the human. Use this when the human explicitly asks to DM an agent. Agent-to-agent DMs are not allowed — all inter-agent communication must happen in public channels.",
	), handleTeamDMOpen)

	mcp.AddTool(server, readOnlyTool(
		"team_tasks",
		"List the current shared tasks and who owns them so the team does not duplicate work.",
	), handleTeamTasks)

	mcp.AddTool(server, readOnlyTool(
		"team_task_status",
		"Summarize how many shared tasks are running and whether any are isolated in local worktrees.",
	), handleTeamTaskStatus)

	mcp.AddTool(server, readOnlyTool(
		"team_runtime_state",
		"Return the canonical office runtime snapshot, including tasks, pending human requests, recovery summary, and runtime capabilities.",
	), handleTeamRuntimeState)

	mcp.AddTool(server, officeWriteTool(
		"team_task",
		"Create, claim, assign, complete, block, resume, or release a shared task in the office task list.",
	), handleTeamTask)

	if slug == "artist" {
		registerImageTools(server)
	}
	registerContextTools(server)
	registerSharedMemoryTools(server)

	mcp.AddTool(server, readOnlyTool(
		"team_requests",
		"List the current office requests so you know whether the human already owes the team a decision.",
	), handleTeamRequests)

	mcp.AddTool(server, officeWriteTool(
		"team_request",
		"Create a structured request for the human: confirmation, choice, approval, freeform answer, or private/secret answer.",
	), handleTeamRequest)

	mcp.AddTool(server, officeWriteTool(
		"human_interview",
		"Ask the human an interview question. If they dismiss it, or send another message in this channel/thread, the interview is canceled.",
	), handleHumanInterview)

	mcp.AddTool(server, officeWriteTool(
		"human_message",
		"Send a direct note to the human.",
	), handleHumanMessage)
	mcp.AddTool(server, officeWriteTool(
		"team_react",
		"React to a message with an emoji.",
	), handleTeamReact)
	mcp.AddTool(server, officeWriteTool(
		"team_skill_run",
		"Invoke a named team skill. When the request matches an available skill (see the skill list in your prompt), call this BEFORE doing the work — do not freelance. Bumps the skill's usage, logs a skill_invocation in the channel so the office sees you followed the playbook, and returns the skill's canonical step-by-step content for you to execute.",
	), handleTeamSkillRun)
	registerSkillAuthoringTools(server)

	// Gate external-action tools behind a configured provider. Registering 14
	// empty action tools inflates the MCP tool schema and pushes the total
	// registry past Claude Code's deferred-tools threshold, which causes the
	// model to emit a wasted ToolSearch before every call to a deferred tool.
	// When no provider is available the tools would just return errors anyway.
	if hasActionProvider() {
		registerActionTools(server)
	}

	// Lead-only tools: structural + coordination tools that specialists should
	// never invoke. Specialists still see them in the prompt's role-specific
	// guidance, but the MCP schema omits them, so the model cannot call them
	// and cannot waste a ToolSearch turn looking them up.
	if isLead {
		mcp.AddTool(server, officeWriteTool(
			"team_plan",
			"Create a batch of tasks in one shot with optional dependency ordering. Use this instead of multiple team_task calls when you know the full plan up front.",
		), handleTeamPlan)
		mcp.AddTool(server, officeWriteTool(
			"team_bridge",
			"CEO-only tool to bridge relevant context from one channel into another and leave a visible cross-channel trail.",
		), handleTeamBridge)
		mcp.AddTool(server, officeWriteTool(
			"team_channel",
			"Create or remove an office channel. When creating a channel, include a clear description of what work belongs there and the initial roster that should be in it. Only do this when the human explicitly wants channel structure.",
		), handleTeamChannel)
		mcp.AddTool(server, officeWriteTool(
			"team_channel_member",
			"Add, remove, disable, or enable an agent in a specific office channel.",
		), handleTeamChannelMember)
		mcp.AddTool(server, officeWriteTool(
			"team_member",
			"Create or remove an office-wide member. Only create new members when the human explicitly wants to expand the team.",
		), handleTeamMember)
	}
}

// hasActionProvider reports whether any external action provider is configured
// and usable. Used to gate registerActionTools so agents in offices without a
// connected provider do not see 14 action tools that would all return errors.
func hasActionProvider() bool {
	if externalActionProvider != nil {
		return true
	}
	_, err := team.ResolveActionProviderForCapability(action.CapabilityGuide)
	return err == nil
}
