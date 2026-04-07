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

	"github.com/nex-crm/wuphf/internal/team"
)

const defaultBrokerBaseURL = "http://127.0.0.1:7890"
const defaultBrokerTokenFile = "/tmp/wuphf-broker-token"

var reconfigureOfficeSessionFn = reconfigureLiveOffice

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
	Action   string `json:"action" jsonschema:"One of: create, claim, assign, complete, block, release"`
	Channel  string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	ID       string `json:"id,omitempty" jsonschema:"Task ID for non-create actions"`
	Title    string `json:"title,omitempty" jsonschema:"Task title when creating a task"`
	Details  string `json:"details,omitempty" jsonschema:"Optional detail or update"`
	Owner    string `json:"owner,omitempty" jsonschema:"Owner slug for claim or assign"`
	ThreadID string `json:"thread_id,omitempty" jsonschema:"Related thread or message id"`
	MySlug   string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
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
	MySlug         string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

func Run(ctx context.Context) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "wuphf-team",
		Version: "0.1.0",
	}, nil)

	if isOneOnOneMode() {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "reply",
			Description: "Send your reply to the human in the direct 1:1 conversation.",
		}, handleTeamBroadcast)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "read_conversation",
			Description: "Read recent messages from the 1:1 conversation so you stay in sync before replying.",
		}, handleTeamPoll)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "human_interview",
			Description: "Ask the human a blocking interview question when you truly cannot proceed responsibly without a decision.",
		}, handleHumanInterview)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "human_message",
			Description: "Send a direct human-facing note into the chat when you need to present completion, recommend a decision, or tell the human what they should do next.",
		}, handleHumanMessage)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "team_runtime_state",
			Description: "Return the canonical runtime snapshot for this direct session, including tasks, pending human requests, recovery summary, and runtime capabilities.",
		}, handleTeamRuntimeState)

		registerActionTools(server)

		return server.Run(ctx, &mcp.StdioTransport{})
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_broadcast",
		Description: "Post a message into the team channel for all teammates to see.",
	}, handleTeamBroadcast)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_poll",
		Description: "Read recent messages from the team channel so you stay in sync before replying.",
	}, handleTeamPoll)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_inbox",
		Description: "Read only the messages that currently belong in your agent inbox: human asks, CEO guidance, tags to you, and replies in your threads.",
	}, handleTeamInbox)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_outbox",
		Description: "Read only the messages you authored, so you can review what you already told the office.",
	}, handleTeamOutbox)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_status",
		Description: "Share a short status update in the team channel. This is rendered as lightweight activity in the channel UI.",
	}, handleTeamStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_members",
		Description: "List active participants in the shared team channel with their latest visible activity.",
	}, handleTeamMembers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_office_members",
		Description: "List the office-wide roster, including members who are not in the current channel.",
	}, handleTeamOfficeMembers)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_channels",
		Description: "List available office channels, their descriptions, and their memberships. Agents can see channel metadata even when they are not members.",
	}, handleTeamChannels)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_channel",
		Description: "Create or remove an office channel. When creating a channel, include a clear description of what work belongs there and the initial roster that should be in it. Only do this when the human explicitly wants channel structure.",
	}, handleTeamChannel)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_channel_member",
		Description: "Add, remove, disable, or enable an agent in a specific office channel.",
	}, handleTeamChannelMember)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_bridge",
		Description: "CEO-only tool to bridge relevant context from one channel into another and leave a visible cross-channel trail.",
	}, handleTeamBridge)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_member",
		Description: "Create or remove an office-wide member. Only create new members when the human explicitly wants to expand the team.",
	}, handleTeamMember)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_tasks",
		Description: "List the current shared tasks and who owns them so the team does not duplicate work.",
	}, handleTeamTasks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_task_status",
		Description: "Summarize how many shared tasks are running and whether any are isolated in local worktrees.",
	}, handleTeamTaskStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_runtime_state",
		Description: "Return the canonical office runtime snapshot, including tasks, pending human requests, recovery summary, and runtime capabilities.",
	}, handleTeamRuntimeState)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_task",
		Description: "Create, claim, assign, complete, block, or release a shared task in the office task list.",
	}, handleTeamTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_requests",
		Description: "List the current office requests so you know whether the human already owes the team a decision.",
	}, handleTeamRequests)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_request",
		Description: "Create a structured request for the human: confirmation, choice, approval, freeform answer, or private/secret answer.",
	}, handleTeamRequest)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "human_interview",
		Description: "Ask the human a blocking interview question when the team cannot proceed responsibly without a decision.",
	}, handleHumanInterview)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "human_message",
		Description: "Send a direct human-facing note into the main chat when you need to present completion, recommend a decision, or tell the human what they should do next.",
	}, handleHumanMessage)

	registerActionTools(server)

	return server.Run(ctx, &mcp.StdioTransport{})
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
	return textResult(text), nil, nil
}

