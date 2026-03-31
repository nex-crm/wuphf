package action

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

const composioDigestWorkflowKind = "wuphf_digest_email_v1"

type composioDigestWorkflowDefinition struct {
	Kind             string `json:"kind"`
	Title            string `json:"title,omitempty"`
	Description      string `json:"description,omitempty"`
	ConnectionKey    string `json:"connection_key"`
	RecipientEmail   string `json:"recipient_email,omitempty"`
	Subject          string `json:"subject,omitempty"`
	Query            string `json:"query,omitempty"`
	WindowHours      int    `json:"window_hours,omitempty"`
	MaxResults       int    `json:"max_results,omitempty"`
	InsightLimit     int    `json:"insight_limit,omitempty"`
	IncludeSpamTrash bool   `json:"include_spam_trash,omitempty"`
	DigestPrompt     string `json:"digest_prompt,omitempty"`
}

type composioDigestWorkflowInputs struct {
	ConnectionKey    string
	RecipientEmail   string
	Subject          string
	Query            string
	WindowHours      int
	MaxResults       int
	InsightLimit     int
	IncludeSpamTrash bool
	DigestPrompt     string
}

type composioFetchedMessage struct {
	MessageID        string   `json:"messageId"`
	ThreadID         string   `json:"threadId"`
	Timestamp        string   `json:"messageTimestamp"`
	Subject          string   `json:"subject"`
	Sender           string   `json:"sender"`
	To               string   `json:"to"`
	MessageText      string   `json:"messageText"`
	LabelIDs         []string `json:"labelIds"`
	AttachmentList   []any    `json:"attachmentList"`
	Preview          struct {
		Body    string `json:"body"`
		Subject string `json:"subject"`
	} `json:"preview"`
}

type composioFetchEmailsResponse struct {
	Data struct {
		Messages           []composioFetchedMessage `json:"messages"`
		NextPageToken      string                   `json:"nextPageToken"`
		ResultSizeEstimate int                      `json:"resultSizeEstimate"`
	} `json:"data"`
}

type workflowRunRecord struct {
	Provider    string                     `json:"provider"`
	WorkflowKey string                     `json:"workflow_key"`
	RunID       string                     `json:"run_id"`
	Status      string                     `json:"status"`
	StartedAt   string                     `json:"started_at"`
	FinishedAt  string                     `json:"finished_at"`
	Steps       map[string]json.RawMessage `json:"steps,omitempty"`
}

func (c *ComposioREST) CreateWorkflow(ctx context.Context, req WorkflowCreateRequest) (WorkflowCreateResult, error) {
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return WorkflowCreateResult{}, fmt.Errorf("workflow key is required")
	}
	normalized, err := c.normalizeWorkflowDefinition(req.Definition)
	if err != nil {
		return WorkflowCreateResult{}, err
	}
	path, err := saveWorkflowDefinition(c.Name(), key, normalized)
	if err != nil {
		return WorkflowCreateResult{}, err
	}
	return WorkflowCreateResult{Created: true, Key: key, Path: path}, nil
}

