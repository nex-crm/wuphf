package teammcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/team"
)

const defaultBrokerTokenFile = brokeraddr.DefaultTokenFile

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
	Action        string   `json:"action" jsonschema:"One of: create, claim, assign, complete, block, release"`
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
	Provider           string `json:"provider,omitempty" jsonschema:"LLM runtime for this agent. One of: claude-code, codex, openclaw. Empty = install default."`
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

type TeamTaskAckArgs struct {
	ID      string `json:"id" jsonschema:"Task ID to acknowledge"`
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamTaskStatusArgs struct {
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
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

// agentToolEventMiddleware wraps every incoming MCP method so tools/call
// invocations are teed to the broker's per-agent stream. This gives the web
// UI an audit trail of what tool each agent called, with arguments and
// either the result summary or an error — visibility the raw pane capture
// can't provide for agents that do their work through MCP calls.
func agentToolEventMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}
		toolName, argsJSON := extractToolCallRequest(req)
		if toolName != "" {
			postAgentToolEvent(ctx, resolveSlugOptional(""), "call", toolName, argsJSON, "", "")
		}
		result, err := next(ctx, method, req)
		if toolName != "" {
			phase := "result"
			errStr := ""
			if err != nil {
				phase = "error"
				errStr = err.Error()
			}
			postAgentToolEvent(ctx, resolveSlugOptional(""), phase, toolName, "", summarizeToolResult(result), errStr)
		}
		return result, err
	}
}

func extractToolCallRequest(req mcp.Request) (tool, args string) {
	if req == nil {
		return "", ""
	}
	sr, ok := req.(*mcp.ServerRequest[*mcp.CallToolParamsRaw])
	if !ok || sr == nil || sr.Params == nil {
		return "", ""
	}
	tool = sr.Params.Name
	if len(sr.Params.Arguments) > 0 {
		args = string(sr.Params.Arguments)
	}
	return tool, args
}

func summarizeToolResult(res mcp.Result) string {
	r, ok := res.(*mcp.CallToolResult)
	if !ok || r == nil {
		return ""
	}
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok && tc != nil {
			return tc.Text
		}
	}
	return ""
}

func postAgentToolEvent(ctx context.Context, slug, phase, tool, args, result, errStr string) {
	slug = strings.TrimSpace(slug)
	if slug == "" || tool == "" {
		return
	}
	body := map[string]string{
		"slug":   slug,
		"phase":  phase,
		"tool":   tool,
		"args":   args,
		"result": result,
		"error":  errStr,
	}
	// Fire-and-forget; dropping a log line must never fail a tool call.
	go func() {
		// Ignore errors — the broker might be restarting or unreachable,
		// and an audit-log failure is not worth surfacing to the agent.
		_ = brokerPostJSON(context.Background(), "/agent-tool-event", body, nil)
	}()
	_ = ctx
}

// registerSharedMemoryTools registers the active shared-memory / wiki tool
// set on the server. Markdown-backend installs expose team_wiki_* tools;
// nex/gbrain installs expose the legacy team_memory_* tools; `none` skips
// them entirely. Both tool sets NEVER coexist — agents see exactly one.
func registerSharedMemoryTools(server *mcp.Server) {
	switch strings.TrimSpace(os.Getenv("WUPHF_MEMORY_BACKEND")) {
	case "markdown":
		mcp.AddTool(server, officeWriteTool(
			"team_wiki_write",
			"Write a markdown article to the team wiki git repo. The content you pass becomes the article bytes; this tool does not rewrite for you. Picks author identity from my_slug so git log shows which agent wrote each article. Images are supported via standard markdown: embed a remote URL with `![alt text](https://example.com/diagram.png)` and the wiki renderer will show it inline. Use images you found on the web while researching the article; do not upload bytes — only reference URLs.",
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
		// Notebook tools ride on the same markdown backend. Registered here
		// so they share the WUPHF_MEMORY_BACKEND gate with team_wiki_*.
		registerNotebookTools(server)
		// Entity brief tools (v1.2) — fact log + broker-level synthesis.
		// Same backend gate: entity briefs live in the wiki subtree.
		registerEntityTools(server)
		// Playbook compilation tools (v1.3) — compile team/playbooks/*.md
		// into invokable skills + record execution outcomes. Same markdown
		// substrate, so the backend gate is unchanged.
		registerPlaybookTools(server)
	case "none":
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
			"Ask the human a blocking interview question when you truly cannot proceed responsibly without a decision.",
		), handleHumanInterview)

		mcp.AddTool(server, officeWriteTool(
			"human_message",
			"Send a direct human-facing note into the chat when you need to present completion, recommend a decision, or tell the human what they should do next.",
		), handleHumanMessage)

		registerSharedMemoryTools(server)

		mcp.AddTool(server, readOnlyTool(
			"team_runtime_state",
			"Return the canonical runtime snapshot for this direct session, including tasks, pending human requests, recovery summary, and runtime capabilities.",
		), handleTeamRuntimeState)

		registerActionTools(server)
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
			"Ask the human a blocking decision question.",
		), handleHumanInterview)
		registerSharedMemoryTools(server)
		mcp.AddTool(server, officeWriteTool(
			"team_skill_run",
			"Invoke a named team skill. When the human's request matches an available skill, call this BEFORE replying — do not freelance. Bumps the skill's usage, logs a skill_invocation to the channel, and returns the skill's canonical step-by-step content for you to follow.",
		), handleTeamSkillRun)
		registerActionTools(server)
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

	mcp.AddTool(server, officeWriteTool(
		"team_channel",
		"Create or remove an office channel. When creating a channel, include a clear description of what work belongs there and the initial roster that should be in it. Only do this when the human explicitly wants channel structure.",
	), handleTeamChannel)

	mcp.AddTool(server, officeWriteTool(
		"team_channel_member",
		"Add, remove, disable, or enable an agent in a specific office channel.",
	), handleTeamChannelMember)

	mcp.AddTool(server, officeWriteTool(
		"team_bridge",
		"CEO-only tool to bridge relevant context from one channel into another and leave a visible cross-channel trail.",
	), handleTeamBridge)

	mcp.AddTool(server, officeWriteTool(
		"team_member",
		"Create or remove an office-wide member. Only create new members when the human explicitly wants to expand the team.",
	), handleTeamMember)

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
		"Create, claim, assign, complete, block, or release a shared task in the office task list.",
	), handleTeamTask)

	mcp.AddTool(server, officeWriteTool(
		"team_plan",
		"Create a batch of tasks in one shot with optional dependency ordering. Use this instead of multiple team_task calls when you know the full plan up front.",
	), handleTeamPlan)

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
		"Ask the human a blocking interview question when the team cannot proceed responsibly without a decision.",
	), handleHumanInterview)

	mcp.AddTool(server, officeWriteTool(
		"human_message",
		"Send a direct note to the human.",
	), handleHumanMessage)
	mcp.AddTool(server, officeWriteTool(
		"human_interview",
		"Ask the human a blocking decision question.",
	), handleHumanInterview)
	mcp.AddTool(server, readOnlyTool(
		"team_tasks",
		"List shared tasks and ownership.",
	), handleTeamTasks)
	mcp.AddTool(server, officeWriteTool(
		"team_react",
		"React to a message with an emoji.",
	), handleTeamReact)
	mcp.AddTool(server, officeWriteTool(
		"team_status",
		"Share a short status update.",
	), handleTeamStatus)
	mcp.AddTool(server, officeWriteTool(
		"team_skill_run",
		"Invoke a named team skill. When the request matches an available skill (see the skill list in your prompt), call this BEFORE doing the work — do not freelance. Bumps the skill's usage, logs a skill_invocation in the channel so the office sees you followed the playbook, and returns the skill's canonical step-by-step content for you to execute.",
	), handleTeamSkillRun)
	registerActionTools(server)

	// Lead-only tools: CEO gets coordination, delegation, and structural tools
	if isLead {
		mcp.AddTool(server, officeWriteTool(
			"team_task",
			"Create, assign, complete, or block a task.",
		), handleTeamTask)
		mcp.AddTool(server, officeWriteTool(
			"team_plan",
			"Create a batch of tasks with dependency ordering.",
		), handleTeamPlan)
		mcp.AddTool(server, officeWriteTool(
			"team_bridge",
			"Bridge context from one channel to another.",
		), handleTeamBridge)
		mcp.AddTool(server, readOnlyTool(
			"team_members",
			"List channel participants and activity.",
		), handleTeamMembers)
		mcp.AddTool(server, readOnlyTool(
			"team_requests",
			"List pending human requests.",
		), handleTeamRequests)
		mcp.AddTool(server, officeWriteTool(
			"team_request",
			"Create a structured request for the human.",
		), handleTeamRequest)
		mcp.AddTool(server, readOnlyTool(
			"team_runtime_state",
			"Office runtime snapshot: tasks, requests, recovery.",
		), handleTeamRuntimeState)
		mcp.AddTool(server, readOnlyTool(
			"team_office_members",
			"List the full office roster.",
		), handleTeamOfficeMembers)
		mcp.AddTool(server, readOnlyTool(
			"team_channels",
			"List office channels and memberships.",
		), handleTeamChannels)
		mcp.AddTool(server, officeWriteTool(
			"team_channel",
			"Create or remove a channel.",
		), handleTeamChannel)
		mcp.AddTool(server, officeWriteTool(
			"team_channel_member",
			"Add or remove an agent from a channel.",
		), handleTeamChannelMember)
		mcp.AddTool(server, officeWriteTool(
			"team_member",
			"Create or remove an office member.",
		), handleTeamMember)
	}
}

