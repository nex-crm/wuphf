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
	DependsOn        []string `json:"depends_on,omitempty"`
	Blocked          bool     `json:"blocked,omitempty"`
	Handoffs         []struct {
		FromAgent string `json:"from_agent"`
		ToAgent   string `json:"to_agent"`
		WhatIDid  string `json:"what_i_did"`
		WhatToDo  string `json:"what_to_do"`
		Context   string `json:"context"`
	} `json:"handoffs,omitempty"`
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
}

type TeamStatusArgs struct {
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	Status  string `json:"status" jsonschema:"Short status like 'reviewing onboarding flow' or 'implementing search index'"`
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Agent slug sending the status. Defaults to WUPHF_AGENT_SLUG."`
}

type HumanInterviewOption struct {
	ID          string `json:"id" jsonschema:"Stable short ID like 'sales' or 'smbs'"`
	Label       string `json:"label" jsonschema:"User-facing option label"`
	Description string `json:"description,omitempty" jsonschema:"One-sentence explanation of tradeoff or impact"`
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

type TeamTaskArgs struct {
	Action    string   `json:"action" jsonschema:"One of: create, claim, assign, complete, block, release"`
	Channel   string   `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	ID        string   `json:"id,omitempty" jsonschema:"Task ID for non-create actions"`
	Title     string   `json:"title,omitempty" jsonschema:"Task title when creating a task"`
	Details   string   `json:"details,omitempty" jsonschema:"Optional detail or update"`
	Owner     string   `json:"owner,omitempty" jsonschema:"Owner slug for claim or assign"`
	ThreadID  string   `json:"thread_id,omitempty" jsonschema:"Related thread or message id"`
	DependsOn []string `json:"depends_on,omitempty" jsonschema:"Task IDs this task depends on. Blocked until all dependencies are done."`
	MySlug    string   `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamPlanArgs struct {
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	Tasks   []struct {
		Title     string   `json:"title" jsonschema:"Task title"`
		Assignee  string   `json:"assignee" jsonschema:"Agent slug to own this task"`
		Details   string   `json:"details,omitempty" jsonschema:"Optional task details"`
		DependsOn []string `json:"depends_on,omitempty" jsonschema:"Titles or IDs of tasks this depends on"`
	} `json:"tasks" jsonschema:"List of tasks to create in dependency order"`
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamMemoryWriteArgs struct {
	Key    string `json:"key" jsonschema:"Key to store under your namespace"`
	Value  string `json:"value" jsonschema:"Value to store"`
	MySlug string `json:"my_slug,omitempty" jsonschema:"Your agent slug (used as namespace). Defaults to WUPHF_AGENT_SLUG."`
}

type TeamTaskAckArgs struct {
	ID      string `json:"id" jsonschema:"Task ID to acknowledge"`
	Channel string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamConcludeArgs struct {
	Channel   string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	ThreadID  string `json:"thread_id" jsonschema:"Root message ID of the thread to conclude"`
	Discussed string `json:"discussed" jsonschema:"What topics were covered"`
	Decided   string `json:"decided" jsonschema:"What decisions were made"`
	Done      string `json:"done" jsonschema:"What concrete work was completed"`
	OpenItems string `json:"open_items,omitempty" jsonschema:"Anything remaining or handed off"`
	MySlug    string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamHandoffArgs struct {
	Channel  string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	TaskID   string `json:"task_id" jsonschema:"Task ID to hand off"`
	ToAgent  string `json:"to_agent" jsonschema:"Agent slug receiving the handoff"`
	WhatIDid string `json:"what_i_did" jsonschema:"What you completed"`
	WhatToDo string `json:"what_to_do" jsonschema:"What the receiving agent should do next"`
	Context  string `json:"context,omitempty" jsonschema:"Relevant details, gotchas, dependencies"`
	MySlug   string `json:"my_slug,omitempty" jsonschema:"Your agent slug. Defaults to WUPHF_AGENT_SLUG."`
}

type TeamReopenArgs struct {
	Channel  string `json:"channel,omitempty" jsonschema:"Channel slug. Defaults to the agent's current channel or general."`
	ThreadID string `json:"thread_id" jsonschema:"Root message ID of the thread to reopen"`
	Reason   string `json:"reason" jsonschema:"Why the thread needs to be reopened"`
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
			Name:        "team_broadcast",
			Description: "Compatibility alias in 1:1 mode. Send your normal reply into the direct conversation.",
		}, handleTeamBroadcast)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "read_conversation",
			Description: "Read recent messages from the 1:1 conversation so you stay in sync before replying.",
		}, handleTeamPoll)
		mcp.AddTool(server, &mcp.Tool{
			Name:        "team_poll",
			Description: "Compatibility alias in 1:1 mode. Read recent messages from the direct conversation before replying.",
		}, handleTeamPoll)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "human_interview",
			Description: "Ask the human a blocking interview question when you truly cannot proceed responsibly without a decision.",
		}, handleHumanInterview)

		mcp.AddTool(server, &mcp.Tool{
			Name:        "human_message",
			Description: "Send a direct human-facing note into the chat when you need to present completion, recommend a decision, or tell the human what they should do next.",
		}, handleHumanMessage)

		registerActionTools(server)

		return server.Run(ctx, &mcp.StdioTransport{})
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_broadcast",
		Description: "Post a message into the team channel for all teammates to see.",
	}, handleTeamBroadcast)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_react",
		Description: "React to a message with an emoji instead of posting a full reply. Use this when you agree with what someone said and have nothing new to add. Common reactions: 👍 (agree), 💯 (strongly agree), 🔥 (great idea), 👀 (noted/watching), ✅ (done/confirmed).",
	}, handleTeamReact)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_poll",
		Description: "Read recent messages from the team channel so you stay in sync before replying.",
	}, handleTeamPoll)

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
		Name:        "team_task",
		Description: "Create, claim, assign, complete, block, or release a shared task in the office task list.",
	}, handleTeamTask)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_plan",
		Description: "Create a structured task plan with dependencies in one call. Tasks execute in dependency order. Use this for work involving 2+ agents.",
	}, handleTeamPlan)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_memory_write",
		Description: "Write a key-value pair into the shared team memory under your namespace. All agents can read all namespaces via team_poll.",
	}, handleTeamMemoryWrite)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_task_ack",
		Description: "Acknowledge a task assigned to you, confirming you have seen it and will begin work.",
	}, handleTeamTaskAck)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_conclude",
		Description: "Conclude a thread with a structured summary. The summary is shown directly to the human. Write it for them, not for agents.",
	}, handleTeamConclude)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_handoff",
		Description: "Hand off a task to another agent with full context: what you did, what they need to do, and relevant details.",
	}, handleTeamHandoff)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_reopen",
		Description: "Reopen a previously concluded thread. CEO/lead only.",
	}, handleTeamReopen)

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
	channel := resolveChannel(args.Channel)

	replyTo := strings.TrimSpace(args.ReplyToID)
	if !isOneOnOneMode() && replyTo == "" && !args.NewTopic {
		replyTo, _ = inferReplyTarget(ctx, slug, channel)
	}
	if !isOneOnOneMode() && replyTo == "" && !args.NewTopic {
		replyTo, _ = inferDefaultThreadTarget(ctx, slug, channel)
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
	err = brokerPostJSON(ctx, "/reactions", map[string]any{
		"message_id": args.MessageID,
		"emoji":      args.Emoji,
		"from":       slug,
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}
	if result.Duplicate {
		return textResult(fmt.Sprintf("Already reacted %s to %s.", args.Emoji, args.MessageID)), nil, nil
	}
	return textResult(fmt.Sprintf("Reacted %s to %s as @%s.", args.Emoji, args.MessageID, slug)), nil, nil
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
	// Never suppress CEO.
	if strings.TrimSpace(slug) == "" || slug == "ceo" {
		return ""
	}
	// Never suppress if the agent is explicitly tagged or owns the task.
	latest := latestRelevantMessage(messages, replyTo)
	latestDomain := inferOfficeTextDomain(content)
	if latestDomain == "general" && latest != nil {
		latestDomain = inferOfficeTextDomain(latest.Title + " " + latest.Content)
	}
	if latest != nil && (containsSlug(latest.Tagged, slug) || containsSlug(latest.Tagged, "all")) {
		return ""
	}
	if ownsRelevantTask(slug, replyTo, latestDomain, tasks) {
		return ""
	}
	// If the agent already posted in this thread, don't suppress follow-ups.
	if replyTo != "" {
		for _, msg := range messages {
			if msg.From == slug && (msg.ReplyTo == replyTo || msg.ID == replyTo) {
				return ""
			}
		}
	}
	// Allow everything else — agents should share viewpoints.
	// The specialist prompt tells them to use reactions for agreement instead.
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
	channel := resolveChannel(args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	if slug := strings.TrimSpace(resolveSlugOptional(args.MySlug)); slug != "" {
		values.Set("my_slug", slug)
	}
	if since := strings.TrimSpace(args.SinceID); since != "" {
		values.Set("since_id", since)
	}
	if args.Limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", args.Limit))
	} else if isOneOnOneMode() {
		values.Set("limit", "8")
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
		if focus == "" {
			return textResult("Direct conversation\n\n" + summary), nil, nil
		}
		return textResult("Direct conversation\n\nLatest human request to answer now:\n" + focus + "\n\nOlder messages are background unless the latest request depends on them.\n\nRecent messages:\n" + summary), nil, nil
	}
	mySlug := resolveSlugOptional(args.MySlug)
	taskSummary := formatTaskSummary(ctx, mySlug, channel)
	requestSummary := formatRequestSummary(ctx, channel)
	memorySummary := formatMemorySummary(ctx)
	conclusionsSummary := formatConclusionsSummary(ctx, channel)
	handoffsSummary := formatPendingHandoffs(ctx, mySlug, channel)
	return textResult(fmt.Sprintf("Channel #%s\n\n%s\n\nTagged messages for you: %d\n\n%s\n\n%s\n\n%s\n\n%s\n\n%s", channel, summary, result.TaggedCount, taskSummary, requestSummary, memorySummary, conclusionsSummary, handoffsSummary)), nil, nil
}

func handleTeamStatus(ctx context.Context, _ *mcp.CallToolRequest, args TeamStatusArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
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
	channel := resolveChannel(args.Channel)
	viewer := strings.TrimSpace(resolveSlugOptional(args.MySlug))
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
	mySlug := strings.TrimSpace(resolveSlugOptional(args.MySlug))
	channel := resolveChannel(args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	if mySlug != "" {
		values.Set("viewer_slug", mySlug)
	}
	if mySlug != "" {
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
		return toolError(err), nil, nil
	}
	if len(result.Tasks) == 0 {
		return textResult("No active team tasks."), nil, nil
	}
	lines := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		line := fmt.Sprintf("- %s [%s]", task.ID, task.Status)
		if task.Blocked {
			line += " BLOCKED"
		}
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
		line += " — " + task.Title
		if len(task.DependsOn) > 0 {
			line += " (deps: " + strings.Join(task.DependsOn, ", ") + ")"
		}
		if task.ThreadID != "" {
			line += " ↳ " + task.ThreadID
		}
		if task.Details != "" {
			line += " (" + task.Details + ")"
		}
		lines = append(lines, line)
	}
	return textResult("Current team tasks in #" + channel + ":\n" + strings.Join(lines, "\n")), nil, nil
}

func handleTeamTask(ctx context.Context, _ *mcp.CallToolRequest, args TeamTaskArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
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
	if len(args.DependsOn) > 0 {
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
			ID      string `json:"id"`
			Title   string `json:"title"`
			Owner   string `json:"owner"`
			Status  string `json:"status"`
			Blocked bool   `json:"blocked"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/tasks", payload, &result); err != nil {
		return toolError(err), nil, nil
	}
	text := fmt.Sprintf("Task %s in #%s is now %s", result.Task.ID, channel, result.Task.Status)
	if result.Task.Blocked {
		text += " (BLOCKED — waiting on dependencies)"
	}
	if result.Task.Owner != "" {
		text += " @" + result.Task.Owner
	}
	text += " — " + result.Task.Title
	return textResult(text), nil, nil
}

func handleTeamPlan(ctx context.Context, _ *mcp.CallToolRequest, args TeamPlanArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
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
	channel := resolveChannel(args.Channel)
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

func handleTeamConclude(ctx context.Context, _ *mcp.CallToolRequest, args TeamConcludeArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	threadID := strings.TrimSpace(args.ThreadID)
	if threadID == "" {
		return toolError(fmt.Errorf("thread_id is required")), nil, nil
	}
	var result struct {
		Conclusion struct {
			ThreadID    string `json:"thread_id"`
			ConcludedAt string `json:"concluded_at"`
		} `json:"conclusion"`
	}
	if err := brokerPostJSON(ctx, "/conclude", map[string]any{
		"channel":      channel,
		"thread_id":    threadID,
		"discussed":    strings.TrimSpace(args.Discussed),
		"decided":      strings.TrimSpace(args.Decided),
		"done":         strings.TrimSpace(args.Done),
		"open_items":   strings.TrimSpace(args.OpenItems),
		"concluded_by": mySlug,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Thread %s in #%s concluded.", result.Conclusion.ThreadID, channel)), nil, nil
}

func handleTeamHandoff(ctx context.Context, _ *mcp.CallToolRequest, args TeamHandoffArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	taskID := strings.TrimSpace(args.TaskID)
	toAgent := strings.TrimSpace(args.ToAgent)
	if taskID == "" || toAgent == "" {
		return toolError(fmt.Errorf("task_id and to_agent are required")), nil, nil
	}
	var result struct {
		Task struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Owner string `json:"owner"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/handoff", map[string]any{
		"channel":    channel,
		"task_id":    taskID,
		"from_agent": mySlug,
		"to_agent":   toAgent,
		"what_i_did": strings.TrimSpace(args.WhatIDid),
		"what_to_do": strings.TrimSpace(args.WhatToDo),
		"context":    strings.TrimSpace(args.Context),
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Handed off %s to @%s — %s", result.Task.ID, result.Task.Owner, result.Task.Title)), nil, nil
}

func handleTeamReopen(ctx context.Context, _ *mcp.CallToolRequest, args TeamReopenArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
	threadID := strings.TrimSpace(args.ThreadID)
	if threadID == "" {
		return toolError(fmt.Errorf("thread_id is required")), nil, nil
	}
	if err := brokerPostJSON(ctx, "/conclude/reopen", map[string]any{
		"channel":   channel,
		"thread_id": threadID,
		"reason":    strings.TrimSpace(args.Reason),
		"slug":      mySlug,
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Thread %s in #%s reopened.", threadID, channel)), nil, nil
}

func handleTeamRequests(ctx context.Context, _ *mcp.CallToolRequest, args TeamRequestsArgs) (*mcp.CallToolResult, any, error) {
	channel := resolveChannel(args.Channel)
	viewer := strings.TrimSpace(resolveSlugOptional(args.MySlug))
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
	channel := resolveChannel(args.Channel)
	replyTo := strings.TrimSpace(args.ReplyToID)
	if replyTo == "" {
		replyTo, _ = inferReplyTarget(ctx, slug, channel)
	}

	kind := defaultRequestKind(args.Kind)
	blocking := args.Blocking
	required := args.Required
	if kind == "approval" || kind == "confirm" || kind == "choice" {
		blocking = true
		required = true
	}

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
		"options":        args.Options,
		"recommended_id": args.RecommendedOptionID,
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
	channel := resolveChannel(args.Channel)

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
		"options":        args.Options,
		"recommended_id": args.RecommendedOptionID,
		"blocking":       true,
		"required":       true,
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
	channel := resolveChannel(args.Channel)
	replyTo := strings.TrimSpace(args.ReplyToID)
	if replyTo == "" {
		replyTo, _ = inferReplyTarget(ctx, slug, channel)
	}

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
	channel := resolveChannel(args.Channel)
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
	if _, err := resolveSlug(args.MySlug); err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveChannel(args.Channel)
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

func resolveChannel(input string) string {
	channel := strings.TrimSpace(input)
	if channel == "" {
		channel = strings.TrimSpace(os.Getenv("WUPHF_CHANNEL"))
	}
	if channel == "" {
		channel = strings.TrimSpace(os.Getenv("NEX_CHANNEL"))
	}
	if channel == "" {
		channel = "general"
	}
	channel = strings.ToLower(strings.ReplaceAll(channel, " ", "-"))
	return channel
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
		return msg.ID, nil
	}
	return "", nil
}

func inferDefaultThreadTarget(ctx context.Context, slug string, channel string) (string, error) {
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?channel="+url.QueryEscape(channel)+"&my_slug="+url.QueryEscape(slug)+"&limit=40", &result); err != nil {
		return "", err
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
		return msg.ID, nil
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

func formatMemorySummary(ctx context.Context) string {
	var result struct {
		Memory map[string]map[string]string `json:"memory"`
	}
	if err := brokerGetJSON(ctx, "/memory", &result); err != nil || len(result.Memory) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Shared Context\n")
	for ns, entries := range result.Memory {
		sb.WriteString("### " + ns + "\n")
		for key, val := range entries {
			sb.WriteString("- " + key + ": " + val + "\n")
		}
	}
	return sb.String()
}

func formatConclusionsSummary(ctx context.Context, channel string) string {
	var result struct {
		Conclusions []struct {
			ThreadID string `json:"thread_id"`
			Summary  struct {
				Discussed string `json:"discussed"`
				Decided   string `json:"decided"`
				Done      string `json:"done"`
				OpenItems string `json:"open_items"`
			} `json:"summary"`
			ConcludedBy string `json:"concluded_by"`
		} `json:"conclusions"`
	}
	path := "/conclusions?channel=" + url.QueryEscape(channel) + "&limit=5"
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Conclusions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Recent Conclusions\n")
	for _, c := range result.Conclusions {
		sb.WriteString(fmt.Sprintf("- Thread %s (@%s): Done: %s", c.ThreadID, c.ConcludedBy, c.Summary.Done))
		if c.Summary.OpenItems != "" {
			sb.WriteString(" | Open: " + c.Summary.OpenItems)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func formatPendingHandoffs(ctx context.Context, mySlug, channel string) string {
	if mySlug == "" {
		return ""
	}
	var result brokerTasksResponse
	path := "/tasks?channel=" + url.QueryEscape(channel) + "&my_slug=" + url.QueryEscape(mySlug)
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Tasks) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, task := range result.Tasks {
		if task.Owner != mySlug || len(task.Handoffs) == 0 {
			continue
		}
		h := task.Handoffs[len(task.Handoffs)-1]
		if h.ToAgent != mySlug {
			continue
		}
		if sb.Len() == 0 {
			sb.WriteString("## Pending Handoffs\n")
		}
		sb.WriteString(fmt.Sprintf("- %s from @%s: What was done: %s | What to do: %s", task.ID, h.FromAgent, h.WhatIDid, h.WhatToDo))
		if h.Context != "" {
			sb.WriteString(" | Context: " + h.Context)
		}
		sb.WriteString("\n")
	}
	return sb.String()
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