func (c *ComposioREST) ExecuteWorkflow(ctx context.Context, req WorkflowExecuteRequest) (WorkflowExecuteResult, error) {
	key, definition, _, err := loadWorkflowDefinition(c.Name(), req.KeyOrPath)
	if err != nil {
		return WorkflowExecuteResult{}, err
	}
	spec, err := c.decodeDigestWorkflowDefinition(definition)
	if err != nil {
		return WorkflowExecuteResult{}, err
	}
	inputs := mergeDigestWorkflowInputs(spec, req.Inputs)
	if inputs.ConnectionKey == "" {
		return WorkflowExecuteResult{}, fmt.Errorf("workflow %s is missing connection_key", key)
	}
	if inputs.RecipientEmail == "" {
		return WorkflowExecuteResult{}, fmt.Errorf("workflow %s is missing recipient_email", key)
	}

	runID := fmt.Sprintf("cmpwf_%d", time.Now().UTC().UnixNano())
	startedAt := time.Now().UTC()
	steps := map[string]json.RawMessage{}

	fetchResult, err := c.ExecuteAction(ctx, ExecuteRequest{
		Platform:      "gmail",
		ActionID:      "GMAIL_FETCH_EMAILS",
		ConnectionKey: inputs.ConnectionKey,
		Data: map[string]any{
			"query":              inputs.Query,
			"max_results":        inputs.MaxResults,
			"include_payload":    false,
			"verbose":            false,
			"include_spam_trash": inputs.IncludeSpamTrash,
		},
	})
	if err != nil {
		return WorkflowExecuteResult{}, err
	}
	steps["fetch_emails"] = mustMarshalJSON(map[string]any{
		"request": fetchResult.Request,
	})

	var fetchPayload composioFetchEmailsResponse
	if err := json.Unmarshal(fetchResult.Response, &fetchPayload); err != nil {
		return WorkflowExecuteResult{}, fmt.Errorf("parse composio workflow fetch response: %w", err)
	}
	messages := fetchPayload.Data.Messages
	steps["fetch_emails"] = mustMarshalJSON(map[string]any{
		"request":              fetchResult.Request,
		"count":                len(messages),
		"next_page_token":      fetchPayload.Data.NextPageToken,
		"result_size_estimate": fetchPayload.Data.ResultSizeEstimate,
		"messages":             summarizeFetchedMessages(messages),
	})

	digestBody, hydration, err := buildDigestBody(messages, inputs)
	if err != nil {
		return WorkflowExecuteResult{}, err
	}
	steps["hydrate_digest"] = mustMarshalJSON(hydration)

	sendResult, err := c.ExecuteAction(ctx, ExecuteRequest{
		Platform:      "gmail",
		ActionID:      "GMAIL_SEND_EMAIL",
		ConnectionKey: inputs.ConnectionKey,
		DryRun:        req.DryRun,
		Data: map[string]any{
			"recipient_email": inputs.RecipientEmail,
			"subject":         inputs.Subject,
			"body":            digestBody,
		},
	})
	if err != nil {
		return WorkflowExecuteResult{}, err
	}
	steps["send_email"] = mustMarshalJSON(map[string]any{
		"dry_run":  sendResult.DryRun,
		"request":  sendResult.Request,
		"response": json.RawMessage(sendResult.Response),
	})

	status := "completed"
	if req.DryRun {
		status = "planned"
	}
	result := WorkflowExecuteResult{
		RunID:  runID,
		Status: status,
		Steps:  steps,
		Events: []json.RawMessage{
			mustMarshalJSON(map[string]any{"event": "workflow_started", "run_id": runID, "workflow_key": key}),
			mustMarshalJSON(map[string]any{"event": "workflow_finished", "run_id": runID, "status": status}),
		},
	}

	_ = appendWorkflowRun(c.Name(), key, workflowRunRecord{
		Provider:    c.Name(),
		WorkflowKey: key,
		RunID:       runID,
		Status:      status,
		StartedAt:   startedAt.Format(time.RFC3339),
		FinishedAt:  time.Now().UTC().Format(time.RFC3339),
		Steps:       steps,
	})
	return result, nil
}

func (c *ComposioREST) ListWorkflowRuns(_ context.Context, key string) (WorkflowRunsResult, error) {
	return listWorkflowRuns(c.Name(), key)
}

