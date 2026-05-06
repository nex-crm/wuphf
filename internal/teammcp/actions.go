package teammcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/calendar"
	"github.com/nex-crm/wuphf/internal/team"
)

// readOnlyActionVerbs are unambiguous information-read verbs. Matched as
// WHOLE TOKENS (splitting action_id on - _ . / space), never as substrings —
// substring matching is too permissive (e.g. "get" matches inside "budget",
// "find" inside "findone_and_update", "view" inside "review_delete"). The
// list is intentionally narrower than the operator might expect: ambiguous
// nouns like "status", "count", "view", "query", "find", "summary" appear in
// both read and write action names ("update_status", "post_summary",
// "findone_and_update") and are excluded so mutating actions can never be
// misclassified.
var readOnlyActionVerbs = map[string]struct{}{
	"search":    {},
	"list":      {},
	"read":      {},
	"get":       {},
	"fetch":     {},
	"browse":    {},
	"describe":  {},
	"show":      {},
	"lookup":    {},
	"summarize": {},
}

// mutatingActionVerbs are unambiguous state-changing verbs. If ANY of these
// appears as a whole token in the action_id, the action is never classified
// read-only — even if a read verb is also present. This guards against
// composite action names like "GMAIL_LIST_AND_DELETE" or "FIND_AND_UPDATE":
// a single read verb is not enough; a single mutating verb vetoes.
var mutatingActionVerbs = map[string]struct{}{
	"send": {}, "create": {}, "update": {}, "delete": {}, "post": {},
	"put": {}, "patch": {}, "remove": {}, "insert": {}, "write": {},
	"clear": {}, "reset": {}, "archive": {}, "star": {}, "unstar": {},
	"mark": {}, "publish": {}, "add": {}, "move": {}, "invite": {},
	"accept": {}, "reject": {}, "approve": {}, "cancel": {}, "refund": {},
	"charge": {}, "pay": {}, "enable": {}, "disable": {}, "revoke": {},
	"grant": {}, "set": {}, "draft": {}, "schedule": {}, "upload": {},
	"replace": {}, "transfer": {}, "merge": {}, "split": {},
}

// actionApprovalTimeout is how long handleTeamActionExecute will wait for a
// human decision on a pending approval request before giving up.
const actionApprovalTimeout = 30 * time.Minute

// actionApprovalPollInterval mirrors the human_interview tool cadence.
const actionApprovalPollInterval = 1500 * time.Millisecond

// actionIDSeparator reports whether r is an action_id token boundary.
func actionIDSeparator(r rune) bool {
	return r == '-' || r == '_' || r == '.' || r == '/' || r == ' '
}

// actionIsReadOnly reports whether an action_id is safe to run without human
// approval. A read-only action has at least one read verb AND no mutating
// verb appearing as a whole token.
func actionIsReadOnly(actionID string) bool {
	id := strings.ToLower(strings.TrimSpace(actionID))
	if id == "" {
		return false
	}
	tokens := strings.FieldsFunc(id, actionIDSeparator)
	hasRead := false
	for _, tok := range tokens {
		if _, ok := mutatingActionVerbs[tok]; ok {
			return false
		}
		if _, ok := readOnlyActionVerbs[tok]; ok {
			hasRead = true
		}
	}
	return hasRead
}

// requireTeamActionApproval gates mutating external-action calls behind a
// human approval request. Returns nil when the call may proceed, an error
// describing the rejection otherwise. The approval contract:
//
//  1. DryRun calls never gate — they only build the request, not send it.
//  2. WUPHF_UNSAFE=1 bypasses the gate. The --unsafe launch flag sets this.
//  3. Read-only action IDs (search/list/get/etc.) bypass the gate.
//  4. Otherwise a blocking "approval" request is created in the Requests
//     panel; the handler polls until the human answers. An "approve"/
//     "approve_with_note" choice returns nil. Any other choice (reject,
//     reject_with_steer, needs_more_info) returns an error. Timeout after
//     actionApprovalTimeout returns an error.
//
// The point: a prompt-injected agent cannot send email, write to a CRM, or
// post a Slack message without the human explicitly clicking approve.
func requireTeamActionApproval(ctx context.Context, slug, channel string, args TeamActionExecuteArgs) error {
	if args.DryRun {
		return nil
	}
	if os.Getenv("WUPHF_UNSAFE") == "1" {
		return nil
	}
	if actionIsReadOnly(args.ActionID) {
		return nil
	}

	spec := buildActionApprovalSpec(slug, channel, args)

	options, recommendedID := normalizeHumanRequestOptions("approval", "", nil)

	// Collapse retries onto a single approval. Without this dedupe key,
	// every agent loop reconnect or retry of the same external-action
	// call posts a fresh /requests entry, and the human ends up staring
	// at 100+ stacked "Approve gmail action" cards for the same intent.
	dedupeKey := actionApprovalDedupeKey(slug, args)

	var created struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/requests", map[string]any{
		"kind":           "approval",
		"channel":        channel,
		"from":           slug,
		"title":          spec.Title,
		"question":       spec.Question,
		"context":        spec.Context,
		"options":        options,
		"recommended_id": recommendedID,
		"blocking":       true,
		"required":       true,
		"dedupe_key":     dedupeKey,
	}, &created); err != nil {
		return fmt.Errorf("create approval request: %w", err)
	}
	if strings.TrimSpace(created.ID) == "" {
		return fmt.Errorf("approval request did not return an ID")
	}

	timeout := time.After(actionApprovalTimeout)
	ticker := time.NewTicker(actionApprovalPollInterval)
	defer ticker.Stop()

	platform := strings.TrimSpace(args.Platform)
	if platform == "" {
		platform = "unknown"
	}
	actionID := strings.TrimSpace(args.ActionID)
	if actionID == "" {
		actionID = "unknown"
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timed out waiting for human approval of %s on %s", actionID, platform)
		case <-ticker.C:
			var result brokerInterviewAnswerResponse
			path := "/interview/answer?id=" + url.QueryEscape(created.ID)
			if err := brokerGetJSON(ctx, path, &result); err != nil {
				return fmt.Errorf("poll approval: %w", err)
			}
			switch strings.ToLower(strings.TrimSpace(result.Status)) {
			case "canceled", "cancelled":
				return fmt.Errorf("human approval canceled for %s on %s", actionID, platform)
			case "not_found":
				return fmt.Errorf("human approval request not found for %s on %s", actionID, platform)
			}
			if result.Answered == nil {
				continue
			}
			choice := strings.ToLower(strings.TrimSpace(result.Answered.ChoiceID))
			switch choice {
			case "approve", "approve_with_note", "confirm_proceed":
				return nil
			}
			reason := strings.TrimSpace(result.Answered.CustomText)
			if reason == "" {
				reason = strings.TrimSpace(result.Answered.ChoiceText)
			}
			if reason == "" {
				reason = choice
			}
			return fmt.Errorf("human rejected %s on %s: %s", actionID, platform, reason)
		}
	}
}

