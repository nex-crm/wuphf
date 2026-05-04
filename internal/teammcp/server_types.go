package teammcp

type brokerMessage struct {
	ID          string   `json:"id"`
	From        string   `json:"from"`
	Channel     string   `json:"channel,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Source      string   `json:"source,omitempty"`
	SourceLabel string   `json:"source_label,omitempty"`
	EventID     string   `json:"event_id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Content     string   `json:"content"`
	Tagged      []string `json:"tagged,omitempty"`
	ReplyTo     string   `json:"reply_to,omitempty"`
	Timestamp   string   `json:"timestamp"`
	Usage       *struct {
		InputTokens         int `json:"input_tokens,omitempty"`
		OutputTokens        int `json:"output_tokens,omitempty"`
		CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
		CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
		TotalTokens         int `json:"total_tokens,omitempty"`
	} `json:"usage,omitempty"`
}

type brokerMessagesResponse struct {
	Messages    []brokerMessage `json:"messages"`
	TaggedCount int             `json:"tagged_count"`
}

type brokerMembersResponse struct {
	Members []struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Role        string `json:"role"`
		Disabled    bool   `json:"disabled"`
		LastMessage string `json:"lastMessage"`
		LastTime    string `json:"lastTime"`
	} `json:"members"`
}

type brokerChannelsResponse struct {
	Channels []brokerChannelSummary `json:"channels"`
}

type brokerChannelSummary struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Members     []string `json:"members"`
	Disabled    []string `json:"disabled"`
}

type brokerOfficeMembersResponse struct {
	Members []struct {
		Slug           string   `json:"slug"`
		Name           string   `json:"name"`
		Role           string   `json:"role"`
		Expertise      []string `json:"expertise"`
		Personality    string   `json:"personality"`
		PermissionMode string   `json:"permission_mode"`
		BuiltIn        bool     `json:"built_in"`
	} `json:"members"`
}

type brokerInterviewAnswerResponse struct {
	Answered *struct {
		ChoiceID   string `json:"choice_id,omitempty"`
		ChoiceText string `json:"choice_text,omitempty"`
		CustomText string `json:"custom_text,omitempty"`
		AnsweredAt string `json:"answered_at,omitempty"`
	} `json:"answered"`
	Status string `json:"status,omitempty"`
}

type brokerRequestsResponse struct {
	Requests []struct {
		ID            string                 `json:"id"`
		Kind          string                 `json:"kind"`
		Status        string                 `json:"status"`
		From          string                 `json:"from"`
		Channel       string                 `json:"channel"`
		Title         string                 `json:"title"`
		Question      string                 `json:"question"`
		Context       string                 `json:"context"`
		Options       []HumanInterviewOption `json:"options"`
		RecommendedID string                 `json:"recommended_id"`
		Blocking      bool                   `json:"blocking"`
		Required      bool                   `json:"required"`
		Secret        bool                   `json:"secret"`
	} `json:"requests"`
	Pending *struct {
		ID            string                 `json:"id"`
		Kind          string                 `json:"kind"`
		From          string                 `json:"from"`
		Channel       string                 `json:"channel"`
		Title         string                 `json:"title"`
		Question      string                 `json:"question"`
		Context       string                 `json:"context"`
		Options       []HumanInterviewOption `json:"options"`
		RecommendedID string                 `json:"recommended_id"`
		Blocking      bool                   `json:"blocking"`
		Required      bool                   `json:"required"`
		Secret        bool                   `json:"secret"`
	} `json:"pending"`
}

type brokerTasksResponse struct {
	Tasks []brokerTaskSummary `json:"tasks"`
}

type brokerMemoryResponse struct {
	Namespace string             `json:"namespace,omitempty"`
	Entries   []brokerMemoryNote `json:"entries,omitempty"`
}