func handleTeamBroadcast(ctx context.Context, _ *mcp.CallToolRequest, args TeamBroadcastArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	location := resolveConversationContext(ctx, slug, args.Channel, args.ReplyToID)
	channel := location.Channel
	replyTo := strings.TrimSpace(args.ReplyToID)
	if replyTo == "" && !args.NewTopic {
		replyTo = location.ReplyToID
	}

	if !isOneOnOneMode() {
		if messages, tasks, err := fetchBroadcastContext(ctx, channel, slug); err == nil {
			if reason := suppressBroadcastReason(slug, args.Content, replyTo, messages, tasks); reason != "" {
				return textResult(fmt.Sprintf("Held reply for @%s: %s. Poll again if the thread changes or if the CEO tags you in.", slug, reason)), nil, nil
			}
		}
	}

	var result struct {
		ID string `json:"id"`
	}
	err = brokerPostJSON(ctx, "/messages", map[string]any{
		"channel":  channel,
		"from":     slug,
		"content":  args.Content,
		"tagged":   args.Tagged,
		"reply_to": replyTo,
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}

	text := fmt.Sprintf("Posted to #%s as @%s", channel, slug)
	if isOneOnOneMode() {
		text = fmt.Sprintf("Sent direct reply to the human as @%s", slug)
	}
	if result.ID != "" {
		text += " (" + result.ID + ")"
	}
	if replyTo != "" {
		text += " in reply to " + replyTo
	}
	text += "."

	// Warn when the message text contains @-mentions but none were passed in
	// the `tagged` parameter. Text @-mentions are display-only — they do NOT
	// wake agents. This is the most common CEO mistake: writing "@engineering
	// please do X" without tagging engineering in the tool call.
	if len(args.Tagged) == 0 && !isOneOnOneMode() {
		if untaggedMentions := detectUntaggedMentions(args.Content, args.Tagged); len(untaggedMentions) > 0 {
			text += fmt.Sprintf(
				" WARNING: message text mentions %s but `tagged` is empty — those agents will NOT be woken. Re-send with tagged: %s to notify them.",
				strings.Join(untaggedMentions, ", "),
				strings.Join(untaggedMentions, ", "),
			)
		}
	}

	return textResult(text), nil, nil
}

// detectUntaggedMentions returns @-slugs found in content that are not in the
// tagged list. Only slug-like words (alphanumeric + hyphen, 2-20 chars) are
// flagged to avoid false positives from conversational @-references.
func detectUntaggedMentions(content string, tagged []string) []string {
	taggedSet := make(map[string]struct{}, len(tagged))
	for _, t := range tagged {
		taggedSet[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []string
	parts := strings.Fields(content)
	for _, p := range parts {
		if !strings.HasPrefix(p, "@") {
			continue
		}
		// Strip trailing punctuation
		raw := strings.TrimLeft(p, "@")
		raw = strings.TrimRight(raw, ".,;:!?)")
		raw = strings.ToLower(raw)
		if len(raw) < 2 || len(raw) > 20 {
			continue
		}
		// Only flag slug-like strings: alphanumeric + hyphens
		valid := true
		for _, r := range raw {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		// Skip common non-agent references
		switch raw {
		case "you", "human", "nex", "system", "everyone", "all", "team", "channel":
			continue
		}
		if _, inTagged := taggedSet[raw]; inTagged {
			continue
		}
		if _, already := seen[raw]; already {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, "@"+raw)
	}
	return out
}

func handleTeamReact(ctx context.Context, _ *mcp.CallToolRequest, args TeamReactArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if args.MessageID == "" || args.Emoji == "" {
		return toolError(fmt.Errorf("message_id and emoji are required")), nil, nil
	}
	var result struct {
		OK        bool `json:"ok"`
		Duplicate bool `json:"duplicate"`
	}
	if err := brokerPostJSON(ctx, "/reactions", map[string]any{
		"message_id": args.MessageID,
		"emoji":      args.Emoji,
		"from":       slug,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	if result.Duplicate {
		return textResult(fmt.Sprintf("Already reacted %s to %s.", args.Emoji, args.MessageID)), nil, nil
	}
	return textResult(fmt.Sprintf("Reacted %s to %s as @%s.", args.Emoji, args.MessageID, slug)), nil, nil
}

func fetchBroadcastContext(ctx context.Context, channel, mySlug string) ([]brokerMessage, []brokerTaskSummary, error) {
	msgValues := url.Values{}
	msgValues.Set("channel", channel)
	msgValues.Set("limit", "40")
	if mySlug != "" {
		msgValues.Set("my_slug", mySlug)
	}
	var messages brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?"+msgValues.Encode(), &messages); err != nil {
		return nil, nil, err
	}
	// Fetch tasks across ALL channels so ownsRelevantTask can find tasks that live
	// in dedicated channels (e.g. "engineering") even when the specialist broadcasts
	// into "general". Without all_channels=true, a specialist who completes work
	// cross-channel would be incorrectly suppressed.
	var tasks brokerTasksResponse
	if err := brokerGetJSON(ctx, "/tasks?all_channels=true", &tasks); err != nil {
		return messages.Messages, nil, err
	}
	return messages.Messages, tasks.Tasks, nil
}

func suppressBroadcastReason(slug, content, replyTo string, messages []brokerMessage, tasks []brokerTaskSummary) string {
	if strings.TrimSpace(slug) == "" || slug == "ceo" {
		return ""
	}
	threadRoot := threadRootForReply(replyTo, messages)
	myDomain := inferOfficeAgentDomain(slug)
	latest := latestRelevantMessage(messages, replyTo)
	latestDomain := inferOfficeTextDomain(content)
	if latestDomain == "general" && latest != nil {
		latestDomain = inferOfficeTextDomain(latest.Title + " " + latest.Content)
	}
	// An agent is explicitly needed if it was tagged in the latest relevant
	// message OR in any message in the thread (e.g. the root human message that
	// originally requested work from multiple agents in parallel).
	explicitNeed := latest != nil && containsSlug(latest.Tagged, slug)
	if !explicitNeed && replyTo != "" {
		for _, msg := range messages {
			if (msg.ID == replyTo || strings.TrimSpace(msg.ReplyTo) == replyTo) && containsSlug(msg.Tagged, slug) {
				explicitNeed = true
				break
			}
		}
	}
	ownsTask := ownsRelevantTask(slug, replyTo, threadRoot, latestDomain, tasks)

	if explicitNeed || ownsTask {
		return ""
	}
	// Safety net: only block hard domain mismatches
	if latestDomain != "" && latestDomain != "general" && myDomain != latestDomain {
		return "this is outside your domain"
	}
	return ""
}

func latestRelevantMessage(messages []brokerMessage, replyTo string) *brokerMessage {
	replyTo = strings.TrimSpace(replyTo)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.HasPrefix(strings.TrimSpace(msg.Content), "[STATUS]") {
			continue
		}
		if replyTo != "" {
			if msg.ID != replyTo && strings.TrimSpace(msg.ReplyTo) != replyTo {
				continue
			}
		} else if strings.TrimSpace(msg.ReplyTo) != "" {
			continue
		}
		return &messages[i]
	}
	return nil
}

func threadRootForReply(replyTo string, messages []brokerMessage) string {
	replyTo = strings.TrimSpace(replyTo)
	if replyTo == "" {
		return ""
	}
	for _, msg := range messages {
		if strings.TrimSpace(msg.ID) != replyTo {
			continue
		}
		if root := strings.TrimSpace(msg.ReplyTo); root != "" {
			return root
		}
		return replyTo
	}
	return replyTo
}

func ownsRelevantTask(slug, replyTo, threadRoot, domain string, tasks []brokerTaskSummary) bool {
	slug = strings.TrimSpace(slug)
	replyTo = strings.TrimSpace(replyTo)
	threadRoot = strings.TrimSpace(threadRoot)
	now := time.Now()
	for _, task := range tasks {
		if strings.TrimSpace(task.Owner) != slug {
			continue
		}
		if !taskAllowsFollowUpBroadcast(task, replyTo, domain, now) {
			continue
		}
		if replyTo != "" {
			if strings.TrimSpace(task.ThreadID) == replyTo || (threadRoot != "" && strings.TrimSpace(task.ThreadID) == threadRoot) {
				return true
			}
			continue
		}
		taskDomain := inferOfficeTextDomain(task.Title + " " + task.Details)
		if taskDomain == domain || taskDomain == "general" || domain == "" {
			return true
		}
	}
	return false
}

const completedTaskBroadcastGrace = 15 * time.Minute

func taskAllowsFollowUpBroadcast(task brokerTaskSummary, replyTo, domain string, now time.Time) bool {
	status := strings.TrimSpace(task.Status)
	if !strings.EqualFold(status, "done") {
		return true
	}
	if replyTo != "" && strings.TrimSpace(task.ThreadID) == replyTo {
		return true
	}
	updatedAt := strings.TrimSpace(task.UpdatedAt)
	if updatedAt == "" {
		return false
	}
	updated, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return false
	}
	if now.Sub(updated) > completedTaskBroadcastGrace {
		return false
	}
	taskDomain := inferOfficeTextDomain(task.Title + " " + task.Details)
	return taskDomain == domain || taskDomain == "general" || domain == ""
}

// inferOfficeAgentDomain and inferOfficeTextDomain are canonical wrappers around
// team.InferAgentDomain / team.InferTextDomain. All domain classification lives in
// team/domains.go — update keywords there and both packages stay in sync.

func inferOfficeAgentDomain(slug string) string { return team.InferAgentDomain(slug) }
func inferOfficeTextDomain(text string) string  { return team.InferTextDomain(text) }

func containsSlug(items []string, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	for _, item := range items {
		if strings.TrimSpace(strings.ToLower(item)) == want {
			return true
		}
	}
	return false
}

func handleTeamPoll(ctx context.Context, _ *mcp.CallToolRequest, args TeamPollArgs) (*mcp.CallToolResult, any, error) {
	channel := resolveConversationChannel(ctx, resolveSlugOptional(args.MySlug), args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	scope, err := normalizePollScope(args.Scope)
	if err != nil {
		return toolError(err), nil, nil
	}
	if slug := strings.TrimSpace(resolveSlugOptional(args.MySlug)); slug != "" {
		values.Set("my_slug", slug)
		applyAgentMessageScope(values, slug, scope)
	} else if scope != "" {
		values.Set("scope", scope)
	}
	if since := strings.TrimSpace(args.SinceID); since != "" {
		values.Set("since_id", since)
	}
	if args.Limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", args.Limit))
	}

	var result brokerMessagesResponse
	path := "/messages"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}

	summary := formatMessages(result.Messages, resolveSlugOptional(args.MySlug))
	if isOneOnOneMode() {
		if strings.TrimSpace(summary) == "" {
			return textResult("The 1:1 is quiet right now."), nil, nil
		}
		focus := latestHumanRequestSummary(result.Messages)
		if focus != "" {
			return textResult("Direct conversation\n\nLatest human request to answer now:\n" + focus + "\n\nOlder messages are background unless the latest request depends on them.\n\nRecent messages:\n" + summary), nil, nil
		}
		return textResult("Direct conversation\n\n" + summary), nil, nil
	}
	if scope == "inbox" || scope == "outbox" {
		scopeTitle := strings.ToUpper(scope[:1]) + scope[1:]
		if slug := strings.TrimSpace(resolveSlugOptional(args.MySlug)); slug != "" {
			return textResult(fmt.Sprintf("%s for @%s in #%s\n\n%s", scopeTitle, slug, channel, summary)), nil, nil
		}
		return textResult(fmt.Sprintf("%s in #%s\n\n%s", scopeTitle, channel, summary)), nil, nil
	}
	taskSummary := formatTaskSummary(ctx, resolveSlugOptional(args.MySlug), channel)
	requestSummary := formatRequestSummary(ctx, channel)
	return textResult(fmt.Sprintf("Channel #%s\n\n%s\n\nTagged messages for you: %d\n\n%s\n\n%s", channel, summary, result.TaggedCount, taskSummary, requestSummary)), nil, nil
}

func handleTeamInbox(ctx context.Context, req *mcp.CallToolRequest, args TeamPollArgs) (*mcp.CallToolResult, any, error) {
	args.Scope = "inbox"
	return handleTeamPoll(ctx, req, args)
}

func handleTeamOutbox(ctx context.Context, req *mcp.CallToolRequest, args TeamPollArgs) (*mcp.CallToolResult, any, error) {
	args.Scope = "outbox"
	return handleTeamPoll(ctx, req, args)
}

func handleTeamStatus(ctx context.Context, _ *mcp.CallToolRequest, args TeamStatusArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": channel,
		"from":    slug,
		"content": "[STATUS] " + args.Status,
		"tagged":  []string{},
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Updated #%s status for @%s: %s", channel, slug, args.Status)), nil, nil
}

func handleTeamMembers(ctx context.Context, _ *mcp.CallToolRequest, args TeamMembersArgs) (*mcp.CallToolResult, any, error) {
	viewer := strings.TrimSpace(resolveSlugOptional(args.MySlug))
	channel := resolveConversationChannel(ctx, viewer, args.Channel)
	var result brokerMembersResponse
	values := url.Values{}
	values.Set("channel", channel)
	if viewer != "" {
		values.Set("viewer_slug", viewer)
	}
	if err := brokerGetJSON(ctx, "/members?"+values.Encode(), &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Members) == 0 {
		return textResult("No active team members yet."), nil, nil
	}
	lines := make([]string, 0, len(result.Members))
	for _, member := range result.Members {
		line := "- @" + member.Slug
		if member.Name != "" {
			line += " (" + member.Name + ")"
		}
		if member.Role != "" {
			line += " · " + member.Role
		}
		if member.Disabled {
			line += " · disabled"
		}
		if member.LastTime != "" {
			line += " at " + member.LastTime
		}
		if member.LastMessage != "" {
			line += " — " + member.LastMessage
		}
		lines = append(lines, line)
	}
	return textResult("Active team members in #" + channel + ":\n" + strings.Join(lines, "\n")), nil, nil
}

func handleTeamOfficeMembers(ctx context.Context, _ *mcp.CallToolRequest, _ TeamOfficeMembersArgs) (*mcp.CallToolResult, any, error) {
	var result brokerOfficeMembersResponse
	if err := brokerGetJSON(ctx, "/office-members", &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Members) == 0 {
		return textResult("No office members."), nil, nil
	}
	lines := make([]string, 0, len(result.Members))
	for _, member := range result.Members {
		line := fmt.Sprintf("- @%s (%s)", member.Slug, member.Name)
		if member.Role != "" {
			line += " · " + member.Role
		}
		if len(member.Expertise) > 0 {
			line += " · " + strings.Join(member.Expertise, ", ")
		}
		if member.BuiltIn {
			line += " · built-in"
		}
		lines = append(lines, line)
	}
	return textResult("Office members:\n" + strings.Join(lines, "\n")), nil, nil
}

func handleTeamTasks(ctx context.Context, _ *mcp.CallToolRequest, args TeamTasksArgs) (*mcp.CallToolResult, any, error) {
	channel, tasks, err := fetchTeamTasks(ctx, args)
	if err != nil {
		return toolError(err), nil, nil
	}
	if len(tasks) == 0 {
		return textResult("No active team tasks."), nil, nil
	}
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		lines = append(lines, formatTaskRuntimeLine(task))
	}
	status := summarizeTaskRuntime(channel, tasks)
	return textResult(status + "\n\nCurrent team tasks:\n" + strings.Join(lines, "\n")), nil, nil
}

func handleTeamTaskStatus(ctx context.Context, _ *mcp.CallToolRequest, args TeamTasksArgs) (*mcp.CallToolResult, any, error) {
	channel, tasks, err := fetchTeamTasks(ctx, args)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(summarizeTaskRuntime(channel, tasks)), nil, nil
}

func handleTeamRuntimeState(ctx context.Context, _ *mcp.CallToolRequest, args TeamRuntimeStateArgs) (*mcp.CallToolResult, any, error) {
	slug := resolveSlugOptional(args.MySlug)
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	taskChannel, tasks, err := fetchTeamTasks(ctx, TeamTasksArgs{
		Channel:     channel,
		MySlug:      args.MySlug,
		IncludeDone: false,
	})
	if err != nil {
		return toolError(err), nil, nil
	}

	requests, err := fetchRuntimeRequests(ctx, channel, args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	recent, err := fetchRuntimeMessages(ctx, channel, args.MySlug, args.MessageLimit)
	if err != nil {
		return toolError(err), nil, nil
	}

	mode := team.SessionModeOffice
	directAgent := ""
	if isOneOnOneMode() {
		mode = team.SessionModeOneOnOne
		directAgent = team.NormalizeOneOnOneAgent(os.Getenv("WUPHF_ONE_ON_ONE_AGENT"))
	}

	snapshot := team.BuildRuntimeSnapshot(team.RuntimeSnapshotInput{
		Channel:     taskChannel,
		SessionMode: mode,
		DirectAgent: directAgent,
		Tasks:       convertRuntimeTasks(tasks),
		Requests:    requests,
		Recent:      recent,
		Capabilities: team.DetectRuntimeCapabilitiesWithOptions(team.CapabilityProbeOptions{
			IncludeConnections: true,
			ConnectionLimit:    5,
			ConnectionTimeout:  3 * time.Second,
		}),
	})
	return textResult(snapshot.FormatText()), snapshot, nil
}

func handleTeamTask(ctx context.Context, _ *mcp.CallToolRequest, args TeamTaskArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := strings.TrimSpace(args.Channel)
	if channel == "" && strings.TrimSpace(args.ID) != "" {
		channel = findTaskContextByID(ctx, mySlug, args.ID).Channel
	}
	channel = resolveConversationChannel(ctx, mySlug, channel)
	action := strings.TrimSpace(args.Action)
	payload := map[string]any{
		"action":     action,
		"channel":    channel,
		"id":         strings.TrimSpace(args.ID),
		"title":      strings.TrimSpace(args.Title),
		"details":    strings.TrimSpace(args.Details),
		"thread_id":  strings.TrimSpace(args.ThreadID),
		"created_by": mySlug,
	}
	if taskType := strings.TrimSpace(args.TaskType); taskType != "" {
		payload["task_type"] = taskType
	}
	if executionMode := strings.TrimSpace(args.ExecutionMode); executionMode != "" {
		payload["execution_mode"] = executionMode
	}
	if action == "create" && len(args.DependsOn) > 0 {
		payload["depends_on"] = args.DependsOn
	}
	switch action {
	case "claim":
		payload["owner"] = mySlug
	case "assign":
		payload["owner"] = strings.TrimSpace(args.Owner)
	default:
		if owner := strings.TrimSpace(args.Owner); owner != "" {
			payload["owner"] = owner
		}
	}

	var result struct {
		Task struct {
			ID             string `json:"id"`
			Title          string `json:"title"`
			Owner          string `json:"owner"`
			Status         string `json:"status"`
			ExecutionMode  string `json:"execution_mode"`
			WorktreePath   string `json:"worktree_path"`
			WorktreeBranch string `json:"worktree_branch"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/tasks", payload, &result); err != nil {
		return toolError(err), nil, nil
	}
	text := fmt.Sprintf("Task %s in #%s is now %s", result.Task.ID, channel, result.Task.Status)
	if result.Task.Owner != "" {
		text += " @" + result.Task.Owner
	}
	if branch := strings.TrimSpace(result.Task.WorktreeBranch); branch != "" {
		text += " · branch " + branch
	}
	if path := strings.TrimSpace(result.Task.WorktreePath); path != "" {
		text += " · working_directory " + path
	}
	text += " — " + result.Task.Title
	return textResult(text), nil, nil
}

func fetchTeamTasks(ctx context.Context, args TeamTasksArgs) (string, []brokerTaskSummary, error) {
	mySlug := strings.TrimSpace(resolveSlugOptional(args.MySlug))
	channel := resolveConversationChannel(ctx, mySlug, args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	if mySlug != "" {
		values.Set("viewer_slug", mySlug)
		values.Set("my_slug", mySlug)
	}
	if args.IncludeDone {
		values.Set("include_done", "true")
	}
	var result brokerTasksResponse
	path := "/tasks"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return "", nil, err
	}
	return channel, result.Tasks, nil
}

func summarizeTaskRuntime(channel string, tasks []brokerTaskSummary) string {
	if len(tasks) == 0 {
		return "No active team tasks."
	}

	running := 0
	isolated := 0
	reviewing := 0
	for _, task := range tasks {
		if taskCountsAsRunning(task) {
			running++
		}
		if taskUsesIsolation(task) {
			isolated++
		}
		if strings.TrimSpace(task.ReviewState) != "" && task.ReviewState != "not_required" && task.ReviewState != "approved" {
			reviewing++
		}
	}

	lines := []string{
		fmt.Sprintf("Team task status in #%s:", channel),
		fmt.Sprintf("- Running tasks: %d of %d", running, len(tasks)),
		fmt.Sprintf("- Isolated worktrees: %d", isolated),
		fmt.Sprintf("- In review flow: %d", reviewing),
	}

	isolatedTasks := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if !taskUsesIsolation(task) {
			continue
		}
		line := fmt.Sprintf("- %s", task.ID)
		if task.Owner != "" {
			line += " @" + task.Owner
		}
		if branch := strings.TrimSpace(task.WorktreeBranch); branch != "" {
			line += " · branch " + branch
		}
		if path := strings.TrimSpace(task.WorktreePath); path != "" {
			line += " · working_directory " + path
		}
		isolatedTasks = append(isolatedTasks, line)
	}
	if len(isolatedTasks) > 0 {
		lines = append(lines, "", "Isolated task worktrees:")
		lines = append(lines, isolatedTasks...)
		lines = append(lines, "", "For isolated tasks, use the listed worktree path as working_directory for local file and bash tools.")
	}

	return strings.Join(lines, "\n")
}

func fetchRuntimeRequests(ctx context.Context, channel, mySlug string) ([]team.RuntimeRequest, error) {
	values := url.Values{}
	values.Set("channel", channel)
	if viewer := strings.TrimSpace(resolveSlugOptional(mySlug)); viewer != "" {
		values.Set("viewer_slug", viewer)
	}
	var result brokerRequestsResponse
	path := "/requests"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return nil, err
	}

	requests := make([]team.RuntimeRequest, 0, len(result.Requests)+1)
	seen := map[string]bool{}
	if result.Pending != nil {
		req := team.RuntimeRequest{
			ID:       result.Pending.ID,
			Kind:     result.Pending.Kind,
			Title:    result.Pending.Title,
			Question: result.Pending.Question,
			From:     result.Pending.From,
			Blocking: result.Pending.Blocking,
			Required: result.Pending.Required,
			Status:   "pending",
			Channel:  result.Pending.Channel,
			Secret:   result.Pending.Secret,
		}
		requests = append(requests, req)
		seen[req.ID] = true
	}
	for _, req := range result.Requests {
		if seen[req.ID] {
			continue
		}
		requests = append(requests, team.RuntimeRequest{
			ID:       req.ID,
			Kind:     req.Kind,
			Title:    req.Title,
			Question: req.Question,
			From:     req.From,
			Blocking: req.Blocking,
			Required: req.Required,
			Status:   req.Status,
			Channel:  req.Channel,
			Secret:   req.Secret,
		})
	}
	return requests, nil
}

func fetchRuntimeMessages(ctx context.Context, channel, mySlug string, limit int) ([]team.RuntimeMessage, error) {
	values := url.Values{}
	values.Set("channel", channel)
	if slug := strings.TrimSpace(resolveSlugOptional(mySlug)); slug != "" {
		values.Set("my_slug", slug)
		applyAgentMessageScope(values, slug, "agent")
	}
	switch {
	case limit <= 0:
		values.Set("limit", "12")
	case limit > 40:
		values.Set("limit", "40")
	default:
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?"+values.Encode(), &result); err != nil {
		return nil, err
	}
	messages := make([]team.RuntimeMessage, 0, len(result.Messages))
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		messages = append(messages, team.RuntimeMessage{
			ID:        msg.ID,
			From:      msg.From,
			Title:     msg.Title,
			Content:   msg.Content,
			ReplyTo:   msg.ReplyTo,
			Timestamp: msg.Timestamp,
		})
	}
	return messages, nil
}

func convertRuntimeTasks(tasks []brokerTaskSummary) []team.RuntimeTask {
	out := make([]team.RuntimeTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, team.RuntimeTask{
			ID:             task.ID,
			Title:          task.Title,
			Owner:          task.Owner,
			Status:         task.Status,
			PipelineStage:  task.PipelineStage,
			ReviewState:    task.ReviewState,
			ExecutionMode:  task.ExecutionMode,
			WorktreePath:   task.WorktreePath,
			WorktreeBranch: task.WorktreeBranch,
			Blocked:        task.Blocked,
		})
	}
	return out
}

func formatTaskRuntimeLine(task brokerTaskSummary) string {
	line := fmt.Sprintf("- %s [%s]", task.ID, task.Status)
	if task.Owner != "" {
		line += " @" + task.Owner
	}
	if task.PipelineStage != "" {
		line += " · stage " + task.PipelineStage
	}
	if task.ReviewState != "" && task.ReviewState != "not_required" {
		line += " · review " + task.ReviewState
	}
	if task.ExecutionMode != "" {
		line += " · " + task.ExecutionMode
	}
	if branch := strings.TrimSpace(task.WorktreeBranch); branch != "" {
		line += " · branch " + branch
	}
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		line += " · working_directory " + path
	}
	line += " — " + task.Title
	if task.ThreadID != "" {
		line += " ↳ " + task.ThreadID
	}
	if task.Details != "" {
		line += " (" + task.Details + ")"
	}
	return line
}

func taskUsesIsolation(task brokerTaskSummary) bool {
	return strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") ||
		strings.TrimSpace(task.WorktreePath) != "" ||
		strings.TrimSpace(task.WorktreeBranch) != ""
}

func taskCountsAsRunning(task brokerTaskSummary) bool {
	status := strings.ToLower(strings.TrimSpace(task.Status))
	switch status {
	case "", "done", "completed", "canceled", "cancelled":
		return false
	default:
		return true
	}
}

func handleTeamPlan(ctx context.Context, _ *mcp.CallToolRequest, args TeamPlanArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, mySlug, args.Channel)
	if len(args.Tasks) == 0 {
		return toolError(fmt.Errorf("tasks list is empty")), nil, nil
	}

	type planItem struct {
		Title         string   `json:"title"`
		Assignee      string   `json:"assignee"`
		Details       string   `json:"details,omitempty"`
		TaskType      string   `json:"task_type,omitempty"`
		ExecutionMode string   `json:"execution_mode,omitempty"`
		DependsOn     []string `json:"depends_on,omitempty"`
	}
	items := make([]planItem, 0, len(args.Tasks))
	for _, t := range args.Tasks {
		items = append(items, planItem{
			Title:         strings.TrimSpace(t.Title),
			Assignee:      strings.TrimSpace(t.Assignee),
			Details:       strings.TrimSpace(t.Details),
			TaskType:      strings.TrimSpace(t.TaskType),
			ExecutionMode: strings.TrimSpace(t.ExecutionMode),
			DependsOn:     t.DependsOn,
		})
	}

	var result struct {
		Tasks []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Owner   string `json:"owner"`
			Status  string `json:"status"`
			Blocked bool   `json:"blocked"`
		} `json:"tasks"`
	}
	if err := brokerPostJSON(ctx, "/task-plan", map[string]any{
		"channel":    channel,
		"created_by": mySlug,
		"tasks":      items,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}

	lines := make([]string, 0, len(result.Tasks))
	for _, t := range result.Tasks {
		line := fmt.Sprintf("- %s [%s]", t.ID, t.Status)
		if t.Blocked {
			line += " BLOCKED"
		}
		if t.Owner != "" {
			line += " @" + t.Owner
		}
		line += " — " + t.Title
		lines = append(lines, line)
	}
	return textResult(fmt.Sprintf("Created %d tasks in #%s:\n%s", len(result.Tasks), channel, strings.Join(lines, "\n"))), nil, nil
}

func handleTeamMemoryQuery(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemoryQueryArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return toolError(fmt.Errorf("query is required")), nil, nil
	}
	scope, err := normalizeMemoryScope(args.Scope)
	if err != nil {
		return toolError(err), nil, nil
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}

	lines := []string{}
	privateEntries := []brokerMemoryNote{}
	sharedHits := []team.ScopedMemoryHit{}
	if scope == "auto" || scope == "private" {
		values := url.Values{}
		values.Set("namespace", privateMemoryNamespace(mySlug))
		values.Set("query", query)
		values.Set("limit", fmt.Sprintf("%d", limit))
		var result brokerMemoryResponse
		if err := brokerGetJSON(ctx, "/memory?"+values.Encode(), &result); err != nil {
			return toolError(err), nil, nil
		}
		privateEntries = append(privateEntries, result.Entries...)
		if len(result.Entries) > 0 {
			lines = append(lines, "Private memory:")
			for _, entry := range result.Entries {
				title := strings.TrimSpace(entry.Title)
				if title == "" {
					title = strings.TrimSpace(entry.Key)
				}
				lines = append(lines, fmt.Sprintf("- %s (%s): %s", title, entry.Key, truncate(strings.TrimSpace(strings.ReplaceAll(entry.Content, "\n", " ")), 220)))
			}
		}
	}
	if scope == "auto" || scope == "shared" {
		hits, err := team.QuerySharedMemory(ctx, query, limit)
		if err != nil {
			return toolError(err), nil, nil
		}
		sharedHits = append(sharedHits, hits...)
		if len(hits) > 0 {
			header := "Shared memory:"
			if scope == "shared" {
				header = fmt.Sprintf("Shared %s memory:", strings.ToUpper(hits[0].Backend[:1])+hits[0].Backend[1:])
			}
			lines = append(lines, header)
			for _, hit := range hits {
				lines = append(lines, fmt.Sprintf("- %s (%s): %s", hit.Title, hit.Identifier, truncate(strings.TrimSpace(hit.Snippet), 220)))
			}
		} else if scope == "shared" {
			lines = append(lines, "Shared memory: no relevant hits.")
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "No memory hits.")
	}
	if hints := promotionHintsForNotes(privateEntries); len(hints) > 0 {
		lines = append(lines, "", "Promotion hints:")
		lines = append(lines, hints...)
	}
	if len(sharedHits) > 0 {
		var office brokerOfficeMembersResponse
		if err := brokerGetJSON(ctx, "/office-members", &office); err == nil {
			if hints := sharedMemoryRoutingHints(mySlug, sharedHits, office); len(hints) > 0 {
				lines = append(lines, "", "Routing hints:")
				lines = append(lines, hints...)
			}
		}
	}
	return textResult(strings.Join(lines, "\n")), nil, nil
}

func handleTeamMemoryWrite(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemoryWriteArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	visibility, err := normalizeMemoryVisibility(args.Visibility)
	if err != nil {
		return toolError(err), nil, nil
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	key := derivedMemoryKey(args.Key, args.Title, content)
	title := strings.TrimSpace(args.Title)
	if visibility == "shared" {
		identifier, err := team.WriteSharedMemory(ctx, team.SharedMemoryWrite{
			Actor:   mySlug,
			Key:     key,
			Title:   title,
			Content: content,
		})
		if err != nil {
			return toolError(err), nil, nil
		}
		return textResult(fmt.Sprintf("Stored shared memory %s.", strings.TrimSpace(identifier))), nil, nil
	}
	if err := brokerPostJSON(ctx, "/memory", map[string]any{
		"namespace": privateMemoryNamespace(mySlug),
		"key":       key,
		"value": map[string]any{
			"key":     key,
			"title":   title,
			"content": content,
			"author":  mySlug,
		},
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	lines := []string{fmt.Sprintf("Saved private note %s.", key)}
	if hints := promotionHintsForNotes([]brokerMemoryNote{{
		Key:     key,
		Title:   title,
		Content: content,
		Author:  mySlug,
	}}); len(hints) > 0 {
		lines = append(lines, hints...)
	}
	return textResult(strings.Join(lines, "\n")), nil, nil
}

func handleTeamMemoryPromote(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemoryPromoteArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	key := normalizeMemoryKey(args.Key)
	if key == "" {
		return toolError(fmt.Errorf("key is required")), nil, nil
	}
	values := url.Values{}
	values.Set("namespace", privateMemoryNamespace(mySlug))
	values.Set("key", key)
	var result brokerMemoryResponse
	if err := brokerGetJSON(ctx, "/memory?"+values.Encode(), &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Entries) == 0 {
		return toolError(fmt.Errorf("private note %q not found", key)), nil, nil
	}
	entry := result.Entries[0]
	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = strings.TrimSpace(entry.Title)
	}
	identifier, err := team.WriteSharedMemory(ctx, team.SharedMemoryWrite{
		Actor:   mySlug,
		Key:     entry.Key,
		Title:   title,
		Content: strings.TrimSpace(entry.Content),
	})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Promoted private note %s into shared memory as %s.", entry.Key, strings.TrimSpace(identifier))), nil, nil
}

// handleTeamWikiWrite posts the article to the broker's wiki worker queue.
// Queue saturation surfaces as a tool error so the agent sees it and retries
// on the next turn — no hidden retries.
func handleTeamWikiWrite(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiWriteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	path := strings.TrimSpace(args.ArticlePath)
	if path == "" {
		return toolError(fmt.Errorf("article_path is required")), nil, nil
	}
	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "create"
	}
	switch mode {
	case "create", "replace", "append_section":
	default:
		return toolError(fmt.Errorf("mode must be one of create | replace | append_section; got %q", mode)), nil, nil
	}
	if strings.TrimSpace(args.Content) == "" {
		return toolError(fmt.Errorf("content is required")), nil, nil
	}
	var result struct {
		Path         string `json:"path"`
		CommitSHA    string `json:"commit_sha"`
		BytesWritten int    `json:"bytes_written"`
	}
	err = brokerPostJSON(ctx, "/wiki/write", map[string]any{
		"slug":           slug,
		"path":           path,
		"mode":           mode,
		"content":        args.Content,
		"commit_message": args.CommitMsg,
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"path":          result.Path,
		"commit_sha":    result.CommitSHA,
		"bytes_written": result.BytesWritten,
	})
	return textResult(string(payload)), nil, nil
}

// handleTeamWikiRead returns the raw article bytes.
func handleTeamWikiRead(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiReadArgs) (*mcp.CallToolResult, any, error) {
	path := strings.TrimSpace(args.ArticlePath)
	if path == "" {
		return toolError(fmt.Errorf("article_path is required")), nil, nil
	}
	bytes, err := brokerGetRaw(ctx, "/wiki/read?path="+url.QueryEscape(path))
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(string(bytes)), nil, nil
}

// handleTeamWikiSearch runs a literal substring search.
func handleTeamWikiSearch(ctx context.Context, _ *mcp.CallToolRequest, args TeamWikiSearchArgs) (*mcp.CallToolResult, any, error) {
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return toolError(fmt.Errorf("pattern is required")), nil, nil
	}
	var result struct {
		Hits []map[string]any `json:"hits"`
	}
	if err := brokerGetJSON(ctx, "/wiki/search?pattern="+url.QueryEscape(pattern), &result); err != nil {
		return toolError(err), nil, nil
	}
	payload, _ := json.Marshal(result.Hits)
	return textResult(string(payload)), nil, nil
}

// handleTeamWikiList returns the auto-regenerated catalog at index/all.md.
func handleTeamWikiList(ctx context.Context, _ *mcp.CallToolRequest, _ TeamWikiListArgs) (*mcp.CallToolResult, any, error) {
	bytes, err := brokerGetRaw(ctx, "/wiki/list")
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(string(bytes)), nil, nil
}

func handleTeamTaskAck(ctx context.Context, _ *mcp.CallToolRequest, args TeamTaskAckArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := strings.TrimSpace(args.Channel)
	if channel == "" {
		channel = findTaskContextByID(ctx, mySlug, args.ID).Channel
	}
	channel = resolveConversationChannel(ctx, mySlug, channel)
	taskID := strings.TrimSpace(args.ID)
	if taskID == "" {
		return toolError(fmt.Errorf("task ID is required")), nil, nil
	}
	var result struct {
		Task struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/tasks/ack", map[string]any{
		"id":      taskID,
		"channel": channel,
		"slug":    mySlug,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Acknowledged task %s — %s", result.Task.ID, result.Task.Title)), nil, nil
}

func handleTeamRequests(ctx context.Context, _ *mcp.CallToolRequest, args TeamRequestsArgs) (*mcp.CallToolResult, any, error) {
	viewer := strings.TrimSpace(resolveSlugOptional(args.MySlug))
	channel := resolveConversationChannel(ctx, viewer, args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	if viewer != "" {
		values.Set("viewer_slug", viewer)
	}
	if args.IncludeResolved {
		values.Set("include_resolved", "true")
	}
	var result brokerRequestsResponse
	path := "/requests"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Requests) == 0 {
		return textResult("No active office requests in #" + channel + "."), nil, nil
	}
	lines := make([]string, 0, len(result.Requests))
	for _, req := range result.Requests {
		line := fmt.Sprintf("- %s [%s] @%s", req.ID, req.Kind, req.From)
		if req.Blocking {
			line += " · blocking"
		}
		if req.Required {
			line += " · required"
		}
		if req.Title != "" {
			line += " — " + req.Title
		} else {
			line += " — " + req.Question
		}
		lines = append(lines, line)
	}
	text := "Office requests in #" + channel + ":\n" + strings.Join(lines, "\n")
	if result.Pending != nil {
		text += fmt.Sprintf("\n\nBlocking request pending: %s", result.Pending.Question)
	}
	return textResult(text), nil, nil
}

func handleTeamRequest(ctx context.Context, _ *mcp.CallToolRequest, args TeamRequestArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	ctxTarget := resolveConversationContext(ctx, slug, args.Channel, args.ReplyToID)
	channel := ctxTarget.Channel
	replyTo := ctxTarget.ReplyToID

	kind := defaultRequestKind(args.Kind)
	blocking := args.Blocking
	required := args.Required
	if kind == "approval" || kind == "confirm" || kind == "choice" {
		blocking = true
		required = true
	}
	options, recommendedID := normalizeHumanRequestOptions(kind, args.RecommendedOptionID, args.Options)

	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":           kind,
		"channel":        channel,
		"from":           slug,
		"title":          strings.TrimSpace(args.Title),
		"question":       args.Question,
		"context":        args.Context,
		"options":        options,
		"recommended_id": recommendedID,
		"blocking":       blocking,
		"required":       required,
		"secret":         args.Secret,
		"reply_to":       replyTo,
	}, &created); err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(created.ID) == "" {
		return toolError(fmt.Errorf("request did not return an ID")), nil, nil
	}
	return textResult(fmt.Sprintf("Created %s request %s in #%s.", defaultRequestKind(args.Kind), created.ID, channel)), nil, nil
}

func handleHumanInterview(ctx context.Context, _ *mcp.CallToolRequest, args HumanInterviewArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	location := resolveConversationContext(ctx, slug, args.Channel, "")
	channel := location.Channel

	options, recommendedID := normalizeHumanRequestOptions("interview", args.RecommendedOptionID, args.Options)
	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":           "interview",
		"channel":        channel,
		"from":           slug,
		"title":          "Human interview",
		"question":       args.Question,
		"context":        args.Context,
		"options":        options,
		"recommended_id": recommendedID,
		"blocking":       true,
		"required":       true,
		"reply_to":       location.ReplyToID,
	}, &created); err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(created.ID) == "" {
		return toolError(fmt.Errorf("interview request did not return an ID")), nil, nil
	}

	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return toolError(ctx.Err()), nil, nil
		case <-timeout:
			return toolError(fmt.Errorf("timed out waiting for human interview answer")), nil, nil
		case <-ticker.C:
			var result brokerInterviewAnswerResponse
			path := "/interview/answer?id=" + url.QueryEscape(created.ID)
			if err := brokerGetJSON(ctx, path, &result); err != nil {
				return toolError(err), nil, nil
			}
			if result.Answered == nil {
				continue
			}
			finalText := strings.TrimSpace(result.Answered.CustomText)
			if finalText == "" {
				finalText = strings.TrimSpace(result.Answered.ChoiceText)
			}
			payload, _ := json.MarshalIndent(map[string]any{
				"interview_id": created.ID,
				"answered":     true,
				"choice_id":    result.Answered.ChoiceID,
				"answer":       finalText,
				"answered_at":  result.Answered.AnsweredAt,
			}, "", "  ")
			return textResult(string(payload)), nil, nil
		}
	}
}

func handleHumanMessage(ctx context.Context, _ *mcp.CallToolRequest, args HumanMessageArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	ctxTarget := resolveConversationContext(ctx, slug, args.Channel, args.ReplyToID)
	channel := ctxTarget.Channel
	replyTo := ctxTarget.ReplyToID

	kind := strings.ToLower(strings.TrimSpace(args.Kind))
	switch kind {
	case "", "report":
		kind = "human_report"
	case "decision":
		kind = "human_decision"
	case "action":
		kind = "human_action"
	default:
		return toolError(fmt.Errorf("unsupported human message kind %q", args.Kind)), nil, nil
	}

	title := strings.TrimSpace(args.Title)
	if title == "" {
		switch kind {
		case "human_decision":
			title = "Decision for you"
		case "human_action":
			title = "Action for you"
		default:
			title = "Update for you"
		}
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel":  channel,
		"from":     slug,
		"kind":     kind,
		"title":    title,
		"content":  args.Content,
		"reply_to": replyTo,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}

	location := "#" + channel
	if isOneOnOneMode() {
		location = "this direct session"
	}
	text := fmt.Sprintf("Sent %s to the human in %s as @%s", strings.TrimPrefix(kind, "human_"), location, slug)
	if result.ID != "" {
		text += " (" + result.ID + ")"
	}
	if replyTo != "" {
		text += " in reply to " + replyTo
	}
	text += "."
	return textResult(text), nil, nil
}

func handleTeamChannels(ctx context.Context, _ *mcp.CallToolRequest, _ TeamChannelsArgs) (*mcp.CallToolResult, any, error) {
	var result struct {
		Channels []struct {
			Slug        string   `json:"slug"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Members     []string `json:"members"`
			Disabled    []string `json:"disabled"`
		} `json:"channels"`
	}
	if err := brokerGetJSON(ctx, "/channels", &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Channels) == 0 {
		return textResult("No office channels."), nil, nil
	}
	lines := make([]string, 0, len(result.Channels))
	for _, ch := range result.Channels {
		line := fmt.Sprintf("- #%s", ch.Slug)
		if strings.TrimSpace(ch.Description) != "" {
			line += " — " + strings.TrimSpace(ch.Description)
		}
		if len(ch.Members) > 0 {
			line += " · members: @" + strings.Join(ch.Members, ", @")
		}
		if len(ch.Disabled) > 0 {
			line += " · disabled: @" + strings.Join(ch.Disabled, ", @")
		}
		lines = append(lines, line)
	}
	return textResult("Office channels:\n" + strings.Join(lines, "\n") + "\n\nYou can inspect channel names and descriptions even if you are not a member. Only the CEO has full cross-channel content context by default."), nil, nil
}

func handleTeamDMOpen(ctx context.Context, _ *mcp.CallToolRequest, args TeamDMOpenArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Members) < 2 {
		return toolError(fmt.Errorf("members must have at least 2 entries (e.g. [\"human\", \"engineering\"])")), nil, nil
	}
	// Validate: must include human. Agent-to-agent DMs are not allowed.
	hasHuman := false
	for _, m := range args.Members {
		if m == "human" || m == "you" {
			hasHuman = true
			break
		}
	}
	if !hasHuman {
		return toolError(fmt.Errorf("DM must include 'human' as a member; agent-to-agent DMs are not allowed")), nil, nil
	}

	dmType := strings.TrimSpace(strings.ToLower(args.Type))
	if dmType == "" {
		dmType = "direct"
	}

	var result struct {
		ID      string `json:"id"`
		Slug    string `json:"slug"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Created bool   `json:"created"`
	}
	if err := brokerPostJSON(ctx, "/channels/dm", map[string]any{
		"members": args.Members,
		"type":    dmType,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}

	action := "Found existing"
	if result.Created {
		action = "Created new"
	}
	return textResult(fmt.Sprintf("%s DM channel: #%s (id: %s, type: %s, name: %s)", action, result.Slug, result.ID, result.Type, result.Name)), nil, nil
}

func handleTeamChannel(ctx context.Context, _ *mcp.CallToolRequest, args TeamChannelArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	action := strings.TrimSpace(args.Action)
	channel := normalizeSlug(args.Channel)
	switch action {
	case "create", "remove":
		if channel == "" {
			return toolError(fmt.Errorf("channel slug is required for %s", action)), nil, nil
		}
	default:
		channel = resolveConversationChannel(ctx, slug, args.Channel)
	}
	if err := brokerPostJSON(ctx, "/channels", map[string]any{
		"action":      action,
		"slug":        channel,
		"name":        strings.TrimSpace(args.Name),
		"description": strings.TrimSpace(args.Description),
		"members":     args.Members,
		"created_by":  slug,
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	if err := reconfigureOfficeSessionFn(); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("%s channel #%s", titleCaser.String(strings.TrimSpace(args.Action)), channel)), nil, nil
}

func handleTeamChannelMember(ctx context.Context, _ *mcp.CallToolRequest, args TeamChannelMemberArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	member := normalizeSlug(args.MemberSlug)
	if member == "" {
		return toolError(fmt.Errorf("member_slug is required")), nil, nil
	}
	if err := brokerPostJSON(ctx, "/channel-members", map[string]any{
		"action":  strings.TrimSpace(args.Action),
		"channel": channel,
		"slug":    member,
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	if err := reconfigureOfficeSessionFn(); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("%s @%s in #%s", titleCaser.String(strings.TrimSpace(args.Action)), member, channel)), nil, nil
}

func handleTeamBridge(ctx context.Context, _ *mcp.CallToolRequest, args TeamBridgeArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if slug != "ceo" {
		return toolError(fmt.Errorf("only the CEO can bridge channel context; ask @ceo to do it")), nil, nil
	}
	source := resolveChannel(args.SourceChannel)
	target := resolveChannel(args.TargetChannel)
	if source == target {
		return toolError(fmt.Errorf("source and target channels must be different")), nil, nil
	}
	var result struct {
		ID         string   `json:"id"`
		DecisionID string   `json:"decision_id"`
		SignalIDs  []string `json:"signal_ids"`
	}
	if err := brokerPostJSON(ctx, "/bridges", map[string]any{
		"actor":          slug,
		"source_channel": source,
		"target_channel": target,
		"summary":        strings.TrimSpace(args.Summary),
		"tagged":         args.Tagged,
		"reply_to":       strings.TrimSpace(args.ReplyToID),
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	text := fmt.Sprintf("CEO bridged context from #%s to #%s", source, target)
	if result.ID != "" {
		text += " (" + result.ID + ")"
	}
	text += "."
	return textResult(text), nil, nil
}

func handleTeamMember(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemberArgs) (*mcp.CallToolResult, any, error) {
	if _, err := resolveSlug(args.MySlug); err != nil {
		return toolError(err), nil, nil
	}
	slug := normalizeSlug(args.Slug)
	if slug == "" {
		return toolError(fmt.Errorf("slug is required")), nil, nil
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	switch action {
	case "create":
		body := map[string]any{
			"action":          "create",
			"slug":            slug,
			"name":            strings.TrimSpace(args.Name),
			"role":            strings.TrimSpace(args.Role),
			"expertise":       args.Expertise,
			"personality":     strings.TrimSpace(args.Personality),
			"permission_mode": strings.TrimSpace(args.PermissionMode),
			"created_by":      strings.TrimSpace(resolveSlugOptional(args.MySlug)),
		}
		if pkind := strings.TrimSpace(args.Provider); pkind != "" || strings.TrimSpace(args.Model) != "" {
			p := map[string]any{"kind": pkind, "model": strings.TrimSpace(args.Model)}
			if pkind == "openclaw" {
				oc := map[string]any{}
				if v := strings.TrimSpace(args.OpenclawSessionKey); v != "" {
					oc["session_key"] = v
				}
				if v := strings.TrimSpace(args.OpenclawAgentID); v != "" {
					oc["agent_id"] = v
				}
				p["openclaw"] = oc
			}
			body["provider"] = p
		}
		if err := brokerPostJSON(ctx, "/office-members", body, nil); err != nil {
			return toolError(err), nil, nil
		}
		if err := reconfigureOfficeSessionFn(); err != nil {
			return toolError(err), nil, nil
		}
		return textResult(fmt.Sprintf("Created office member @%s.", slug)), nil, nil
	case "remove":
		if err := brokerPostJSON(ctx, "/office-members", map[string]any{
			"action": "remove",
			"slug":   slug,
		}, nil); err != nil {
			return toolError(err), nil, nil
		}
		if err := reconfigureOfficeSessionFn(); err != nil {
			return toolError(err), nil, nil
		}
		return textResult(fmt.Sprintf("Removed office member @%s.", slug)), nil, nil
	default:
		return toolError(fmt.Errorf("unknown action %q", args.Action)), nil, nil
	}
}

func reconfigureLiveOffice() error {
	if !team.HasLiveTmuxSession() {
		// Web mode: no tmux session to reconfigure. The broker state is already
		// updated, and the headless turn system picks up new members by slug.
		return nil
	}
	l, err := team.NewLauncher("")
	if err != nil {
		return err
	}
	return l.ReconfigureSession()
}

func brokerBaseURL() string {
	base := strings.TrimSpace(os.Getenv("WUPHF_TEAM_BROKER_URL"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("NEX_TEAM_BROKER_URL"))
	}
	if base == "" {
		base = brokeraddr.ResolveBaseURL()
	}
	return strings.TrimRight(base, "/")
}

func authHeaders() http.Header {
	headers := http.Header{}
	token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("NEX_BROKER_TOKEN"))
	}
	if token == "" {
		token = readBrokerTokenFile()
	}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	// Identify the agent behind this MCP process so the broker can apply a
	// per-agent rate limit. A prompt-injected agent that loops on tool calls
	// will otherwise bypass the IP-scoped limiter because it holds the broker
	// token. Operator traffic from the web UI never sets this header.
	if slug := strings.TrimSpace(os.Getenv("WUPHF_AGENT_SLUG")); slug != "" {
		headers.Set("X-WUPHF-Agent", slug)
	} else if slug := strings.TrimSpace(os.Getenv("NEX_AGENT_SLUG")); slug != "" {
		headers.Set("X-WUPHF-Agent", slug)
	}
	return headers
}

func readBrokerTokenFile() string {
	path := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN_FILE"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("NEX_BROKER_TOKEN_FILE"))
	}
	if path == "" {
		path = defaultBrokerTokenFile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func isOneOnOneMode() bool {
	value := strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE"))
	return strings.EqualFold(value, "1") || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func resolveSlug(input string) (string, error) {
	if slug := strings.TrimSpace(resolveSlugOptional(input)); slug != "" {
		return slug, nil
	}
	return "", fmt.Errorf("missing agent slug; pass my_slug explicitly or set WUPHF_AGENT_SLUG")
}

func resolveSlugOptional(input string) string {
	if slug := strings.TrimSpace(input); slug != "" {
		return slug
	}
	if slug := strings.TrimSpace(os.Getenv("WUPHF_AGENT_SLUG")); slug != "" {
		return slug
	}
	return strings.TrimSpace(os.Getenv("NEX_AGENT_SLUG"))
}

func normalizeChannelInput(input string) string {
	channel := strings.TrimSpace(input)
	if channel == "" {
		return ""
	}
	channel = strings.ToLower(strings.ReplaceAll(channel, " ", "-"))
	return channel
}

func resolveChannelHint(input string) string {
	channel := normalizeChannelInput(input)
	if channel == "" {
		channel = normalizeChannelInput(os.Getenv("WUPHF_CHANNEL"))
	}
	if channel == "" {
		channel = normalizeChannelInput(os.Getenv("NEX_CHANNEL"))
	}
	return channel
}

func resolveChannel(input string) string {
	channel := resolveChannelHint(input)
	if channel == "" {
		channel = "general"
	}
	return channel
}

func resolveConversationChannel(ctx context.Context, slug string, requestedChannel string) string {
	return resolveConversationContext(ctx, slug, requestedChannel, "").Channel
}

func resolveConversationContext(ctx context.Context, slug, requestedChannel, requestedReplyTo string) conversationContext {
	channel := resolveChannelHint(requestedChannel)
	replyTo := strings.TrimSpace(requestedReplyTo)
	if channel != "" {
		if replyTo == "" {
			replyTo = defaultReplyTargetForChannel(ctx, slug, channel)
		}
		return conversationContext{Channel: channel, ReplyToID: replyTo, Source: "explicit_channel"}
	}

	if replyTo != "" {
		if located := findMessageContextByID(ctx, slug, replyTo); located.Channel != "" {
			located.ReplyToID = replyTo
			located.Source = "explicit_reply"
			return located
		}
	}

	if isOneOnOneMode() {
		channel = resolveChannel("")
		if replyTo == "" {
			replyTo = inferDirectReplyTarget(ctx, slug, channel)
		}
		return conversationContext{Channel: channel, ReplyToID: replyTo, Source: "direct_session"}
	}

	if inferred := inferRecentConversationContext(ctx, slug); inferred.Channel != "" {
		if replyTo != "" {
			inferred.ReplyToID = replyTo
		}
		if inferred.ReplyToID == "" {
			inferred.ReplyToID = defaultReplyTargetForChannel(ctx, slug, inferred.Channel)
		}
		return inferred
	}

	if inferred := inferTaskConversationContext(ctx, slug); inferred.Channel != "" {
		if replyTo != "" {
			inferred.ReplyToID = replyTo
		}
		if inferred.ReplyToID == "" {
			inferred.ReplyToID = defaultReplyTargetForChannel(ctx, slug, inferred.Channel)
		}
		return inferred
	}

	channel = resolveChannel("")
	if replyTo == "" {
		replyTo = defaultReplyTargetForChannel(ctx, slug, channel)
	}
	return conversationContext{Channel: channel, ReplyToID: replyTo, Source: "fallback"}
}

func fetchAccessibleChannels(ctx context.Context, slug string) []brokerChannelSummary {
	var result brokerChannelsResponse
	if err := brokerGetJSON(ctx, "/channels", &result); err != nil {
		return nil
	}
	slug = strings.TrimSpace(slug)
	if slug == "" || slug == "ceo" {
		return result.Channels
	}
	out := make([]brokerChannelSummary, 0, len(result.Channels))
	for _, ch := range result.Channels {
		if !contains(ch.Members, slug) || contains(ch.Disabled, slug) {
			continue
		}
		out = append(out, ch)
	}
	return out
}

func fetchChannelMessages(ctx context.Context, channel, slug, scope string, limit int) []brokerMessage {
	values := url.Values{}
	values.Set("channel", channel)
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	slug = strings.TrimSpace(slug)
	if slug != "" {
		values.Set("my_slug", slug)
		values.Set("viewer_slug", slug)
		if strings.TrimSpace(scope) != "" {
			values.Set("scope", strings.TrimSpace(scope))
		}
	}
	var result brokerMessagesResponse
	path := "/messages?" + values.Encode()
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return nil
	}
	return result.Messages
}

func inferRecentConversationContext(ctx context.Context, slug string) conversationContext {
	channels := fetchAccessibleChannels(ctx, slug)
	var (
		best      conversationContext
		bestStamp time.Time
	)
	for _, ch := range channels {
		messages := fetchChannelMessages(ctx, ch.Slug, slug, "inbox", 40)
		if len(messages) == 0 {
			continue
		}
		candidate, stamp := latestRelevantMessageContext(messages, slug, ch.Slug)
		if candidate.Channel == "" || stamp.IsZero() {
			continue
		}
		if best.Channel == "" || stamp.After(bestStamp) {
			best = candidate
			bestStamp = stamp
		}
	}
	return best
}

func latestRelevantMessageContext(messages []brokerMessage, slug, fallbackChannel string) (conversationContext, time.Time) {
	byID := make(map[string]brokerMessage, len(messages))
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.From) == strings.TrimSpace(slug) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msg.Content), "[STATUS]") {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(msg.Timestamp))
		if err != nil {
			continue
		}
		channel := normalizeChannelInput(msg.Channel)
		if channel == "" {
			channel = normalizeChannelInput(fallbackChannel)
		}
		if channel == "" {
			channel = "general"
		}
		return conversationContext{
			Channel:   channel,
			ReplyToID: threadTargetForMessage(msg, byID),
			Source:    "recent_message",
		}, stamp
	}
	return conversationContext{}, time.Time{}
}

func threadTargetForMessage(msg brokerMessage, byID map[string]brokerMessage) string {
	current := strings.TrimSpace(msg.ID)
	parent := strings.TrimSpace(msg.ReplyTo)
	if parent == "" {
		return current
	}
	seen := map[string]bool{}
	for parent != "" {
		if seen[parent] {
			return parent
		}
		seen[parent] = true
		next, ok := byID[parent]
		if !ok || strings.TrimSpace(next.ReplyTo) == "" {
			return parent
		}
		parent = strings.TrimSpace(next.ReplyTo)
	}
	return current
}

func inferTaskConversationContext(ctx context.Context, slug string) conversationContext {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return conversationContext{}
	}
	channels := fetchAccessibleChannels(ctx, slug)
	var (
		best      conversationContext
		bestStamp time.Time
	)
	for _, ch := range channels {
		values := url.Values{}
		values.Set("channel", ch.Slug)
		values.Set("viewer_slug", slug)
		values.Set("my_slug", slug)
		var result brokerTasksResponse
		if err := brokerGetJSON(ctx, "/tasks?"+values.Encode(), &result); err != nil {
			continue
		}
		for _, task := range result.Tasks {
			if !taskCountsAsRunning(task) {
				continue
			}
			stamp := parseLatestTaskTime(task)
			if best.Channel != "" && !stamp.After(bestStamp) {
				continue
			}
			best = conversationContext{
				Channel:   normalizeChannelInput(task.Channel),
				ReplyToID: strings.TrimSpace(task.ThreadID),
				Source:    "owned_task",
			}
			bestStamp = stamp
		}
	}
	return best
}

func parseLatestTaskTime(task brokerTaskSummary) time.Time {
	for _, raw := range []string{strings.TrimSpace(task.UpdatedAt), strings.TrimSpace(task.CreatedAt)} {
		if raw == "" {
			continue
		}
		if stamp, err := time.Parse(time.RFC3339, raw); err == nil {
			return stamp
		}
	}
	return time.Time{}
}

func findMessageContextByID(ctx context.Context, slug, messageID string) conversationContext {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return conversationContext{}
	}
	for _, ch := range fetchAccessibleChannels(ctx, slug) {
		messages := fetchChannelMessages(ctx, ch.Slug, slug, "", 100)
		byID := make(map[string]brokerMessage, len(messages))
		for _, msg := range messages {
			if id := strings.TrimSpace(msg.ID); id != "" {
				byID[id] = msg
			}
		}
		if msg, ok := byID[messageID]; ok {
			return conversationContext{
				Channel:   ch.Slug,
				ReplyToID: threadTargetForMessage(msg, byID),
				Source:    "message_lookup",
			}
		}
	}
	return conversationContext{}
}

func findTaskContextByID(ctx context.Context, slug, taskID string) conversationContext {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return conversationContext{}
	}
	channels := fetchAccessibleChannels(ctx, slug)
	for _, ch := range channels {
		values := url.Values{}
		values.Set("channel", ch.Slug)
		if strings.TrimSpace(slug) != "" {
			values.Set("viewer_slug", strings.TrimSpace(slug))
		}
		values.Set("include_done", "true")
		var result brokerTasksResponse
		if err := brokerGetJSON(ctx, "/tasks?"+values.Encode(), &result); err != nil {
			continue
		}
		for _, task := range result.Tasks {
			if strings.TrimSpace(task.ID) == taskID {
				return conversationContext{
					Channel:   ch.Slug,
					ReplyToID: strings.TrimSpace(task.ThreadID),
					Source:    "task_lookup",
				}
			}
		}
	}
	return conversationContext{}
}

func defaultReplyTargetForChannel(ctx context.Context, slug, channel string) string {
	channel = resolveChannel(channel)
	if isOneOnOneMode() {
		return inferDirectReplyTarget(ctx, slug, channel)
	}
	if replyTo, err := inferReplyTarget(ctx, slug, channel); err == nil && strings.TrimSpace(replyTo) != "" {
		return strings.TrimSpace(replyTo)
	}
	if replyTo, err := inferDefaultThreadTarget(ctx, slug, channel); err == nil && strings.TrimSpace(replyTo) != "" {
		return strings.TrimSpace(replyTo)
	}
	return ""
}

func inferDirectReplyTarget(ctx context.Context, slug, channel string) string {
	messages := fetchChannelMessages(ctx, channel, slug, "", 40)
	if len(messages) == 0 {
		return ""
	}
	byID := make(map[string]brokerMessage, len(messages))
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.From) == strings.TrimSpace(slug) {
			continue
		}
		return threadTargetForMessage(msg, byID)
	}
	return ""
}

func normalizeSlug(input string) string {
	slug := strings.ToLower(strings.TrimSpace(input))
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	return slug
}

func defaultRequestKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "choice"
	}
	return kind
}

func humanRequestOptionDefaults(kind string) ([]HumanInterviewOption, string) {
	switch strings.TrimSpace(kind) {
	case "approval":
		return []HumanInterviewOption{
			{ID: "approve", Label: "Approve", Description: "Green-light this and let the team execute immediately."},
			{ID: "approve_with_note", Label: "Approve with note", Description: "Proceed, but attach explicit constraints or guardrails.", RequiresText: true, TextHint: "Type the conditions, constraints, or guardrails the team must follow."},
			{ID: "reject", Label: "Reject", Description: "Do not proceed with this."},
			{ID: "reject_with_steer", Label: "Reject with steer", Description: "Do not proceed as proposed. Redirect the team with clearer steering.", RequiresText: true, TextHint: "Type the steering, redirect, or rationale for rejecting this request."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review or unblock this yourself."},
		}, "approve"
	case "confirm":
		return []HumanInterviewOption{
			{ID: "confirm_proceed", Label: "Confirm", Description: "Looks good. Proceed as planned."},
			{ID: "adjust", Label: "Adjust", Description: "Proceed only after applying the changes you specify.", RequiresText: true, TextHint: "Type the changes that must happen before proceeding."},
			{ID: "reassign", Label: "Reassign", Description: "Move this to a different owner or scope.", RequiresText: true, TextHint: "Type who should own this instead, or how the scope should change."},
			{ID: "hold", Label: "Hold", Description: "Do not act yet. Keep this pending for review."},
		}, "confirm_proceed"
	case "choice":
		return []HumanInterviewOption{
			{ID: "move_fast", Label: "Move fast", Description: "Bias toward speed. Ship now and iterate later."},
			{ID: "balanced", Label: "Balanced", Description: "Balance speed, risk, and quality."},
			{ID: "be_careful", Label: "Be careful", Description: "Bias toward caution and a tighter review loop."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Gather more context before deciding.", RequiresText: true, TextHint: "Type what is missing or what should be investigated next."},
			{ID: "delegate", Label: "Delegate", Description: "Hand this to a specific owner for a closer call.", RequiresText: true, TextHint: "Type who should own this decision and any guidance for them."},
		}, "balanced"
	case "freeform", "secret":
		return []HumanInterviewOption{
			{ID: "proceed", Label: "Proceed", Description: "Let the team handle it with their best judgment."},
			{ID: "give_direction", Label: "Give direction", Description: "Proceed, but only after you provide specific guidance.", RequiresText: true, TextHint: "Type the direction or constraints the team should follow."},
			{ID: "delegate", Label: "Delegate", Description: "Route this to a specific person.", RequiresText: true, TextHint: "Type who should own this and what they should do."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review this further."},
		}, "proceed"
	default:
		return nil, ""
	}
}

func normalizeHumanRequestOptions(kind, recommendedID string, options []HumanInterviewOption) ([]HumanInterviewOption, string) {
	defaults, fallback := humanRequestOptionDefaults(kind)
	if len(options) == 0 {
		return defaults, chooseRecommendedID(strings.TrimSpace(recommendedID), fallback)
	}
	meta := make(map[string]HumanInterviewOption, len(defaults))
	for _, option := range defaults {
		meta[strings.TrimSpace(option.ID)] = option
	}
	out := make([]HumanInterviewOption, 0, len(options))
	for _, option := range options {
		if base, ok := meta[strings.TrimSpace(option.ID)]; ok {
			if !option.RequiresText {
				option.RequiresText = base.RequiresText
			}
			if strings.TrimSpace(option.TextHint) == "" {
				option.TextHint = base.TextHint
			}
			if strings.TrimSpace(option.Label) == "" {
				option.Label = base.Label
			}
			if strings.TrimSpace(option.Description) == "" {
				option.Description = base.Description
			}
		}
		out = append(out, option)
	}
	return out, chooseRecommendedID(strings.TrimSpace(recommendedID), fallback)
}

func chooseRecommendedID(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

func normalizePollScope(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "all", "channel":
		return "", nil
	case "agent", "inbox", "outbox":
		return strings.TrimSpace(strings.ToLower(value)), nil
	default:
		return "", fmt.Errorf("invalid scope %q", value)
	}
}

func normalizeMemoryScope(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "auto":
		return "auto", nil
	case "private":
		return "private", nil
	case "shared":
		return "shared", nil
	default:
		return "", fmt.Errorf("invalid scope %q", value)
	}
}

func normalizeMemoryVisibility(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "private":
		return "private", nil
	case "shared":
		return "shared", nil
	default:
		return "", fmt.Errorf("invalid visibility %q", value)
	}
}

func privateMemoryNamespace(slug string) string {
	return "agent/" + strings.TrimSpace(slug)
}

func normalizeMemoryKey(key string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(strings.ToLower(key)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func derivedMemoryKey(explicit string, title string, content string) string {
	if key := normalizeMemoryKey(explicit); key != "" {
		return key
	}
	if key := normalizeMemoryKey(title); key != "" {
		return key + "-" + time.Now().UTC().Format("20060102-150405")
	}
	words := strings.Fields(content)
	if len(words) > 6 {
		words = words[:6]
	}
	if key := normalizeMemoryKey(strings.Join(words, " ")); key != "" {
		return key + "-" + time.Now().UTC().Format("20060102-150405")
	}
	return "note-" + time.Now().UTC().Format("20060102-150405")
}

func applyAgentMessageScope(values url.Values, slug, scope string) {
	slug = strings.TrimSpace(slug)
	if slug == "" || slug == "ceo" || isOneOnOneMode() {
		return
	}
	values.Set("viewer_slug", slug)
	if scope == "" {
		scope = "agent"
	}
	values.Set("scope", scope)
}

func brokerGetJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerBaseURL()+path, nil)
	if err != nil {
		return err
	}
	req.Header = authHeaders()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker GET %s failed: %s %s", path, res.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// brokerGetRaw is like brokerGetJSON but returns the raw response body for
// endpoints that serve text/plain (the wiki read / list endpoints).
func brokerGetRaw(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerBaseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header = authHeaders()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4*1024*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("broker GET %s failed: %s %s", path, res.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func brokerPostJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, brokerBaseURL()+path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header = authHeaders()
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker POST %s failed: %s %s", path, res.Status, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func brokerPutJSON(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, brokerBaseURL()+path, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header = authHeaders()
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("broker PUT %s failed: %s %s", path, res.Status, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func inferReplyTarget(ctx context.Context, slug string, channel string) (string, error) {
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?channel="+url.QueryEscape(channel)+"&my_slug="+url.QueryEscape(slug)+"&limit=25", &result); err != nil {
		return "", err
	}
	byID := make(map[string]brokerMessage, len(result.Messages))
	for _, msg := range result.Messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		if msg.From == slug {
			continue
		}
		if !contains(msg.Tagged, slug) {
			continue
		}
		if !isRecentEnough(msg.Timestamp, 15*time.Minute) {
			continue
		}
		return threadTargetForMessage(msg, byID), nil
	}
	return "", nil
}

func inferDefaultThreadTarget(ctx context.Context, slug string, channel string) (string, error) {
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?channel="+url.QueryEscape(channel)+"&my_slug="+url.QueryEscape(slug)+"&limit=40", &result); err != nil {
		return "", err
	}
	byID := make(map[string]brokerMessage, len(result.Messages))
	for _, msg := range result.Messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		if msg.From == slug {
			continue
		}
		if strings.HasPrefix(msg.Content, "[STATUS]") {
			continue
		}
		if !isRecentEnough(msg.Timestamp, 20*time.Minute) {
			continue
		}
		return threadTargetForMessage(msg, byID), nil
	}
	return "", nil
}

func isRecentEnough(ts string, maxAge time.Duration) bool {
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return time.Since(parsed) <= maxAge
}

func formatMessages(messages []brokerMessage, mySlug string) string {
	if len(messages) == 0 {
		return "No recent team messages."
	}
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		ts := msg.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}
		tagNote := ""
		if mySlug != "" && contains(msg.Tagged, mySlug) {
			tagNote = " [tagged you]"
		}
		threadNote := ""
		if msg.ReplyTo != "" {
			threadNote = " ↳ " + msg.ReplyTo
		}
		// Truncate content to avoid token explosion when agents return long code
		// blocks or reports. 800 chars is enough for context without burning tokens.
		// team_poll is background context; agents who need the full output can read
		// it directly from the thread via a targeted team_poll with thread_id.
		const pollContentLimit = 800
		if msg.Kind == "automation" || msg.From == "wuphf" || msg.From == "nex" {
			source := msg.Source
			if source == "" {
				source = "context_graph"
			}
			label := msg.SourceLabel
			if label == "" {
				label = "WUPHF"
			}
			title := ""
			if msg.Title != "" {
				title = msg.Title + ": "
			}
			content := msg.Content
			if len(content) > pollContentLimit {
				content = content[:pollContentLimit] + "…"
			}
			lines = append(lines, fmt.Sprintf("%s %s%s [%s/%s]: %s%s%s", ts, msg.ID, threadNote, label, source, title, content, tagNote))
			continue
		}
		content := msg.Content
		if len(content) > pollContentLimit {
			content = content[:pollContentLimit] + "…"
		}
		lines = append(lines, fmt.Sprintf("%s %s%s @%s: %s%s", ts, msg.ID, threadNote, msg.From, content, tagNote))
	}
	return strings.Join(lines, "\n")
}

func latestHumanRequestSummary(messages []brokerMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		from := strings.TrimSpace(strings.ToLower(msg.From))
		if from != "you" && from != "human" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		ts := msg.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}
		return fmt.Sprintf("%s %s @%s: %s", ts, msg.ID, msg.From, content)
	}
	return ""
}

func formatTaskSummary(ctx context.Context, mySlug string, channel string) string {
	values := url.Values{}
	values.Set("channel", channel)
	if strings.TrimSpace(mySlug) != "" {
		values.Set("my_slug", mySlug)
	}
	var result brokerTasksResponse
	path := "/tasks"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Tasks) == 0 {
		return "Open tasks: none"
	}
	lines := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		line := fmt.Sprintf("- %s [%s]", task.ID, task.Status)
		if task.Owner != "" {
			line += " @" + task.Owner
		}
		line += " — " + task.Title
		lines = append(lines, line)
	}
	return "Open tasks:\n" + strings.Join(lines, "\n")
}

func formatRequestSummary(ctx context.Context, channel string) string {
	values := url.Values{}
	values.Set("channel", channel)
	var result brokerRequestsResponse
	path := "/requests?" + values.Encode()
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Requests) == 0 {
		return "Open requests: none"
	}
	lines := make([]string, 0, len(result.Requests))
	for _, req := range result.Requests {
		line := fmt.Sprintf("- %s [%s] @%s", req.ID, req.Kind, req.From)
		if req.Blocking {
			line += " · blocking"
		}
		if req.Title != "" {
			line += " — " + req.Title
		} else {
			line += " — " + req.Question
		}
		lines = append(lines, line)
	}
	return "Open requests:\n" + strings.Join(lines, "\n")
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func toolError(err error) *mcp.CallToolResult {
	res := textResult(err.Error())
	res.IsError = true
	return res
}

func truncate(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return text[:max-1] + "…"
}