// actionApprovalDedupeKey collapses retries of the same external-action
// call onto one approval request. Keyed on agent + platform + action_id +
// connection so an in-flight retry by the agent loop folds onto the
// existing pending approval instead of stacking duplicates. Pure for
// testability — the broker dedupes on whatever string this function
// returns.
func actionApprovalDedupeKey(slug string, args TeamActionExecuteArgs) string {
	return fmt.Sprintf("action:%s:%s:%s:%s",
		strings.ToLower(strings.TrimSpace(slug)),
		strings.ToLower(strings.TrimSpace(args.Platform)),
		strings.ToLower(strings.TrimSpace(args.ActionID)),
		strings.ToLower(strings.TrimSpace(args.ConnectionKey)),
	)
}

// actionApprovalSpec is the structured payload of an external-action
// approval card. Split out from requireTeamActionApproval so the body
// composition can be unit-tested without a live broker. Before this
// existed, the approval card said only "Approve gmail action:
// GMAIL_SEND_EMAIL" with a context blob of internal jargon — the human
// had no way to see who the email was going to or what was inside it.
type actionApprovalSpec struct {
	Title    string
	Question string
	Context  string
}

// buildActionApprovalSpec composes the title, question, and context the
// human sees in the approval card. The shape:
//
//	Title:    "Send Email via Gmail"
//	Question: "@growthops wants to send email via Gmail. Approve?"
//	Context:  Why: <agent summary, if provided>
//	          What this will do:
//	          • To: alex@nex.ai
//	          • Subject: Welcome
//	          • Body: Hi Alex, welcome to ...
//	          Action: GMAIL_SEND_EMAIL via Gmail
//	          Account: <connection_key>
//	          Channel: #general
//
// The "What this will do" block is only included when at least one
// recognized payload field exists. The human can refuse without leaving
// the card because every decision-relevant field appears here.
func buildActionApprovalSpec(slug, channel string, args TeamActionExecuteArgs) actionApprovalSpec {
	platform := strings.TrimSpace(args.Platform)
	if platform == "" {
		platform = "unknown"
	}
	actionID := strings.TrimSpace(args.ActionID)
	if actionID == "" {
		actionID = "unknown"
	}

	verb := actionVerbLabel(platform, actionID)
	platformLabel := platformDisplay(platform)
	title := titleCaser.String(verb) + " via " + platformLabel
	question := fmt.Sprintf("@%s wants to %s via %s. Approve?", slug, verb, platformLabel)

	// Agent-controlled fields are sanitized before injection so a malicious
	// payload cannot forge structural sections in the rendered context.
	// Without this the parser's first-match-wins regexes would let the agent
	// inject a fake "What this will do" block + footer, displaying one
	// action while the broker executes a different one — a confused-deputy
	// approval bypass that defeats the entire reason this gate exists.
	safeActionID := sanitizeContextValue(actionID)
	safeSummary := sanitizeContextValue(strings.TrimSpace(args.Summary))
	safeConnection := sanitizeContextValue(strings.TrimSpace(args.ConnectionKey))

	var b strings.Builder
	if safeSummary != "" {
		b.WriteString("Why: ")
		b.WriteString(safeSummary)
		b.WriteString("\n\n")
	}
	if details := summarizeActionPayload(args); details != "" {
		b.WriteString("What this will do:\n")
		b.WriteString(details)
		b.WriteString("\n\n")
	}
	b.WriteString("Action: ")
	b.WriteString(safeActionID)
	b.WriteString(" via ")
	b.WriteString(platformLabel)
	if safeConnection != "" {
		b.WriteString("\nAccount: ")
		b.WriteString(safeConnection)
	}
	if ch := strings.TrimSpace(channel); ch != "" {
		b.WriteString("\nChannel: #")
		b.WriteString(ch)
	}

	return actionApprovalSpec{
		Title:    title,
		Question: question,
		Context:  strings.TrimRight(b.String(), "\n"),
	}
}

// sanitizeContextValue collapses any control character or structural
// delimiter the approval-card parser keys off of into safe inline text.
// Specifically: every newline variant becomes a space (so a forged
// "Action:" embedded in agent input cannot land at a line start, where
// the parser's `^Action:\s+` regex would match it), the bullet glyph
// becomes a middle dot (so a forged `• Label: value` cannot pose as a
// row inside the "What this will do" block), and runs of whitespace
// collapse to single spaces. Output stays as a single visible line, so
// when an agent tries to forge structure the human sees one long
// rambling sentence instead of authoritative-looking sections — a
// secondary visual signal that something is off.
func sanitizeContextValue(s string) string {
	if s == "" {
		return s
	}
	r := strings.NewReplacer(
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
		" ", " ",
		" ", " ",
		"•", "·", // U+2022 BULLET → U+00B7 MIDDLE DOT
	)
	cleaned := r.Replace(s)
	return strings.Join(strings.Fields(cleaned), " ")
}