type brokerMemoryNote struct {
	Key       string `json:"key"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Author    string `json:"author,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type brokerTaskSummary struct {
	ID               string   `json:"id"`
	Channel          string   `json:"channel"`
	Title            string   `json:"title"`
	Details          string   `json:"details"`
	Owner            string   `json:"owner"`
	Status           string   `json:"status"`
	CreatedBy        string   `json:"created_by"`
	ThreadID         string   `json:"thread_id"`
	TaskType         string   `json:"task_type"`
	PipelineStage    string   `json:"pipeline_stage"`
	ExecutionMode    string   `json:"execution_mode"`
	ReviewState      string   `json:"review_state"`
	SourceSignalID   string   `json:"source_signal_id"`
	SourceDecisionID string   `json:"source_decision_id"`
	WorktreePath     string   `json:"worktree_path"`
	WorktreeBranch   string   `json:"worktree_branch"`
	DependsOn        []string `json:"depends_on,omitempty"`
	Blocked          bool     `json:"blocked,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

type conversationContext struct {
	Channel   string
	ReplyToID string
	Source    string
}

type TeamBroadcastArgs struct {
	Channel   string   `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	Content   string   `json:"content" jsonschema:"Message to post to the shared team channel"`
	MySlug    string   `json:"my_slug,omitempty" jsonschema:"Agent slug sending the message. Defaults to WUPHF_AGENT_SLUG."`
	Tagged    []string `json:"tagged,omitempty" jsonschema:"Optional list of tagged agent slugs who should respond"`
	ReplyToID string   `json:"reply_to_id,omitempty" jsonschema:"Reply in-thread to a specific message ID when continuing a narrow discussion"`
	NewTopic  bool     `json:"new_topic,omitempty" jsonschema:"Set true only when this genuinely needs to start a new top-level thread"`
}

