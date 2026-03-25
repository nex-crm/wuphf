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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultBrokerBaseURL = "http://127.0.0.1:7890"
const defaultBrokerTokenFile = "/tmp/wuphf-broker-token"

type brokerMessage struct {
	ID          string   `json:"id"`
	From        string   `json:"from"`
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
		LastMessage string `json:"lastMessage"`
		LastTime    string `json:"lastTime"`
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

type TeamBroadcastArgs struct {
	Content   string   `json:"content" jsonschema:"Message to post to the shared team channel"`
	MySlug    string   `json:"my_slug,omitempty" jsonschema:"Agent slug sending the message. Defaults to WUPHF_AGENT_SLUG."`
	Tagged    []string `json:"tagged,omitempty" jsonschema:"Optional list of tagged agent slugs who should respond"`
	ReplyToID string   `json:"reply_to_id,omitempty" jsonschema:"Reply in-thread to a specific message ID when continuing a narrow discussion"`
	NewTopic  bool     `json:"new_topic,omitempty" jsonschema:"Set true only when this genuinely needs to start a new top-level thread"`
}

type TeamPollArgs struct {
	MySlug  string `json:"my_slug,omitempty" jsonschema:"Your agent slug so tagged_count can be computed. Defaults to WUPHF_AGENT_SLUG."`
	SinceID string `json:"since_id,omitempty" jsonschema:"Only return messages after this message ID"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum messages to return (default 20, max 100)"`
}

type TeamStatusArgs struct {
	Status string `json:"status" jsonschema:"Short status like 'reviewing onboarding flow' or 'implementing search index'"`
	MySlug string `json:"my_slug,omitempty" jsonschema:"Agent slug sending the status. Defaults to WUPHF_AGENT_SLUG."`
}

type HumanInterviewOption struct {
	ID          string `json:"id" jsonschema:"Stable short ID like 'sales' or 'smbs'"`
	Label       string `json:"label" jsonschema:"User-facing option label"`
	Description string `json:"description,omitempty" jsonschema:"One-sentence explanation of tradeoff or impact"`
}

type HumanInterviewArgs struct {
	Question            string                 `json:"question" jsonschema:"The specific decision or clarification needed from the human"`
	Context             string                 `json:"context,omitempty" jsonschema:"Short context explaining why the team is asking now"`
	MySlug              string                 `json:"my_slug,omitempty" jsonschema:"Agent slug asking the question. Defaults to WUPHF_AGENT_SLUG."`
	Options             []HumanInterviewOption `json:"options,omitempty" jsonschema:"Suggested answer options to show the human"`
	RecommendedOptionID string                 `json:"recommended_option_id,omitempty" jsonschema:"Which option you recommend, if any"`
}

func Run(ctx context.Context) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "wuphf-team",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_broadcast",
		Description: "Post a message into the shared team channel so the human and every agent can see it.",
	}, handleTeamBroadcast)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "team_poll",
		Description: "Read recent messages from the shared team channel so you can stay in sync with teammates.",
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
		Name:        "human_interview",
		Description: "Ask the human a blocking interview question when the team cannot proceed responsibly without a decision.",
	}, handleHumanInterview)

	return server.Run(ctx, &mcp.StdioTransport{})
}

func handleTeamBroadcast(ctx context.Context, _ *mcp.CallToolRequest, args TeamBroadcastArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}

	replyTo := strings.TrimSpace(args.ReplyToID)
	if replyTo == "" && !args.NewTopic {
		replyTo, _ = inferReplyTarget(ctx, slug)
	}
	if replyTo == "" && !args.NewTopic {
		replyTo, _ = inferDefaultThreadTarget(ctx, slug)
	}

	var result struct {
		ID string `json:"id"`
	}
	err = brokerPostJSON(ctx, "/messages", map[string]any{
		"from":     slug,
		"content":  args.Content,
		"tagged":   args.Tagged,
		"reply_to": replyTo,
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}

	text := fmt.Sprintf("Posted to team channel as @%s", slug)
	if result.ID != "" {
		text += " (" + result.ID + ")"
	}
	if replyTo != "" {
		text += " in reply to " + replyTo
	}
	text += "."
	return textResult(text), nil, nil
}

func handleTeamPoll(ctx context.Context, _ *mcp.CallToolRequest, args TeamPollArgs) (*mcp.CallToolResult, any, error) {
	values := url.Values{}
	if slug := strings.TrimSpace(resolveSlugOptional(args.MySlug)); slug != "" {
		values.Set("my_slug", slug)
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
	return textResult(fmt.Sprintf("%s\n\nTagged messages for you: %d", summary, result.TaggedCount)), nil, nil
}

func handleTeamStatus(ctx context.Context, _ *mcp.CallToolRequest, args TeamStatusArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"from":    slug,
		"content": "[STATUS] " + args.Status,
		"tagged":  []string{},
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Updated team status for @%s: %s", slug, args.Status)), nil, nil
}

func handleTeamMembers(ctx context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
	var result brokerMembersResponse
	if err := brokerGetJSON(ctx, "/members", &result); err != nil {
		return toolError(err), nil, nil
	}
	if len(result.Members) == 0 {
		return textResult("No active team members yet."), nil, nil
	}
	lines := make([]string, 0, len(result.Members))
	for _, member := range result.Members {
		line := "- @" + member.Slug
		if member.LastTime != "" {
			line += " at " + member.LastTime
		}
		if member.LastMessage != "" {
			line += " — " + member.LastMessage
		}
		lines = append(lines, line)
	}
	return textResult("Active team members:\n" + strings.Join(lines, "\n")), nil, nil
}

func handleHumanInterview(ctx context.Context, _ *mcp.CallToolRequest, args HumanInterviewArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/interview", map[string]any{
		"from":           slug,
		"question":       args.Question,
		"context":        args.Context,
		"options":        args.Options,
		"recommended_id": args.RecommendedOptionID,
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

func inferReplyTarget(ctx context.Context, slug string) (string, error) {
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?my_slug="+url.QueryEscape(slug)+"&limit=25", &result); err != nil {
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

func inferDefaultThreadTarget(ctx context.Context, slug string) (string, error) {
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?my_slug="+url.QueryEscape(slug)+"&limit=40", &result); err != nil {
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