func (c *ComposioREST) normalizeWorkflowDefinition(definition json.RawMessage) (json.RawMessage, error) {
	spec, err := c.decodeDigestWorkflowDefinition(definition)
	if err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *ComposioREST) decodeDigestWorkflowDefinition(definition json.RawMessage) (composioDigestWorkflowDefinition, error) {
	var spec composioDigestWorkflowDefinition
	if !json.Valid(definition) {
		return spec, fmt.Errorf("workflow definition must be valid JSON")
	}
	if err := json.Unmarshal(definition, &spec); err != nil {
		return spec, fmt.Errorf("parse workflow definition: %w", err)
	}
	if strings.TrimSpace(spec.Kind) == "" {
		return spec, fmt.Errorf("workflow definition must include kind")
	}
	if strings.TrimSpace(spec.Kind) != composioDigestWorkflowKind {
		return spec, fmt.Errorf("unsupported composio workflow kind %q", spec.Kind)
	}
	spec.ConnectionKey = strings.TrimSpace(spec.ConnectionKey)
	spec.RecipientEmail = fallbackString(spec.RecipientEmail, config.ResolveComposioUserID())
	spec.Subject = fallbackString(spec.Subject, "WUPHF Daily Digest")
	if spec.WindowHours <= 0 {
		spec.WindowHours = 24
	}
	if spec.MaxResults <= 0 {
		spec.MaxResults = 20
	}
	if spec.InsightLimit <= 0 {
		spec.InsightLimit = 5
	}
	spec.Query = fallbackString(spec.Query, defaultDigestQuery(spec.WindowHours))
	spec.DigestPrompt = strings.TrimSpace(spec.DigestPrompt)
	if spec.ConnectionKey == "" {
		return spec, fmt.Errorf("workflow definition must include connection_key")
	}
	if spec.RecipientEmail == "" {
		return spec, fmt.Errorf("workflow definition must include recipient_email")
	}
	return spec, nil
}

func mergeDigestWorkflowInputs(spec composioDigestWorkflowDefinition, overrides map[string]any) composioDigestWorkflowInputs {
	inputs := composioDigestWorkflowInputs{
		ConnectionKey:    spec.ConnectionKey,
		RecipientEmail:   spec.RecipientEmail,
		Subject:          spec.Subject,
		Query:            spec.Query,
		WindowHours:      spec.WindowHours,
		MaxResults:       spec.MaxResults,
		InsightLimit:     spec.InsightLimit,
		IncludeSpamTrash: spec.IncludeSpamTrash,
		DigestPrompt:     spec.DigestPrompt,
	}
	if v := strings.TrimSpace(stringInput(overrides["connection_key"])); v != "" {
		inputs.ConnectionKey = v
	}
	if v := strings.TrimSpace(stringInput(overrides["recipient_email"])); v != "" {
		inputs.RecipientEmail = v
	}
	if v := strings.TrimSpace(stringInput(overrides["subject"])); v != "" {
		inputs.Subject = v
	}
	if v := strings.TrimSpace(stringInput(overrides["query"])); v != "" {
		inputs.Query = v
	}
	if v := intInput(overrides["window_hours"]); v > 0 {
		inputs.WindowHours = v
	}
	if v := intInput(overrides["max_results"]); v > 0 {
		inputs.MaxResults = v
	}
	if v := intInput(overrides["insight_limit"]); v > 0 {
		inputs.InsightLimit = v
	}
	if v, ok := boolInput(overrides["include_spam_trash"]); ok {
		inputs.IncludeSpamTrash = v
	}
	if v := strings.TrimSpace(stringInput(overrides["digest_prompt"])); v != "" {
		inputs.DigestPrompt = v
	}
	if inputs.Query == "" {
		inputs.Query = defaultDigestQuery(inputs.WindowHours)
	}
	if inputs.Subject == "" {
		inputs.Subject = "WUPHF Daily Digest"
	}
	return inputs
}

func summarizeFetchedMessages(messages []composioFetchedMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		out = append(out, map[string]any{
			"message_id": msg.MessageID,
			"thread_id":  msg.ThreadID,
			"timestamp":  msg.Timestamp,
			"subject":    fallbackString(msg.Subject, msg.Preview.Subject),
			"sender":     msg.Sender,
			"to":         msg.To,
			"preview":    truncateText(fallbackString(msg.Preview.Body, msg.MessageText), 240),
			"labels":     msg.LabelIDs,
		})
	}
	return out
}