type TeamReactArgs struct {
	MessageID string `json:"message_id" jsonschema:"The message ID to react to"`
	Emoji     string `json:"emoji" jsonschema:"Emoji reaction (e.g. 👍, 💯, 🔥, 👀, ✅)"`
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamPollArgs struct {
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug so tagged_count can be computed. Defaults to WUPHF_AGENT_SLUG."`
	SinceID string `json:"since_id,omitempty" jsonschema:"Only return messages after this message ID"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum messages to return (default 10, max 100)"`
	Scope   string `json:"scope,omitempty" jsonschema:"Transcript scope: all, agent, inbox, or outbox. Defaults to agent-scoped for non-CEO office agents."`
}

type TeamStatusArgs struct {
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	Status  string `json:"status" jsonschema:"Short status like 'reviewing onboarding flow' or 'implementing search index'"`
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Agent slug sending the status. Defaults to WUPHF_AGENT_SLUG."`
}

type HumanInterviewOption struct {
	ID           string `json:"id" jsonschema:"Stable short ID like 'sales' or 'smbs'"`
	Label        string `json:"label" jsonschema:"User-facing option label"`
	Description  string `json:"description,omitempty" jsonschema:"One-sentence explanation of tradeoff or impact"`
	RequiresText bool   `json:"requires_text,omitempty" jsonschema:"Whether the human must add typed guidance when choosing this option"`
	TextHint     string `json:"text_hint,omitempty" jsonschema:"Hint shown when typed guidance is required or recommended for this option"`
}

type HumanInterviewArgs struct {
	Channel             string                 `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	Question            string                 `json:"question" jsonschema:"The specific decision or clarification needed from the human"`
	Context             string                 `json:"context,omitempty" jsonschema:"Short context explaining why the team is asking now"`
	MySlug              string                 `json:"my_slug,omitempty" jsonschema:"Agent slug asking the question. Defaults to WUPHF_AGENT_SLUG."`
	Options             []HumanInterviewOption `json:"options,omitempty" jsonschema:"Suggested answer options to show the human"`
	RecommendedOptionID string                 `json:"recommended_option_id,omitempty" jsonschema:"Which option you recommend, if any"`
}

type HumanMessageArgs struct {
	Kind      string `json:"kind,omitempty" jsonschema:"One of: report, decision, action. Defaults to report."`
	Channel   string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel, or the active direct session in 1:1 mode."`
	Title     string `json:"title,omitempty" jsonschema:"Short human-facing headline like 'Frontend ready for review' or 'Need your call on pricing'"`
	Content   string `json:"content" jsonschema:"What you want to tell the human directly: completion update, recommendation, decision framing, or next action."`
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Agent slug speaking to the human. Defaults to WUPHF_AGENT_SLUG."`
	ReplyToID string `json:"reply_to_id,omitempty" jsonschema:"Optional message ID this human-facing note belongs to."`
}

type TeamRequestsArgs struct {
	Channel         string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	IncludeResolved bool   `json:"include_resolved,omitempty" jsonschema:"Include already answered or canceled requests."`
	MySlug          string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamRequestArgs struct {
	Kind                string                 `json:"kind,omitempty" jsonschema:"One of: choice, confirm, freeform, approval, secret. Defaults to choice."`
	Channel             string                 `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	Title               string                 `json:"title,omitempty" jsonschema:"Short request title"`
	Question            string                 `json:"question" jsonschema:"The actual question or approval the human needs to respond to"`
	Context             string                 `json:"context,omitempty" jsonschema:"Short context for why the request exists"`
	MySlug              string                 `json:"my_slug,omitempty" jsonschema:"Agent slug asking the question. Defaults to WUPHF_AGENT_SLUG."`
	Options             []HumanInterviewOption `json:"options,omitempty" jsonschema:"Suggested answer options for choice-style requests"`
	RecommendedOptionID string                 `json:"recommended_option_id,omitempty" jsonschema:"Which option you recommend, if any"`
	Blocking            bool                   `json:"blocking,omitempty" jsonschema:"Whether this request should pause channel work until answered"`
	Required            bool                   `json:"required,omitempty" jsonschema:"Whether an answer is truly required before continuing"`
	Secret              bool                   `json:"secret,omitempty" jsonschema:"Whether the answer should be treated as private in channel history"`
	ReplyToID           string                 `json:"reply_to_id,omitempty" jsonschema:"Optional message ID this request belongs to"`
}

type TeamTasksArgs struct {
	Channel     string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
	IncludeDone bool   `json:"include_done,omitempty" jsonschema:"Include completed tasks as well"`
}

type TeamRuntimeStateArgs struct {
	Channel      string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	MySlug       string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
	MessageLimit int    `json:"message_limit,omitempty" jsonschema:"How many recent messages to include when building the recovery summary (default 12, max 40)."`
}

type TeamTaskArgs struct {
	Action        string   `json:"action" jsonschema:"One of: create, claim, assign, complete, block, resume, release"`
	Channel       string   `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	ID            string   `json:"id,omitempty" jsonschema:"Task ID for non-create actions"`
	Title         string   `json:"title,omitempty" jsonschema:"Task title when creating a task"`
	Details       string   `json:"details,omitempty" jsonschema:"Optional detail or update"`
	Owner         string   `json:"owner,omitempty" jsonschema:"Owner slug for claim or assign"`
	ThreadID      string   `json:"thread_id,omitempty" jsonschema:"Related thread or message id"`
	TaskType      string   `json:"task_type,omitempty" jsonschema:"Optional task type such as research, feature, launch, follow_up, bugfix, or incident"`
	ExecutionMode string   `json:"execution_mode,omitempty" jsonschema:"Optional execution mode such as office or local_worktree"`
	DependsOn     []string `json:"depends_on,omitempty" jsonschema:"Task IDs this task must wait for before starting (create action only)"`
	MySlug        string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamChannelsArgs struct{}

type TeamMembersArgs struct {
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamChannelArgs struct {
	Action      string   `json:"action" jsonschema:"One of: create, remove"`
	Channel     string   `json:"channel" jsonschema:"Channel slug"`
	Name        string   `json:"name,omitempty" jsonschema:"Optional channel display name on create"`
	Description string   `json:"description,omitempty" jsonschema:"One-sentence explanation of what the channel is for. Required in practice when creating channels."`
	Members     []string `json:"members,omitempty" jsonschema:"Optional initial member slugs to add when creating the channel. CEO is always included."`
	MySlug      string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamDMOpenArgs struct {
	Members []string `json:"members" jsonschema:"Array of member slugs. Must include 'human'. For 1:1 DMs: ['human', 'agent-slug']. Agent-to-agent DMs are not allowed."`
	Type    string   `json:"type,omitempty" jsonschema:"Channel type: 'direct' (default, 1:1) or 'group' (multi-member). Defaults to direct."`
}

type TeamChannelMemberArgs struct {
	Action     string `json:"action" jsonschema:"One of: add, remove, disable, enable"`
	Channel    string `json:"channel" jsonschema:"Channel slug"`
	MemberSlug string `json:"member_slug" jsonschema:"Agent slug to modify"`
	MySlug     string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamBridgeArgs struct {
	SourceChannel string   `json:"source_channel" jsonschema:"Channel slug the context is coming from"`
	TargetChannel string   `json:"target_channel" jsonschema:"Channel slug the context should be carried into"`
	Summary       string   `json:"summary" jsonschema:"Concise bridged context to carry across channels"`
	Tagged        []string `json:"tagged,omitempty" jsonschema:"Optional agents to wake in the target channel after the bridge lands"`
	MySlug        string   `json:"my_slug,omitempty" jsonschema:"Agent slug performing the bridge. Defaults to WUPHF_AGENT_SLUG."`
	ReplyToID     string   `json:"reply_to_id,omitempty" jsonschema:"Optional target-channel message ID this bridge belongs to"`
}

type TeamOfficeMembersArgs struct{}

type TeamMemberArgs struct {
	Action         string   `json:"action" jsonschema:"One of: create, remove"`
	Slug           string   `json:"slug" jsonschema:"Stable agent slug like growthops or research-lead"`
	Name           string   `json:"name,omitempty" jsonschema:"Display name for the office member"`
	Role           string   `json:"role,omitempty" jsonschema:"Role/job title"`
	Expertise      []string `json:"expertise,omitempty" jsonschema:"Optional expertise list"`
	Personality    string   `json:"personality,omitempty" jsonschema:"Optional short personality description"`
	PermissionMode string   `json:"permission_mode,omitempty" jsonschema:"Optional Claude permission mode"`
	// Per-agent provider selection. Empty Provider means the agent inherits the
	// install-wide default runtime. Set Provider to pick a specific runtime and
	// (optionally) model for this agent: one team can mix Claude, Codex, and
	// OpenClaw agents, each on its own provider.
	Provider           string `json:"provider,omitempty" jsonschema:"LLM runtime for this agent. One of: claude-code, codex, opencode, openclaw. Empty = install default."`
	Model              string `json:"model,omitempty" jsonschema:"Model name passed to the runtime (e.g. claude-sonnet-4.6, gpt-5.4, openai-codex/gpt-5.4). Free-form; runtime validates."`
	OpenclawSessionKey string `json:"openclaw_session_key,omitempty" jsonschema:"Optional: attach to an existing OpenClaw session key (e.g. after WUPHF reinstall). Leave empty to auto-create a new session."`
	OpenclawAgentID    string `json:"openclaw_agent_id,omitempty" jsonschema:"Optional: OpenClaw agent config name (defaults to 'main')."`
	MySlug             string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamPlanArgs struct {
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	Tasks   []struct {
		Title         string   `json:"title" jsonschema:"Task title"`
		Assignee      string   `json:"assignee" jsonschema:"Agent slug to own this task"`
		Details       string   `json:"details,omitempty" jsonschema:"Optional task details"`
		TaskType      string   `json:"task_type,omitempty" jsonschema:"Optional task type such as research, feature, launch, follow_up, bugfix, or incident"`
		ExecutionMode string   `json:"execution_mode,omitempty" jsonschema:"Optional execution mode such as office or local_worktree"`
		DependsOn     []string `json:"depends_on,omitempty" jsonschema:"Titles or IDs of tasks this depends on"`
	} `json:"tasks" jsonschema:"List of tasks to create in dependency order"`
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamMemoryQueryArgs struct {
	Query  string `json:"query" jsonschema:"What you want to look up in memory"`
	Scope  string `json:"scope,omitempty" jsonschema:"One of: auto, private, shared. Defaults to auto."`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum hits to return per scope (default 5)"`
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamMemoryWriteArgs struct {
	Key        string `json:"key,omitempty" jsonschema:"Optional stable key. Omit to auto-generate one from the title or content."`
	Title      string `json:"title,omitempty" jsonschema:"Optional short title for the note"`
	Content    string `json:"content" jsonschema:"Note content to store"`
	Visibility string `json:"visibility,omitempty" jsonschema:"One of: private, shared. Defaults to private."`
	MySlug     string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamMemoryPromoteArgs struct {
	Key    string `json:"key" jsonschema:"Private note key to promote into shared durable memory"`
	Title  string `json:"title,omitempty" jsonschema:"Optional override title for the promoted shared note"`
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

// TeamWikiWriteArgs is the contract for the team_wiki_write MCP tool.
type TeamWikiWriteArgs struct {
	MySlug      string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG env."`
	ArticlePath string `json:"article_path" jsonschema:"Path within wiki root, e.g. team/people/nazz.md"`
	Mode        string `json:"mode" jsonschema:"One of: create | replace | append_section"`
	Content     string `json:"content" jsonschema:"Full article content (create/replace) or new section text (append_section)"`
	CommitMsg   string `json:"commit_message" jsonschema:"Why this change — becomes the git commit message"`
}

// TeamWikiReadArgs is the contract for team_wiki_read.
type TeamWikiReadArgs struct {
	ArticlePath string `json:"article_path" jsonschema:"Path within wiki root"`
}

// TeamWikiSearchArgs is the contract for team_wiki_search.
type TeamWikiSearchArgs struct {
	Pattern string `json:"pattern" jsonschema:"Literal substring to search (not regex)"`
}

// TeamWikiListArgs is intentionally empty — team_wiki_list takes no args.
type TeamWikiListArgs struct{}

// TeamWikiLookupArgs is the contract for wuphf_wiki_lookup.
type TeamWikiLookupArgs struct {
	Query string `json:"query" jsonschema:"Natural-language question to answer from the team wiki"`
	TopK  int    `json:"top_k,omitempty" jsonschema:"Max sources to retrieve (default 20)"`
}

type TeamTaskAckArgs struct {
	ID      string `json:"id" jsonschema:"Task ID to acknowledge"`
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamTaskStatusArgs struct {
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}