func fetchBroadcastContext(ctx context.Context, channel, mySlug string) ([]brokerMessage, []brokerTaskSummary, error) {
	values := url.Values{}
	values.Set("channel", channel)
	values.Set("limit", "40")
	if mySlug != "" {
		values.Set("my_slug", mySlug)
	}
	var messages brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?"+values.Encode(), &messages); err != nil {
		return nil, nil, err
	}
	var tasks brokerTasksResponse
	if err := brokerGetJSON(ctx, "/tasks?channel="+url.QueryEscape(channel), &tasks); err != nil {
		return messages.Messages, nil, err
	}
	return messages.Messages, tasks.Tasks, nil
}

func suppressBroadcastReason(slug, content, replyTo string, messages []brokerMessage, tasks []brokerTaskSummary) string {
	if strings.TrimSpace(slug) == "" || slug == "ceo" {
		return ""
	}
	myDomain := inferOfficeAgentDomain(slug)
	latest := latestRelevantMessage(messages, replyTo)
	latestDomain := inferOfficeTextDomain(content)
	if latestDomain == "general" && latest != nil {
		latestDomain = inferOfficeTextDomain(latest.Title + " " + latest.Content)
	}
	explicitNeed := latest != nil && containsSlug(latest.Tagged, slug)
	ownsTask := ownsRelevantTask(slug, replyTo, latestDomain, tasks)

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

func ownsRelevantTask(slug, replyTo, domain string, tasks []brokerTaskSummary) bool {
	slug = strings.TrimSpace(slug)
	replyTo = strings.TrimSpace(replyTo)
	for _, task := range tasks {
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		if strings.TrimSpace(task.Owner) != slug {
			continue
		}
		if replyTo != "" {
			if strings.TrimSpace(task.ThreadID) == replyTo {
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

func inferOfficeAgentDomain(slug string) string {
	switch strings.ToLower(strings.TrimSpace(slug)) {
	case "fe", "frontend":
		return "frontend"
	case "be", "backend":
		return "backend"
	case "ai", "ml", "llm":
		return "ai"
	case "designer", "design":
		return "design"
	case "cmo", "growth", "marketing":
		return "marketing"
	case "cro", "sales", "revenue":
		return "sales"
	case "pm", "product", "ceo":
		return "product"
	default:
		return "general"
	}
}

func inferOfficeTextDomain(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	tokens := tokenizeOfficeText(text)
	switch {
	case hasAnyOfficeToken(tokens, "frontend", "ui", "ux", "web", "component") || containsAnyFragment(text, "hero", "cta", "signup page"):
		return "frontend"
	case hasAnyOfficeToken(tokens, "backend", "database", "api", "sync", "queue", "service", "auth", "integration"):
		return "backend"
	case hasAnyOfficeToken(tokens, "model", "prompt", "llm", "ai", "transcript", "embedding", "rag"):
		return "ai"
	case hasAnyOfficeToken(tokens, "design", "visual", "typography", "layout") || containsAnyFragment(text, "brand system"):
		return "design"
	case hasAnyOfficeToken(tokens, "positioning", "campaign", "launch", "brand", "marketing", "copy", "persona", "messaging", "growth"):
		return "marketing"
	case hasAnyOfficeToken(tokens, "sales", "pipeline", "pricing", "revenue", "deal", "budget", "buyer"):
		return "sales"
	case hasAnyOfficeToken(tokens, "product", "roadmap", "scope", "planning", "priority"):
		return "product"
	default:
		return "general"
	}
}

func tokenizeOfficeText(text string) map[string]bool {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(' ')
		}
	}
	parts := strings.Fields(b.String())
	out := make(map[string]bool, len(parts))
	for _, part := range parts {
		out[part] = true
	}
	return out
}

func hasAnyOfficeToken(tokens map[string]bool, words ...string) bool {
	for _, word := range words {
		if tokens[word] {
			return true
		}
	}
	return false
}

func containsAnyFragment(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

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
		return textResult("Direct conversation\n\n" + summary), nil, nil
	}
	if scope == "inbox" || scope == "outbox" {
		scopeTitle := strings.Title(scope)
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
		Channel:      taskChannel,
		SessionMode:  mode,
		DirectAgent:  directAgent,
		Tasks:        convertRuntimeTasks(tasks),
		Requests:     requests,
		Recent:       recent,
		Capabilities: team.DetectRuntimeCapabilities(),
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
		Title     string   `json:"title"`
		Assignee  string   `json:"assignee"`
		Details   string   `json:"details,omitempty"`
		DependsOn []string `json:"depends_on,omitempty"`
	}
	items := make([]planItem, 0, len(args.Tasks))
	for _, t := range args.Tasks {
		items = append(items, planItem{
			Title:     strings.TrimSpace(t.Title),
			Assignee:  strings.TrimSpace(t.Assignee),
			Details:   strings.TrimSpace(t.Details),
			DependsOn: t.DependsOn,
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

func handleTeamMemoryWrite(ctx context.Context, _ *mcp.CallToolRequest, args TeamMemoryWriteArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	key := strings.TrimSpace(args.Key)
	if key == "" {
		return toolError(fmt.Errorf("key is required")), nil, nil
	}
	if err := brokerPostJSON(ctx, "/memory", map[string]any{
		"namespace": mySlug,
		"key":       key,
		"value":     args.Value,
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Saved %s/%s to shared memory.", mySlug, key)), nil, nil
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

func handleTeamChannel(ctx context.Context, _ *mcp.CallToolRequest, args TeamChannelArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	if err := brokerPostJSON(ctx, "/channels", map[string]any{
		"action":      strings.TrimSpace(args.Action),
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
	return textResult(fmt.Sprintf("%s channel #%s", strings.Title(strings.TrimSpace(args.Action)), channel)), nil, nil
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
	return textResult(fmt.Sprintf("%s @%s in #%s", strings.Title(strings.TrimSpace(args.Action)), member, channel)), nil, nil
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
		if err := brokerPostJSON(ctx, "/office-members", map[string]any{
			"action":          "create",
			"slug":            slug,
			"name":            strings.TrimSpace(args.Name),
			"role":            strings.TrimSpace(args.Role),
			"expertise":       args.Expertise,
			"personality":     strings.TrimSpace(args.Personality),
			"permission_mode": strings.TrimSpace(args.PermissionMode),
			"created_by":      strings.TrimSpace(resolveSlugOptional(args.MySlug)),
		}, nil); err != nil {
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
		base = defaultBrokerBaseURL
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
			lines = append(lines, fmt.Sprintf("%s %s%s [%s/%s]: %s%s%s", ts, msg.ID, threadNote, label, source, title, msg.Content, tagNote))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s%s @%s: %s%s", ts, msg.ID, threadNote, msg.From, msg.Content, tagNote))
	}
	return strings.Join(lines, "\n")
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