func buildDigestBody(messages []composioFetchedMessage, inputs composioDigestWorkflowInputs) (string, map[string]any, error) {
	summaries := summarizeFetchedMessages(messages)
	sort.Slice(summaries, func(i, j int) bool {
		return stringInput(summaries[i]["timestamp"]) > stringInput(summaries[j]["timestamp"])
	})
	insights, insightsErr := nexInsightsSince(time.Now().UTC().Add(-time.Duration(inputs.WindowHours)*time.Hour), inputs.InsightLimit)
	askPrompt := composeDigestPrompt(summaries, insights.Insights, inputs)
	answer, askErr := nexAsk(askPrompt)

	body := strings.TrimSpace(answer.Answer)
	if body == "" || askErr != nil {
		body = fallbackDigestBody(summaries, insights.Insights, inputs, askErr)
	}
	hydration := map[string]any{
		"emails_considered": len(summaries),
		"insights_considered": len(insights.Insights),
		"nex_prompt": askPrompt,
		"nex_answer": body,
	}
	if insightsErr != nil {
		hydration["insights_error"] = insightsErr.Error()
	}
	if askErr != nil {
		hydration["nex_error"] = askErr.Error()
	}
	return body, hydration, nil
}

func composeDigestPrompt(emails []map[string]any, insights []nexInsightItem, inputs composioDigestWorkflowInputs) string {
	if strings.TrimSpace(inputs.DigestPrompt) != "" {
		return strings.TrimSpace(inputs.DigestPrompt) + "\n\nRecent emails:\n" + string(mustMarshalJSON(emails)) + "\n\nRecent insights:\n" + string(mustMarshalJSON(insights))
	}
	return fmt.Sprintf(
		"Create a plain-text daily digest email for the human operator.\n\n"+
			"Use the recent emails and relevant organizational context to produce these sections exactly:\n"+
			"1. Executive Summary\n2. Why This Matters\n3. What To Do Next\n4. Email Highlights\n5. Relevant Nex Insights\n\n"+
			"Requirements:\n"+
			"- focus on the last %d hours\n"+
			"- explain why each priority matters now\n"+
			"- give concrete, short next actions\n"+
			"- mention sender and subject in Email Highlights\n"+
			"- keep it concise but useful\n\n"+
			"Recent emails:\n%s\n\nRecent Nex insights:\n%s\n",
		inputs.WindowHours,
		string(mustMarshalJSON(emails)),
		string(mustMarshalJSON(insights)),
	)
}

func fallbackDigestBody(emails []map[string]any, insights []nexInsightItem, inputs composioDigestWorkflowInputs, askErr error) string {
	var lines []string
	lines = append(lines, "Executive Summary")
	if len(emails) == 0 {
		lines = append(lines, "- No new emails matched the digest window.")
	} else {
		lines = append(lines, fmt.Sprintf("- Reviewed %d recent emails from the last %d hours.", len(emails), inputs.WindowHours))
	}
	lines = append(lines, "", "Why This Matters")
	if len(insights) == 0 {
		lines = append(lines, "- No recent Nex insights were available; this digest is based on email activity alone.")
	} else {
		for _, insight := range insights {
			lines = append(lines, "- "+truncateText(insight.Content, 180))
		}
	}
	lines = append(lines, "", "What To Do Next")
	if len(emails) == 0 {
		lines = append(lines, "- No immediate action required from email alone.")
	} else {
		for _, email := range emails[:minInt(len(emails), 5)] {
			lines = append(lines, fmt.Sprintf("- Review %s from %s.", stringInput(email["subject"]), stringInput(email["sender"])))
		}
	}
	lines = append(lines, "", "Email Highlights")
	for _, email := range emails[:minInt(len(emails), 8)] {
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", stringInput(email["sender"]), stringInput(email["subject"]), stringInput(email["preview"])))
	}
	if askErr != nil {
		lines = append(lines, "", "Note", "- Nex summary fallback used: "+truncateText(askErr.Error(), 160))
	}
	return strings.Join(lines, "\n")
}

func defaultDigestQuery(windowHours int) string {
	if windowHours <= 0 {
		windowHours = 24
	}
	days := int(math.Ceil(float64(windowHours) / 24.0))
	if days <= 1 {
		return "newer_than:1d"
	}
	return fmt.Sprintf("newer_than:%dd", days)
}

func mustMarshalJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func truncateText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || len(s) <= limit {
		return s
	}
	if limit <= 3 {
		return s[:limit]
	}
	return s[:limit-3] + "..."
}

func stringInput(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprintf("%v", v)
	}
}

func intInput(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
		return n
	default:
		return 0
	}
}

func boolInput(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	}
	return false, false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