// actionVerbLabel turns "GMAIL_SEND_EMAIL" into the lowercased verb phrase
// "send email" so it can be slotted into both a title-cased title
// ("Send Email via Gmail") and a sentence-cased question ("...wants to
// send email via Gmail"). The platform prefix is stripped when present so
// the verb does not awkwardly repeat the platform name.
func actionVerbLabel(platform, actionID string) string {
	id := strings.ToLower(strings.TrimSpace(actionID))
	if id == "" {
		return "run an action"
	}
	tokens := strings.FieldsFunc(id, actionIDSeparator)
	if len(tokens) > 1 {
		first := tokens[0]
		drop := false
		if first == strings.ToLower(strings.TrimSpace(platform)) {
			drop = true
		} else {
			switch first {
			case "gmail", "hubspot", "slack", "slackbot", "googlecalendar",
				"calendar", "stripe", "linear", "notion", "github",
				"googledrive", "drive", "docs", "sheets", "slides",
				"intercom", "zendesk", "salesforce", "asana", "trello",
				"airtable", "discord", "twitter", "x":
				drop = true
			}
		}
		if drop {
			tokens = tokens[1:]
		}
	}
	if len(tokens) == 0 {
		return "run an action"
	}
	return strings.Join(tokens, " ")
}

// platformDisplay turns a kebab-cased provider slug into its human form.
// "google-calendar" → "Google Calendar"; "hubspot" → "HubSpot".
func platformDisplay(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return "Unknown"
	}
	parts := strings.FieldsFunc(strings.ReplaceAll(platform, "_", "-"),
		func(r rune) bool { return r == '-' })
	if len(parts) == 0 {
		return "Unknown"
	}
	for i, p := range parts {
		switch p {
		case "hubspot":
			parts[i] = "HubSpot"
		case "github":
			parts[i] = "GitHub"
		case "slackbot":
			parts[i] = "Slack"
		case "googlecalendar":
			parts[i] = "Google Calendar"
		case "googledrive":
			parts[i] = "Google Drive"
		default:
			parts[i] = titleCaser.String(p)
		}
	}
	return strings.Join(parts, " ")
}

// payloadFieldOrder is the priority list of payload keys we surface in
// the approval card, top to bottom. Each entry maps a payload key (any
// of the synonyms ship together) to the label the human sees. The first
// match per label wins so synonyms like to/recipient/recipients do not
// double up.
var payloadFieldOrder = []struct {
	Keys  []string
	Label string
}{
	{[]string{"to", "recipient", "recipients", "recipient_email", "user_id"}, "To"},
	{[]string{"cc"}, "CC"},
	{[]string{"bcc"}, "BCC"},
	{[]string{"from", "sender"}, "From"},
	{[]string{"subject"}, "Subject"},
	{[]string{"channel", "channel_id"}, "Channel"},
	{[]string{"thread_ts", "thread_id"}, "Thread"},
	{[]string{"text", "message", "body", "content", "html_body"}, "Body"},
	{[]string{"title", "name"}, "Title"},
	{[]string{"summary", "description"}, "Description"},
	{[]string{"url", "link"}, "URL"},
	{[]string{"event_id", "calendar_event_id"}, "Event"},
	{[]string{"start_time", "start"}, "Starts"},
	{[]string{"end_time", "end"}, "Ends"},
	{[]string{"amount", "price"}, "Amount"},
	{[]string{"currency"}, "Currency"},
	{[]string{"query", "q"}, "Query"},
}

// payloadRedactedKeys are field names we never surface in the approval
// card — the human does not need to see their own credentials to decide
// whether to approve, and a leaky log is one OS clipboard away from a
// support ticket.
var payloadRedactedKeys = map[string]struct{}{
	"password":      {},
	"passwd":        {},
	"secret":        {},
	"api_key":       {},
	"access_token":  {},
	"refresh_token": {},
	"token":         {},
	"client_secret": {},
	"private_key":   {},
}

// summarizeActionPayload renders a bulleted list of decision-relevant
// payload fields from args.Data, args.PathVariables, and
// args.QueryParameters. Long values are clipped, multi-line bodies are
// flattened, and redacted keys are skipped entirely. Returns an empty
// string when none of the recognized fields are present.
func summarizeActionPayload(args TeamActionExecuteArgs) string {
	type field struct {
		Label string
		Value string
	}
	var fields []field
	seen := make(map[string]bool, len(payloadFieldOrder))

	sources := []map[string]any{args.Data, args.PathVariables, args.QueryParameters}
	for _, entry := range payloadFieldOrder {
		if seen[entry.Label] {
			continue
		}
		for _, key := range entry.Keys {
			if _, redacted := payloadRedactedKeys[key]; redacted {
				continue
			}
			value, ok := lookupPayloadValue(sources, key)
			if !ok {
				continue
			}
			rendered := formatPayloadValue(value)
			if rendered == "" {
				continue
			}
			fields = append(fields, field{Label: entry.Label, Value: rendered})
			seen[entry.Label] = true
			break
		}
	}

	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	for _, f := range fields {
		b.WriteString("• ")
		b.WriteString(f.Label)
		b.WriteString(": ")
		b.WriteString(f.Value)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// lookupPayloadValue checks each source map for the given key and
// returns the first hit. Comparison is case-insensitive on the key so
// providers that ship "Subject" or "TO" still match.
func lookupPayloadValue(sources []map[string]any, key string) (any, bool) {
	lowered := strings.ToLower(key)
	for _, src := range sources {
		if src == nil {
			continue
		}
		if v, ok := src[key]; ok {
			return v, true
		}
		for k, v := range src {
			if strings.ToLower(k) == lowered {
				return v, true
			}
		}
	}
	return nil, false
}

// payloadValueClipLen is the soft cap on a single payload value we render
// in the approval card, measured in RUNES (not bytes) so multi-byte
// characters like CJK or emoji do not get sliced mid-codepoint into
// invalid UTF-8. Email bodies routinely run thousands of bytes; the
// human only needs the first sentence to recognize the message.
const payloadValueClipLen = 240

// formatPayloadValue renders any payload value as a single, clipped
// string. Arrays become comma-separated lists; structured values fall
// back to JSON. Internal whitespace is collapsed so multi-line bodies
// do not break the bulleted list. Truncation is rune-aware: a CJK
// character at position 240 will not produce a half-glyph and a tofu
// box on the rendered card.
func formatPayloadValue(v any) string {
	raw := payloadValueString(v)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(raw) > payloadValueClipLen {
		runes := []rune(raw)
		raw = string(runes[:payloadValueClipLen]) + "…"
	}
	return raw
}

func payloadValueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []string:
		return strings.Join(t, ", ")
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s := payloadValueString(item); strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, ", ")
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", t), "0"), ".")
	case int, int64:
		return fmt.Sprintf("%d", t)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(raw)
}

var (
	externalActionProvider action.Provider
	titleCaser             = cases.Title(language.English)
)

type TeamActionGuideArgs struct {
	Topic string `json:"topic,omitempty" jsonschema:"One of: overview, actions, flows, relay, all. Defaults to all."`
}

type TeamActionConnectionsArgs struct {
	Search string `json:"search,omitempty" jsonschema:"Optional platform search query like gmail or hub-spot"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum connections to return"`
}

type TeamActionSearchArgs struct {
	Platform string `json:"platform" jsonschema:"Kebab-case platform name like gmail, slack, hub-spot, google-calendar"`
	Query    string `json:"query" jsonschema:"Natural-language action search like send email or create contact"`
	Mode     string `json:"mode,omitempty" jsonschema:"One of: execute or knowledge. Defaults to execute when the intent is to actually do something."`
}

type TeamActionKnowledgeArgs struct {
	Platform string `json:"platform" jsonschema:"Kebab-case platform name"`
	ActionID string `json:"action_id" jsonschema:"Action ID returned by team_action_search"`
}

type TeamActionExecuteArgs struct {
	Platform        string         `json:"platform" jsonschema:"Kebab-case platform name"`
	ActionID        string         `json:"action_id" jsonschema:"Action ID returned by team_action_search"`
	ConnectionKey   string         `json:"connection_key,omitempty" jsonschema:"Optional connection key from team_action_connections. Leave blank when the current provider can auto-resolve a single connected account for the platform."`
	Data            map[string]any `json:"data,omitempty" jsonschema:"Request body as a JSON object"`
	PathVariables   map[string]any `json:"path_variables,omitempty" jsonschema:"Path variables as a JSON object"`
	QueryParameters map[string]any `json:"query_parameters,omitempty" jsonschema:"Query parameters as a JSON object"`
	Headers         map[string]any `json:"headers,omitempty" jsonschema:"Extra headers as a JSON object"`
	FormData        bool           `json:"form_data,omitempty" jsonschema:"Send as multipart/form-data"`
	FormURLEncoded  bool           `json:"form_url_encoded,omitempty" jsonschema:"Send as application/x-www-form-urlencoded"`
	DryRun          bool           `json:"dry_run,omitempty" jsonschema:"Build the request without sending it"`
	Channel         string         `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug          string         `json:"my_slug,omitempty" jsonschema:"Agent slug performing the action. Defaults to WUPHF_AGENT_SLUG."`
	Summary         string         `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionWorkflowCreateArgs struct {
	Key              string   `json:"key" jsonschema:"Stable workflow key like daily-digest or escalate-renewal-risk"`
	DefinitionJSON   string   `json:"definition_json" jsonschema:"Full WUPHF workflow JSON definition as a string"`
	Channel          string   `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug           string   `json:"my_slug,omitempty" jsonschema:"Agent slug creating the workflow. Defaults to WUPHF_AGENT_SLUG."`
	Summary          string   `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
	SkillName        string   `json:"skill_name,omitempty" jsonschema:"Optional WUPHF skill name. Defaults to the workflow key."`
	SkillTitle       string   `json:"skill_title,omitempty" jsonschema:"Optional skill title shown in the Skills app."`
	SkillDescription string   `json:"skill_description,omitempty" jsonschema:"Optional skill description shown in the Skills app."`
	SkillTags        []string `json:"skill_tags,omitempty" jsonschema:"Optional skill tags"`
	SkillTrigger     string   `json:"skill_trigger,omitempty" jsonschema:"Optional trigger text that explains when the workflow should run"`
}

type TeamActionWorkflowExecuteArgs struct {
	KeyOrPath string         `json:"key_or_path" jsonschema:"Workflow key or path"`
	Inputs    map[string]any `json:"inputs,omitempty" jsonschema:"Workflow inputs as a JSON object"`
	DryRun    bool           `json:"dry_run,omitempty" jsonschema:"Run in dry-run mode"`
	Verbose   bool           `json:"verbose,omitempty" jsonschema:"Emit verbose workflow events"`
	Mock      bool           `json:"mock,omitempty" jsonschema:"Mock external steps where supported"`
	AllowBash bool           `json:"allow_bash,omitempty" jsonschema:"Allow bash/code steps in the workflow"`
	Channel   string         `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug    string         `json:"my_slug,omitempty" jsonschema:"Agent slug executing the workflow. Defaults to WUPHF_AGENT_SLUG."`
	Summary   string         `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionWorkflowScheduleArgs struct {
	Key        string         `json:"key" jsonschema:"Saved workflow key to run on a schedule"`
	Schedule   string         `json:"schedule" jsonschema:"Cron expression or shorthand like daily, hourly, 4h, or 0 9 * * 1-5"`
	RunNow     bool           `json:"run_now,omitempty" jsonschema:"Also execute one immediate run after scheduling when the human asked for a manual test run now"`
	Inputs     map[string]any `json:"inputs,omitempty" jsonschema:"Optional workflow inputs"`
	Channel    string         `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug     string         `json:"my_slug,omitempty" jsonschema:"Agent slug scheduling the workflow. Defaults to WUPHF_AGENT_SLUG."`
	Summary    string         `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
	SkillName  string         `json:"skill_name,omitempty" jsonschema:"Optional existing or new WUPHF skill name to mirror this workflow"`
	SkillTitle string         `json:"skill_title,omitempty" jsonschema:"Optional skill title when creating or updating the mirrored skill"`
}

type TeamActionRelaysArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum relays to return"`
	Page  int `json:"page,omitempty" jsonschema:"Page number"`
}

type TeamActionRelayEventTypesArgs struct {
	Platform string `json:"platform" jsonschema:"Kebab-case platform name like gmail, stripe, google-calendar"`
}

type TeamActionRelayCreateArgs struct {
	ConnectionKey string   `json:"connection_key" jsonschema:"Connection key from team_action_connections"`
	Description   string   `json:"description,omitempty" jsonschema:"Short description of what the relay is for"`
	EventFilters  []string `json:"event_filters,omitempty" jsonschema:"Optional list of event types to include"`
	CreateWebhook bool     `json:"create_webhook,omitempty" jsonschema:"Whether One should create the webhook endpoint on the source platform where supported"`
	Channel       string   `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug        string   `json:"my_slug,omitempty" jsonschema:"Agent slug creating the relay. Defaults to WUPHF_AGENT_SLUG."`
	Summary       string   `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionRelayActivateArgs struct {
	ID            string `json:"id" jsonschema:"Relay endpoint ID"`
	ActionsJSON   string `json:"actions_json" jsonschema:"JSON array of relay forwarding actions"`
	WebhookSecret string `json:"webhook_secret,omitempty" jsonschema:"Optional webhook secret"`
	Channel       string `json:"channel,omitempty" jsonschema:"Optional office channel for logging"`
	MySlug        string `json:"my_slug,omitempty" jsonschema:"Agent slug activating the relay. Defaults to WUPHF_AGENT_SLUG."`
	Summary       string `json:"summary,omitempty" jsonschema:"Optional short office log summary"`
}

type TeamActionRelayEventsArgs struct {
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum events to return"`
	Page      int    `json:"page,omitempty" jsonschema:"Page number"`
	Platform  string `json:"platform,omitempty" jsonschema:"Optional platform filter"`
	EventType string `json:"event_type,omitempty" jsonschema:"Optional event type filter"`
	After     string `json:"after,omitempty" jsonschema:"Optional cursor/time filter supported by One"`
	Before    string `json:"before,omitempty" jsonschema:"Optional cursor/time filter supported by One"`
}

type TeamActionRelayEventArgs struct {
	ID string `json:"id" jsonschema:"Relay event ID"`
}

func registerActionTools(server *mcp.Server) {
	mcp.AddTool(server, readOnlyTool(
		"team_action_guide",
		"Read the current external action provider guide in machine-readable form before building or wiring external actions.",
	), handleTeamActionGuide)
	mcp.AddTool(server, readOnlyTool(
		"team_action_connections",
		"List connected external accounts and connection keys available through the current action provider.",
	), handleTeamActionConnections)
	mcp.AddTool(server, readOnlyTool(
		"team_action_search",
		"Search for external actions on a platform using natural language. Use this before knowledge or execute.",
	), handleTeamActionSearch)
	mcp.AddTool(server, readOnlyTool(
		"team_action_knowledge",
		"Load the schema and usage guidance for an external action. Always do this before executing or wiring the action.",
	), handleTeamActionKnowledge)
	mcp.AddTool(server, officeWriteTool(
		"team_action_execute",
		"Execute an external action through the selected provider. Use dry_run first for risky writes.",
	), handleTeamActionExecute)
	mcp.AddTool(server, officeWriteTool(
		"team_action_workflow_create",
		"Save a reusable external workflow from a full WUPHF workflow JSON definition.",
	), handleTeamActionWorkflowCreate)
	mcp.AddTool(server, officeWriteTool(
		"team_action_workflow_execute",
		"Execute a saved external workflow by key or path.",
	), handleTeamActionWorkflowExecute)
	mcp.AddTool(server, officeWriteTool(
		"team_action_workflow_schedule",
		"Schedule a saved external workflow on a WUPHF-native cadence so it shows up in Calendar and runs through the office scheduler. Set run_now when the human also asked for an immediate first run.",
	), handleTeamActionWorkflowSchedule)
	mcp.AddTool(server, readOnlyTool(
		"team_action_relays",
		"List registered external triggers or relay endpoints for the selected provider.",
	), handleTeamActionRelays)
	mcp.AddTool(server, readOnlyTool(
		"team_action_relay_event_types",
		"List supported event types for a platform before creating a trigger or relay.",
	), handleTeamActionRelayEventTypes)
	mcp.AddTool(server, officeWriteTool(
		"team_action_relay_create",
		"Create an external trigger or relay for receiving events from a connected platform.",
	), handleTeamActionRelayCreate)
	mcp.AddTool(server, officeWriteTool(
		"team_action_relay_activate",
		"Enable or activate a previously registered external trigger or relay.",
	), handleTeamActionRelayActivate)
	mcp.AddTool(server, readOnlyTool(
		"team_action_relay_events",
		"List recent One relay events so the office can inspect or poll them.",
	), handleTeamActionRelayEvents)
	mcp.AddTool(server, readOnlyTool(
		"team_action_relay_event",
		"Fetch the full payload for one specific relay event.",
	), handleTeamActionRelayEvent)
}

func handleTeamActionGuide(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionGuideArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityGuide)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.Guide(ctx, args.Topic)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyJSON(result.Raw)), nil, nil
}

func handleTeamActionConnections(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionConnectionsArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityConnections)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ListConnections(ctx, action.ListConnectionsOptions{Search: args.Search, Limit: args.Limit})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionSearch(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionSearchArgs) (*mcp.CallToolResult, any, error) {
	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "execute"
	}
	provider, err := selectedActionProvider(action.CapabilityActionSearch)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.SearchActions(ctx, args.Platform, args.Query, mode)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionKnowledge(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionKnowledgeArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityActionKnowledge)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ActionKnowledge(ctx, args.Platform, args.ActionID)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionExecute(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionExecuteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)

	// Human-in-the-loop gate. Mutating external actions — sending email,
	// posting to Slack, writing a CRM row, etc. — require explicit human
	// approval unless --unsafe was passed or the action is a read-only
	// lookup. A prompt-injected agent must not be able to trigger real
	// side-effects silently.
	if err := requireTeamActionApproval(ctx, slug, channel, args); err != nil {
		return toolError(err), nil, nil
	}

	provider, err := selectedActionProvider(action.CapabilityActionExecute)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ExecuteAction(ctx, action.ExecuteRequest{
		Platform:        args.Platform,
		ActionID:        args.ActionID,
		ConnectionKey:   args.ConnectionKey,
		Data:            args.Data,
		PathVariables:   args.PathVariables,
		QueryParameters: args.QueryParameters,
		Headers:         args.Headers,
		FormData:        args.FormData,
		FormURLEncoded:  args.FormURLEncoded,
		DryRun:          args.DryRun,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_action_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("%s action %s on %s failed", titleCaser.String(provider.Name()), args.ActionID, args.Platform)), args.ActionID)
		return toolError(err), nil, nil
	}
	kind := "external_action_executed"
	summary := fallbackSummary(args.Summary, fmt.Sprintf("Executed %s on %s via %s", args.ActionID, args.Platform, titleCaser.String(provider.Name())))
	if args.DryRun {
		kind = "external_action_planned"
		summary = fallbackSummary(args.Summary, fmt.Sprintf("Planned %s on %s via %s", args.ActionID, args.Platform, titleCaser.String(provider.Name())))
	}
	_ = brokerRecordAction(ctx, kind, provider.Name(), channel, slug, summary, args.ActionID)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionWorkflowCreate(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionWorkflowCreateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	definition := json.RawMessage(strings.TrimSpace(args.DefinitionJSON))
	if !json.Valid(definition) {
		return toolError(fmt.Errorf("definition_json must be valid JSON")), nil, nil
	}
	provider, err := selectedActionProvider(action.CapabilityWorkflowCreate)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.CreateWorkflow(ctx, action.WorkflowCreateRequest{
		Key:        args.Key,
		Definition: definition,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Creating workflow %s via %s failed", args.Key, titleCaser.String(provider.Name()))), args.Key)
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(result.Key) == "" {
		result.Key = strings.TrimSpace(args.Key)
	}
	if err := upsertWorkflowSkill(ctx, workflowSkillSpec{
		Name:             fallbackString(args.SkillName, result.Key),
		Title:            fallbackString(args.SkillTitle, humanizeWorkflowKey(result.Key)),
		Description:      fallbackString(args.SkillDescription, fmt.Sprintf("Reusable %s workflow for %s.", titleCaser.String(provider.Name()), humanizeWorkflowKey(result.Key))),
		Tags:             append([]string{provider.Name(), "workflow"}, args.SkillTags...),
		Trigger:          strings.TrimSpace(args.SkillTrigger),
		WorkflowProvider: provider.Name(),
		WorkflowKey:      result.Key,
		WorkflowDef:      strings.TrimSpace(args.DefinitionJSON),
		Channel:          channel,
		CreatedBy:        slug,
	}); err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fmt.Sprintf("Created workflow %s via %s, but failed to mirror it into Skills", result.Key, titleCaser.String(provider.Name())), result.Key)
		return toolError(err), nil, nil
	}
	_ = brokerRecordAction(ctx, "external_workflow_created", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Created workflow %s via %s", result.Key, titleCaser.String(provider.Name()))), result.Key)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionWorkflowExecute(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionWorkflowExecuteArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	provider, err := selectedActionProvider(action.CapabilityWorkflowExecute)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ExecuteWorkflow(ctx, action.WorkflowExecuteRequest{
		KeyOrPath: args.KeyOrPath,
		Inputs:    args.Inputs,
		DryRun:    args.DryRun,
		Verbose:   args.Verbose,
		Mock:      args.Mock,
		AllowBash: args.AllowBash,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Workflow %s via %s failed", args.KeyOrPath, titleCaser.String(provider.Name()))), args.KeyOrPath)
		return toolError(err), nil, nil
	}
	kind := "external_workflow_executed"
	summary := fallbackSummary(args.Summary, fmt.Sprintf("Executed workflow %s via %s", args.KeyOrPath, titleCaser.String(provider.Name())))
	if args.DryRun {
		kind = "external_workflow_planned"
		summary = fallbackSummary(args.Summary, fmt.Sprintf("Planned workflow %s via %s", args.KeyOrPath, titleCaser.String(provider.Name())))
	}
	_ = brokerRecordAction(ctx, kind, provider.Name(), channel, slug, summary, args.KeyOrPath)
	_ = touchWorkflowSkill(ctx, args.KeyOrPath, result.Status, time.Now().UTC())
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionWorkflowSchedule(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionWorkflowScheduleArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	provider, err := selectedActionProvider(action.CapabilityWorkflowExecute)
	if err != nil {
		return toolError(err), nil, nil
	}
	if strings.TrimSpace(args.Key) == "" {
		return toolError(fmt.Errorf("key is required")), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	sched, err := calendar.ParseCron(args.Schedule)
	if err != nil {
		return toolError(fmt.Errorf("invalid schedule %q: %w", args.Schedule, err)), nil, nil
	}
	nextRun := sched.Next(time.Now().UTC())
	if nextRun.IsZero() {
		return toolError(fmt.Errorf("could not compute next run for %q", args.Schedule)), nil, nil
	}
	payload, err := json.Marshal(map[string]any{
		"provider":      provider.Name(),
		"workflow_key":  strings.TrimSpace(args.Key),
		"inputs":        args.Inputs,
		"schedule_expr": strings.TrimSpace(args.Schedule),
		"created_by":    slug,
		"channel":       channel,
		"skill_name":    strings.TrimSpace(args.SkillName),
	})
	if err != nil {
		return toolError(err), nil, nil
	}
	job := map[string]any{
		"slug":          schedulerSlug(provider.Name(), channel, args.Key),
		"kind":          provider.Name() + "_workflow",
		"label":         "Run " + humanizeWorkflowKey(args.Key),
		"target_type":   "workflow",
		"target_id":     strings.TrimSpace(args.Key),
		"channel":       channel,
		"provider":      provider.Name(),
		"workflow_key":  strings.TrimSpace(args.Key),
		"skill_name":    strings.TrimSpace(args.SkillName),
		"schedule_expr": strings.TrimSpace(args.Schedule),
		"due_at":        nextRun.UTC().Format(time.RFC3339),
		"next_run":      nextRun.UTC().Format(time.RFC3339),
		"status":        "scheduled",
		"payload":       string(payload),
	}
	if err := brokerPostJSON(ctx, "/scheduler", job, nil); err != nil {
		_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fmt.Sprintf("Failed to schedule workflow %s via %s", args.Key, titleCaser.String(provider.Name())), args.Key)
		return toolError(err), nil, nil
	}
	skillName := strings.TrimSpace(args.SkillName)
	if skillName == "" {
		skillName = strings.TrimSpace(args.Key)
	}
	_ = upsertWorkflowSkill(ctx, workflowSkillSpec{
		Name:             skillName,
		Title:            fallbackString(args.SkillTitle, humanizeWorkflowKey(args.Key)),
		Description:      fmt.Sprintf("Reusable %s workflow for %s.", titleCaser.String(provider.Name()), humanizeWorkflowKey(args.Key)),
		Tags:             []string{provider.Name(), "workflow", "scheduled"},
		WorkflowProvider: provider.Name(),
		WorkflowKey:      strings.TrimSpace(args.Key),
		WorkflowSchedule: strings.TrimSpace(args.Schedule),
		Channel:          channel,
		CreatedBy:        slug,
	})
	_ = brokerRecordAction(ctx, "external_workflow_scheduled", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Scheduled workflow %s via %s (%s)", args.Key, titleCaser.String(provider.Name()), args.Schedule)), args.Key)
	result := map[string]any{
		"ok":           true,
		"workflow_key": strings.TrimSpace(args.Key),
		"schedule":     strings.TrimSpace(args.Schedule),
		"next_run":     nextRun.UTC().Format(time.RFC3339),
		"skill_name":   skillName,
	}
	if args.RunNow {
		runResult, execErr := provider.ExecuteWorkflow(ctx, action.WorkflowExecuteRequest{
			KeyOrPath: strings.TrimSpace(args.Key),
			Inputs:    args.Inputs,
		})
		if execErr != nil {
			_ = brokerRecordAction(ctx, "external_workflow_failed", provider.Name(), channel, slug, fmt.Sprintf("Scheduled workflow %s via %s, but the immediate run failed", args.Key, titleCaser.String(provider.Name())), args.Key)
			result["run_now"] = map[string]any{
				"ok":    false,
				"error": execErr.Error(),
			}
			// The workflow is scheduled even though the immediate run
			// failed; surface the failure inside the result payload
			// (run_now.ok=false + error) rather than as a tool-call
			// error so the agent sees a structured response and can
			// decide whether to retry.
			return textResult(prettyObject(result)), nil, nil //nolint:nilerr // intentional: surface execErr inside result, schedule succeeded
		}
		_ = brokerRecordAction(ctx, "external_workflow_executed", provider.Name(), channel, slug, fmt.Sprintf("Scheduled workflow %s via %s and ran it once immediately", args.Key, titleCaser.String(provider.Name())), args.Key)
		_ = touchWorkflowSkill(ctx, args.Key, runResult.Status, time.Now().UTC())
		result["run_now"] = map[string]any{
			"ok":     true,
			"status": runResult.Status,
			"run_id": runResult.RunID,
		}
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelays(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelaysArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityRelayList)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ListRelays(ctx, action.ListRelaysOptions{Limit: args.Limit, Page: args.Page})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayEventTypes(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayEventTypesArgs) (*mcp.CallToolResult, any, error) {
	provider, err := selectedActionProvider(action.CapabilityRelayEventTypes)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.RelayEventTypes(ctx, args.Platform)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayCreate(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayCreateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	provider, err := selectedActionProvider(action.CapabilityRelayCreate)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.CreateRelay(ctx, action.RelayCreateRequest{
		ConnectionKey: args.ConnectionKey,
		Description:   args.Description,
		EventFilters:  args.EventFilters,
		CreateWebhook: args.CreateWebhook,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_trigger_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Creating trigger for %s via %s failed", args.ConnectionKey, titleCaser.String(provider.Name()))), args.ConnectionKey)
		return toolError(err), nil, nil
	}
	_ = brokerRecordAction(ctx, "external_trigger_registered", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Created trigger %s via %s", result.ID, titleCaser.String(provider.Name()))), result.ID)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayActivate(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayActivateArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	actions := json.RawMessage(strings.TrimSpace(args.ActionsJSON))
	if !json.Valid(actions) {
		return toolError(fmt.Errorf("actions_json must be valid JSON")), nil, nil
	}
	provider, err := selectedActionProvider(action.CapabilityRelayActivate)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ActivateRelay(ctx, action.RelayActivateRequest{
		ID:            args.ID,
		Actions:       actions,
		WebhookSecret: args.WebhookSecret,
	})
	if err != nil {
		_ = brokerRecordAction(ctx, "external_trigger_failed", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Activating trigger %s via %s failed", args.ID, titleCaser.String(provider.Name()))), args.ID)
		return toolError(err), nil, nil
	}
	_ = brokerRecordAction(ctx, "external_trigger_registered", provider.Name(), channel, slug, fallbackSummary(args.Summary, fmt.Sprintf("Activated trigger %s via %s", result.ID, titleCaser.String(provider.Name()))), result.ID)
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayEvents(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayEventsArgs) (*mcp.CallToolResult, any, error) {
	result, err := externalActionProvider.ListRelayEvents(ctx, action.RelayEventsOptions{
		Limit:     args.Limit,
		Page:      args.Page,
		Platform:  args.Platform,
		EventType: args.EventType,
		After:     args.After,
		Before:    args.Before,
	})
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func handleTeamActionRelayEvent(ctx context.Context, _ *mcp.CallToolRequest, args TeamActionRelayEventArgs) (*mcp.CallToolResult, any, error) {
	result, err := externalActionProvider.GetRelayEvent(ctx, args.ID)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(prettyObject(result)), nil, nil
}

func brokerRecordAction(ctx context.Context, kind, source, channel, actor, summary, relatedID string) error {
	return brokerPostJSON(ctx, "/actions", map[string]any{
		"kind":       strings.TrimSpace(kind),
		"source":     strings.TrimSpace(source),
		"channel":    resolveChannel(channel),
		"actor":      strings.TrimSpace(actor),
		"summary":    strings.TrimSpace(summary),
		"related_id": strings.TrimSpace(relatedID),
	}, nil)
}

type workflowSkillSpec struct {
	Name             string
	Title            string
	Description      string
	Tags             []string
	Trigger          string
	WorkflowProvider string
	WorkflowKey      string
	WorkflowDef      string
	WorkflowSchedule string
	RelayID          string
	RelayPlatform    string
	RelayEventTypes  []string
	Channel          string
	CreatedBy        string
}

func upsertWorkflowSkill(ctx context.Context, spec workflowSkillSpec) error {
	if strings.TrimSpace(spec.Name) == "" || strings.TrimSpace(spec.CreatedBy) == "" {
		return nil
	}
	payload := map[string]any{
		"action":                "create",
		"name":                  strings.TrimSpace(spec.Name),
		"title":                 strings.TrimSpace(spec.Title),
		"description":           strings.TrimSpace(spec.Description),
		"content":               workflowSkillContent(spec),
		"created_by":            strings.TrimSpace(spec.CreatedBy),
		"channel":               resolveChannel(spec.Channel),
		"tags":                  compactStrings(spec.Tags),
		"trigger":               strings.TrimSpace(spec.Trigger),
		"workflow_provider":     strings.TrimSpace(spec.WorkflowProvider),
		"workflow_key":          strings.TrimSpace(spec.WorkflowKey),
		"workflow_definition":   strings.TrimSpace(spec.WorkflowDef),
		"workflow_schedule":     strings.TrimSpace(spec.WorkflowSchedule),
		"relay_id":              strings.TrimSpace(spec.RelayID),
		"relay_platform":        strings.TrimSpace(spec.RelayPlatform),
		"relay_event_types":     compactStrings(spec.RelayEventTypes),
		"last_execution_status": "",
	}
	if err := brokerPostJSON(ctx, "/skills", payload, nil); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "409") {
		return err
	}
	return brokerPutJSON(ctx, "/skills", map[string]any{
		"name":                strings.TrimSpace(spec.Name),
		"title":               strings.TrimSpace(spec.Title),
		"description":         strings.TrimSpace(spec.Description),
		"content":             workflowSkillContent(spec),
		"channel":             resolveChannel(spec.Channel),
		"tags":                compactStrings(spec.Tags),
		"trigger":             strings.TrimSpace(spec.Trigger),
		"workflow_provider":   strings.TrimSpace(spec.WorkflowProvider),
		"workflow_key":        strings.TrimSpace(spec.WorkflowKey),
		"workflow_definition": strings.TrimSpace(spec.WorkflowDef),
		"workflow_schedule":   strings.TrimSpace(spec.WorkflowSchedule),
		"relay_id":            strings.TrimSpace(spec.RelayID),
		"relay_platform":      strings.TrimSpace(spec.RelayPlatform),
		"relay_event_types":   compactStrings(spec.RelayEventTypes),
	}, nil)
}

func touchWorkflowSkill(ctx context.Context, workflowKey, status string, when time.Time) error {
	key := strings.TrimSpace(workflowKey)
	if key == "" {
		return nil
	}
	return brokerPutJSON(ctx, "/skills", map[string]any{
		"name":                  key,
		"workflow_key":          key,
		"last_execution_at":     when.UTC().Format(time.RFC3339),
		"last_execution_status": strings.TrimSpace(status),
	}, nil)
}

func workflowSkillContent(spec workflowSkillSpec) string {
	label := titleCaser.String(fallbackString(spec.WorkflowProvider, "workflow"))
	lines := []string{
		fmt.Sprintf("WUPHF workflow skill (%s): %s", label, humanizeWorkflowKey(fallbackString(spec.WorkflowKey, spec.Name))),
		"Use team_action_workflow_execute to run it through WUPHF.",
	}
	if strings.TrimSpace(spec.WorkflowSchedule) != "" {
		lines = append(lines, "Schedule: "+strings.TrimSpace(spec.WorkflowSchedule))
	}
	if strings.TrimSpace(spec.Trigger) != "" {
		lines = append(lines, "Trigger: "+strings.TrimSpace(spec.Trigger))
	}
	if strings.TrimSpace(spec.RelayID) != "" {
		lines = append(lines, "Relay: "+strings.TrimSpace(spec.RelayID))
	}
	return strings.Join(lines, "\n")
}

func compactStrings(items []string) []string {
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func humanizeWorkflowKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "Workflow"
	}
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '-' || r == '_' || r == ':'
	})
	for i := range parts {
		parts[i] = titleCaser.String(parts[i])
	}
	return strings.Join(parts, " ")
}

func schedulerSlug(provider, channel, workflowKey string) string {
	channel = resolveChannel(channel)
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "workflow"
	}
	workflowKey = strings.ToLower(strings.TrimSpace(workflowKey))
	workflowKey = strings.ReplaceAll(workflowKey, " ", "-")
	return fmt.Sprintf("%s-workflow:%s:%s", provider, channel, workflowKey)
}

func fallbackSummary(explicit, fallback string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	return fallback
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func prettyObject(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}
	return string(raw)
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err == nil {
		return out.String()
	}
	return string(raw)
}

func selectedActionProvider(cap action.Capability) (action.Provider, error) {
	if externalActionProvider != nil {
		return externalActionProvider, nil
	}
	provider, err := team.ResolveActionProviderForCapability(cap)
	if err == nil {
		return provider, nil
	}
	caps := team.DetectRuntimeCapabilities()
	entry, ok := caps.Registry.Entry(team.RegistryKeyForActionCapability(cap))
	if !ok || strings.TrimSpace(entry.NextStep) == "" {
		return nil, err
	}
	return nil, fmt.Errorf("%w. Next: %s", err, entry.NextStep)
}
