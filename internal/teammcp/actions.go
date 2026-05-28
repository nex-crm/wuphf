package teammcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

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

// approvalContext is the metadata requireTeamActionApproval surfaces to the
// caller after a successful approval. The execute handler reads it to write
// a matching ApprovalAuditEntry to the broker once the action runs.
//
// RequestID is empty when the gate was bypassed (dry-run, unsafe, read-only)
// — the caller should treat an empty RequestID as "skip the audit write."
type approvalContext struct {
	RequestID   string
	IssueID     string
	RequestedAt string
	AnsweredAt  string
}

// requireTeamActionApproval gates mutating external-action calls behind a
// human approval request. Returns the approval context (request id + Issue
// id + timestamps) and a nil error when the call may proceed; returns a
// non-nil error describing the rejection otherwise. The approval contract:
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
// On non-approve terminal dispositions (reject/timeout/cancel) the function
// writes an ApprovalAuditEntry to the broker before returning the error so
// the inbox trail records the dead-end. The success path's audit write is
// the caller's responsibility because only the caller knows the executed_at
// timestamp and outcome chat message id.
//
// The point: a prompt-injected agent cannot send email, write to a CRM, or
// post a Slack message without the human explicitly clicking approve.
func requireTeamActionApproval(ctx context.Context, slug, channel string, args TeamActionExecuteArgs) (approvalContext, error) {
	if args.DryRun {
		return approvalContext{}, nil
	}
	if os.Getenv("WUPHF_UNSAFE") == "1" {
		return approvalContext{}, nil
	}
	if actionIsReadOnly(args.ActionID) {
		return approvalContext{}, nil
	}

	// Auto-resolve the Issue scoping this action. The product rule is
	// "any work getting done has an Issue behind it." Rather than rejecting
	// when the agent forgot to pass issue_id, the broker resolves the
	// container automatically: pick the most recent open Issue in this
	// channel, or auto-create a draft Issue from the action's intent.
	// The resolved id rides on the approval request body so audit-trail
	// work (Bug B Layer 2) can later correlate approval → Issue → outcome.
	resolvedIssueID, resolvedState, err := resolveActionIssue(ctx, slug, channel, args)
	if err != nil {
		// Even when auto-resolve fails (broker unreachable, etc.) we do
		// not block the approval — the human can still answer it. We
		// just lose the Issue link for this one card. Logged so it's
		// visible in the broker stderr.
		fmt.Fprintf(os.Stderr, "teammcp: action issue auto-resolve failed for %s/%s: %v\n", args.Platform, args.ActionID, err)
	} else {
		args.IssueID = resolvedIssueID
		// Hard gate: an Issue in `drafting` is awaiting human review +
		// approve. The agent must NOT proceed with external actions
		// until the human approves it via the Issue detail surface.
		// Surface a clear error back to the agent so its next-step
		// reasoning routes to "wait + ping the human" instead of
		// retrying the action. The agent will see this error in its
		// tool_result and is prompted (RULE ZERO) to back off.
		if strings.EqualFold(strings.TrimSpace(resolvedState), "drafting") {
			return approvalContext{}, fmt.Errorf(
				"issue %s is awaiting human approval (lifecycle_state=drafting). "+
					"Do NOT retry this action. Instead, surface the Issue to the human (it's already in chat as an Issue card and on the Issues board) and wait. "+
					"Resume work only after the human clicks Approve & Start on the Issue.",
				resolvedIssueID,
			)
		}
	}

	spec := buildActionApprovalSpec(slug, channel, args)

	options, recommendedID := normalizeHumanRequestOptions("approval", "", nil)

	// Collapse retries onto a single approval. Without this dedupe key,
	// every agent loop reconnect or retry of the same external-action
	// call posts a fresh /requests entry, and the human ends up staring
	// at 100+ stacked "Approve gmail action" cards for the same intent.
	dedupeKey := actionApprovalDedupeKey(slug, args)

	requestedAt := time.Now().UTC().Format(time.RFC3339Nano)
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
		// Carry the parent Issue id on the approval record so the audit
		// trail can later link approval → Issue → resulting tool call
		// (Bug B Layer 2). The broker ignores unknown fields today, so
		// shipping this is safe and unblocks the follow-up slice.
		"issue_id": strings.TrimSpace(args.IssueID),
	}, &created); err != nil {
		return approvalContext{}, fmt.Errorf("create approval request: %w", err)
	}
	if strings.TrimSpace(created.ID) == "" {
		return approvalContext{}, fmt.Errorf("approval request did not return an ID")
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

	auditBase := team.ApprovalAuditEntry{
		ApprovalRequestID: created.ID,
		TaskID:            strings.TrimSpace(args.IssueID),
		Platform:          platform,
		ActionID:          actionID,
		ConnectionKey:     strings.TrimSpace(args.ConnectionKey),
		RequestedAt:       requestedAt,
		Actor:             slug,
		Channel:           channel,
	}

	for {
		select {
		case <-ctx.Done():
			return approvalContext{}, ctx.Err()
		case <-timeout:
			audit := auditBase
			audit.Outcome = team.ApprovalOutcomeTimedOut
			audit.OutcomeSummary = fmt.Sprintf("timed out waiting for human approval after %s", actionApprovalTimeout)
			brokerPostApprovalAudit(ctx, audit)
			return approvalContext{}, fmt.Errorf("timed out waiting for human approval of %s on %s", actionID, platform)
		case <-ticker.C:
			var result brokerInterviewAnswerResponse
			path := "/interview/answer?id=" + url.QueryEscape(created.ID)
			if err := brokerGetJSON(ctx, path, &result); err != nil {
				return approvalContext{}, fmt.Errorf("poll approval: %w", err)
			}
			switch strings.ToLower(strings.TrimSpace(result.Status)) {
			case "canceled", "cancelled":
				audit := auditBase
				audit.Outcome = team.ApprovalOutcomeCancelled
				audit.OutcomeSummary = "human cancelled approval"
				brokerPostApprovalAudit(ctx, audit)
				return approvalContext{}, fmt.Errorf("human approval canceled for %s on %s", actionID, platform)
			case "not_found":
				return approvalContext{}, fmt.Errorf("human approval request not found for %s on %s", actionID, platform)
			}
			if result.Answered == nil {
				continue
			}
			answeredAt := strings.TrimSpace(result.Answered.AnsweredAt)
			if answeredAt == "" {
				answeredAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			choice := strings.ToLower(strings.TrimSpace(result.Answered.ChoiceID))
			switch choice {
			case "approve", "approve_with_note", "confirm_proceed":
				return approvalContext{
					RequestID:   created.ID,
					IssueID:     strings.TrimSpace(args.IssueID),
					RequestedAt: requestedAt,
					AnsweredAt:  answeredAt,
				}, nil
			}
			reason := strings.TrimSpace(result.Answered.CustomText)
			if reason == "" {
				reason = strings.TrimSpace(result.Answered.ChoiceText)
			}
			if reason == "" {
				reason = choice
			}
			audit := auditBase
			audit.Outcome = team.ApprovalOutcomeRejected
			audit.AnsweredAt = answeredAt
			audit.OutcomeSummary = reason
			brokerPostApprovalAudit(ctx, audit)
			return approvalContext{}, fmt.Errorf("human rejected %s on %s: %s", actionID, platform, reason)
		}
	}
}

// resolveActionIssue returns the Issue (team_task) id that scopes this
// action call. The resolution order:
//
//  1. If args.IssueID is set and exists in the broker, use it.
//  2. Otherwise, look at open Issues in this channel. If at least one
//     exists, return the most-recently-updated one. The action gets
//     attached to that Issue (the agent is presumed to be continuing
//     work on the active Issue).
//  3. Otherwise, auto-create a new draft Issue with a title derived
//     from the action verb + platform, and return its id.
//
// The product rule the user locked: "any work getting done has an Issue
// behind it." Rather than rejecting calls without issue_id and forcing
// the LLM to self-correct, the broker resolves the container itself.
// The agent never sees an error; the human always sees an Issue
// scoping the work.
func resolveActionIssue(ctx context.Context, slug, channel string, args TeamActionExecuteArgs) (string, string, error) {
	// Step 1: caller-supplied id (still preferred — agent knows the
	// scoping better than we do). Verify it exists; if not, fall through
	// to the find-or-create path rather than fail. Returns the resolved
	// id AND the Issue's current lifecycle_state so the caller can
	// enforce gates (e.g. block external actions while the Issue is
	// awaiting human approval in drafting).
	if id := strings.TrimSpace(args.IssueID); id != "" {
		var resp struct {
			Task *struct {
				ID             string `json:"id"`
				LifecycleState string `json:"lifecycle_state"`
			} `json:"task,omitempty"`
			LifecycleState string `json:"lifecycleState"`
		}
		if err := brokerGetJSON(ctx, "/tasks/"+url.PathEscape(id), &resp); err == nil &&
			resp.Task != nil && strings.TrimSpace(resp.Task.ID) != "" {
			state := strings.TrimSpace(resp.Task.LifecycleState)
			if state == "" {
				state = strings.TrimSpace(resp.LifecycleState)
			}
			return id, state, nil
		}
	}

	// Step 2: find the most recently updated open Issue in this channel.
	// "Open" = anything that isn't done / approved / rejected /
	// cancelled. Agents typically work on the latest active Issue, so
	// preferring the newest-updated is a good default.
	if ch := strings.TrimSpace(channel); ch != "" {
		var list struct {
			Tasks []struct {
				ID         string `json:"id"`
				Status     string `json:"status"`
				Lifecycle  string `json:"lifecycle_state"`
				UpdatedAt  string `json:"updated_at"`
				CreatedAt  string `json:"created_at"`
				ParentID   string `json:"parent_issue_id,omitempty"`
				TaskTypeID string `json:"task_type,omitempty"`
			} `json:"tasks"`
		}
		path := "/tasks?viewer_slug=" + url.QueryEscape(slug) + "&channel=" + url.QueryEscape(ch)
		if err := brokerGetJSON(ctx, path, &list); err == nil && len(list.Tasks) > 0 {
			var best string
			var bestState string
			var bestTS string
			for _, t := range list.Tasks {
				switch strings.ToLower(strings.TrimSpace(t.Status)) {
				case "done", "approved", "rejected", "cancelled", "canceled":
					continue
				}
				switch strings.ToLower(strings.TrimSpace(t.Lifecycle)) {
				case "approved", "rejected":
					continue
				}
				// Pick the row with the largest updated_at (RFC3339
				// strings compare correctly), falling back to
				// created_at when updated_at is empty.
				ts := t.UpdatedAt
				if ts == "" {
					ts = t.CreatedAt
				}
				if ts > bestTS {
					bestTS = ts
					best = t.ID
					bestState = strings.TrimSpace(t.Lifecycle)
				}
			}
			if best != "" {
				return best, bestState, nil
			}
		}
	}

	// Step 3: auto-create a draft Issue. Title is the human-readable
	// verb + platform from the action (the same one that goes on the
	// approval card). Description carries enough action context for the
	// human to recognise what work this is — without it the Issue card
	// would just say "Run an action via Gmail" with no further detail.
	verb := actionVerbLabel(args.Platform, args.ActionID)
	platformLabel := platformDisplay(args.Platform)
	title := titleCaser.String(verb) + " via " + platformLabel
	details := strings.TrimSpace(args.Summary)
	if details == "" {
		details = "Auto-created by the broker to scope an external action the agent kicked off without an explicit Issue. " +
			"Action: " + strings.TrimSpace(args.ActionID) + " via " + platformLabel + "."
	}
	// created_by must be a channel member, not the literal "broker"
	// string, because the broker's POST /task-plan ACL check rejects
	// non-member actors with 403. The calling agent is always a member
	// of the channel they're acting in, so attribute the auto-create to
	// them. The "auto-created by gate" intent shows up in `details` and
	// in the absence of an agent-authored description.
	createBody := map[string]any{
		"channel":    strings.TrimSpace(channel),
		"created_by": slug,
		"tasks": []map[string]any{
			{
				"title":          title,
				"assignee":       slug,
				"details":        details,
				"task_type":      "issue",
				"execution_mode": "office",
			},
		},
	}
	var created struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := brokerPostJSON(ctx, "/task-plan", createBody, &created); err != nil {
		return "", "", fmt.Errorf("auto-create issue: %w", err)
	}
	if len(created.Tasks) == 0 || strings.TrimSpace(created.Tasks[0].ID) == "" {
		return "", "", fmt.Errorf("auto-create issue returned no id")
	}
	// Auto-created Issues land in `drafting` (via MutateTask create-path
	// applyLifecycleStateLocked) so the human still controls the gate
	// before the agent proceeds.
	return strings.TrimSpace(created.Tasks[0].ID), "drafting", nil
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
	if account := actionApprovalAccountLabel(args.ConnectionKey); account != "" {
		b.WriteString("\nAccount: ")
		b.WriteString(account)
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
	// Defensive: if the actionID looks like an opaque internal identifier
	// (contains `::`, long hash-like runs, base64-ish padding, etc.), the
	// agent has passed a connection key or workflow handle instead of a
	// proper action_id. Title-casing that string verbatim would surface
	// gibberish like "conn mod def::gj3odoe fdw::ijlww5s" to the human and
	// leave them no way to decide whether to approve. Fall back to a
	// generic verb so the rest of the approval card (Why / What this will
	// do / Action) is still useful.
	if looksLikeOpaqueID(id) {
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

// looksLikeOpaqueID reports whether s smells like an internal identifier
// rather than a human-readable action verb. Heuristics:
//   - contains `::` (the connection-manager internal delimiter we saw in
//     the wild on May 26 — request-6/7 titles rendered raw hashes that way)
//   - any whitespace/separator-split token longer than 12 chars AND
//     containing both letters and digits (hash-shaped)
//   - any token with 4+ consecutive consonants (gibberish-shaped)
//
// The bar is intentionally high so we do not eat real verbs like
// "send_email" or "list_messages". When in doubt the caller falls back to
// "run an action" and the human can still read the action_id from the
// "Action:" line of the context block.
func looksLikeOpaqueID(s string) bool {
	if strings.Contains(s, "::") {
		return true
	}
	for _, tok := range strings.FieldsFunc(s, actionIDSeparator) {
		if len(tok) >= 12 && hasLetterAndDigit(tok) {
			return true
		}
		if consonantRun(tok) >= 5 {
			return true
		}
	}
	return false
}

func hasLetterAndDigit(s string) bool {
	var hasL, hasD bool
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasL = true
		case r >= '0' && r <= '9':
			hasD = true
		}
		if hasL && hasD {
			return true
		}
	}
	return false
}

func consonantRun(s string) int {
	maxRun, run := 0, 0
	for _, r := range s {
		isVowel := r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u' ||
			r == 'A' || r == 'E' || r == 'I' || r == 'O' || r == 'U' || r == 'y' || r == 'Y'
		isConsonant := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if isConsonant && !isVowel {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
	}
	return maxRun
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
	// IssueID is REQUIRED for any mutating (non-dry-run, non-read-only)
	// action. The broker rejects mutating calls without an issue_id so the
	// agent cannot do work that has no scoping artifact. The id must come
	// from a prior team_task action=create call in this conversation. See
	// the ISSUE JUDGMENT block in the system prompt.
	IssueID string `json:"issue_id,omitempty" jsonschema:"REQUIRED for mutating actions. Pass the team_task/Issue id this action executes under. Get it from a prior team_task action=create call. Read-only and dry_run actions may omit it."`
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
	approvalCtx, err := requireTeamActionApproval(ctx, slug, channel, args)
	if err != nil {
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
	verb := actionVerbLabel(args.Platform, args.ActionID)
	platformLabel := platformDisplay(args.Platform)
	// `intent` is the agent's own one-liner from args.Summary — that's
	// the same line the human just read on the approval card, so reusing
	// it in the outcome means the human sees "I approved THIS and it
	// ran" instead of a generic "Executed run an action via Gmail".
	// When the agent left Summary blank, fall back to verb+platform so
	// we still post something readable.
	intent := strings.TrimSpace(args.Summary)
	if intent == "" {
		intent = fmt.Sprintf("%s via %s", verb, platformLabel)
	}
	executedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		failSummary := fallbackSummary(args.Summary, fmt.Sprintf("%s action %s on %s failed", titleCaser.String(provider.Name()), args.ActionID, args.Platform))
		_ = brokerRecordAction(ctx, "external_action_failed", provider.Name(), channel, slug, failSummary, args.ActionID)
		// Failure ALWAYS posts (no dedupe, no read-only skip) — silent
		// failures are the worst UX. Clean the error of CLI/JSON noise
		// before showing it; the agent's followup human_message can
		// carry the deep detail when it matters.
		cleanedErr := sanitizeOutcomeError(err.Error())
		outcomeMsgID := brokerPostActionOutcomeMessage(ctx, channel, slug, fmt.Sprintf(
			"⚠️ %s — failed: %s",
			intent,
			cleanedErr,
		))
		recordExecuteAudit(ctx, approvalCtx, args, slug, channel, executedAt,
			team.ApprovalOutcomeExecutedFailed,
			fmt.Sprintf("%s — failed: %s", intent, cleanedErr),
			outcomeMsgID)
		return toolError(err), nil, nil
	}
	kind := "external_action_executed"
	if args.DryRun {
		kind = "external_action_planned"
	}
	_ = brokerRecordAction(ctx, kind, provider.Name(), channel, slug, intent, args.ActionID)
	// Decide whether to post to chat. Three rules:
	//   1. Read-only actions (list/search/get/...) bypass the approval
	//      gate and are agent-internal lookups; posting every one would
	//      flood chat with noise the human doesn't need to see.
	//   2. Recent duplicate of the same action — dedupe to avoid
	//      "✅ Executed ... ✅ Executed ... ✅ Executed" cascades when
	//      the agent retries a successful call.
	//   3. Everything else — the human approved it; they MUST see the
	//      confirmation.
	var outcomeMsgID string
	if !actionIsReadOnly(args.ActionID) && !recentlyPostedOutcome(slug, args.ActionID, channel) {
		// Build the outcome message: intent + a result preview so the
		// human sees what the approved action actually produced, not
		// just that it ran. Without the preview, the human gets "✅
		// List inbox threads" with no idea how many threads, which
		// ones, or what came back — defeating the trust contract of
		// "every approval feels useful". The preview is bounded so
		// the chat doesn't drown in raw JSON for large payloads; the
		// agent's followup human_message carries the interpretation.
		preview := summarizeActionResult(result)
		var body string
		if args.DryRun {
			body = fmt.Sprintf("📝 Planned: %s (dry-run, nothing sent)", intent)
		} else if preview != "" {
			body = fmt.Sprintf("✅ %s\n\n%s", intent, preview)
		} else {
			body = fmt.Sprintf("✅ %s", intent)
		}
		outcomeMsgID = brokerPostActionOutcomeMessage(ctx, channel, slug, body)
	}
	recordExecuteAudit(ctx, approvalCtx, args, slug, channel, executedAt,
		team.ApprovalOutcomeExecutedOK, intent, outcomeMsgID)
	return textResult(prettyObject(result)), nil, nil
}

// recordExecuteAudit writes an ApprovalAuditEntry to the broker for an
// executed action. No-op when the approval was bypassed (read-only, dry-run,
// unsafe) — those calls don't have a request id to correlate to.
func recordExecuteAudit(ctx context.Context, approvalCtx approvalContext, args TeamActionExecuteArgs, actor, channel, executedAt, outcome, summary, msgID string) {
	if strings.TrimSpace(approvalCtx.RequestID) == "" {
		return
	}
	platform := strings.TrimSpace(args.Platform)
	if platform == "" {
		platform = "unknown"
	}
	actionID := strings.TrimSpace(args.ActionID)
	if actionID == "" {
		actionID = "unknown"
	}
	brokerPostApprovalAudit(ctx, team.ApprovalAuditEntry{
		ApprovalRequestID:    approvalCtx.RequestID,
		TaskID:               approvalCtx.IssueID,
		Platform:             platform,
		ActionID:             actionID,
		ConnectionKey:        strings.TrimSpace(args.ConnectionKey),
		RequestedAt:          approvalCtx.RequestedAt,
		AnsweredAt:           approvalCtx.AnsweredAt,
		ExecutedAt:           executedAt,
		Outcome:              outcome,
		OutcomeSummary:       summary,
		OutcomeChatMessageID: msgID,
		Actor:                actor,
		Channel:              channel,
	})
}

// summarizeActionResult renders the action result into a short,
// chat-friendly block the human can read at a glance. The goal is
// "what came back?" — not the full payload, not raw JSON wrapping.
// For action results that are obviously structured (a list, a map
// with familiar keys), we surface the top-level shape (count, ids,
// subjects). For opaque shapes, we fall back to a pretty-printed
// excerpt clipped to a budget so chat stays readable.
//
// The agent is still prompted to post a richer human_message right
// after — that's where the interpretation lives. This preview just
// guarantees the raw signal is visible immediately, even before the
// agent's followup lands.
func summarizeActionResult(result any) string {
	if result == nil {
		return ""
	}
	// Try smart shaping for common list/map shapes first.
	if shaped := shapeKnownResult(result); shaped != "" {
		return shaped
	}
	// Fall back: pretty JSON, clipped + fenced so chat renders it
	// as a code block.
	raw := prettyObject(result)
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" || raw == "[]" {
		return ""
	}
	const maxPreviewBytes = 600
	if len(raw) > maxPreviewBytes {
		raw = raw[:maxPreviewBytes] + "\n… (truncated; agent will summarize next)"
	}
	return "```\n" + raw + "\n```"
}

// shapeKnownResult tries to lift the most common result shapes into a
// human line:
//   - a slice → "N item(s)" + a couple of representative summaries
//   - a map with a `threads` / `messages` / `items` / `results` /
//     `events` slice → same as above
//
// Returns "" when no familiar shape matches; the caller falls back to
// the raw JSON excerpt.
func shapeKnownResult(result any) string {
	// Slice at the top level.
	if arr, ok := result.([]any); ok {
		return shapeArraySummary(arr)
	}
	// Map at the top level — peek for a familiar list field.
	if m, ok := result.(map[string]any); ok {
		for _, key := range []string{"threads", "messages", "items", "results", "records", "events", "data", "rows"} {
			if v, present := m[key]; present {
				if arr, ok := v.([]any); ok {
					summary := shapeArraySummary(arr)
					if summary != "" {
						return key + ": " + summary
					}
				}
			}
		}
		// Single-item map with an id-shaped field is also useful
		// (e.g. send_email returns {"id": "...", "thread_id": "..."}).
		var ids []string
		for _, key := range []string{"id", "thread_id", "message_id", "event_id"} {
			if v, ok := m[key].(string); ok && v != "" {
				ids = append(ids, key+"="+v)
			}
		}
		if len(ids) > 0 {
			return strings.Join(ids, ", ")
		}
	}
	return ""
}

// shapeArraySummary builds a "N items" header + up to 3 representative
// one-line excerpts, picking common identifying fields (subject, name,
// title, id) when each item is a map.
func shapeArraySummary(arr []any) string {
	if len(arr) == 0 {
		return "0 results"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d result(s)", len(arr))
	max := len(arr)
	if max > 3 {
		max = 3
	}
	for i := 0; i < max; i++ {
		line := shapeArrayItem(arr[i])
		if line == "" {
			continue
		}
		fmt.Fprintf(&b, "\n• %s", line)
	}
	if len(arr) > max {
		fmt.Fprintf(&b, "\n… and %d more", len(arr)-max)
	}
	return b.String()
}

func shapeArrayItem(item any) string {
	m, ok := item.(map[string]any)
	if !ok {
		s := strings.TrimSpace(fmt.Sprint(item))
		if len(s) > 120 {
			s = s[:120] + "…"
		}
		return s
	}
	// Try common identifying fields, in order of "most readable".
	for _, key := range []string{"subject", "title", "name", "summary", "snippet", "preview"} {
		if v, ok := m[key].(string); ok {
			v = strings.TrimSpace(v)
			if v != "" {
				if len(v) > 100 {
					v = v[:100] + "…"
				}
				// Tack on a from/sender when present for emails.
				for _, fromKey := range []string{"from", "sender", "author"} {
					if f, ok := m[fromKey].(string); ok && f != "" {
						return v + " — " + f
					}
				}
				return v
			}
		}
	}
	// Fall back to id when nothing readable found.
	for _, key := range []string{"id", "thread_id", "message_id"} {
		if v, ok := m[key].(string); ok && v != "" {
			return key + "=" + v
		}
	}
	return ""
}

// sanitizeOutcomeError strips structured noise (JSON event blobs, CLI
// flow envelopes, multi-line stack traces) out of an error message so
// the chat outcome reads as a human-facing failure reason. Without
// this, an error like:
//
//	"one CLI failed: {\"event\":\"flow:start\",\"flowKey\":\"wuphf-auto-action\",...}"
//
// dominates the channel with implementation noise. We keep only the
// human prefix and replace the JSON tail with `(see logs)`.
func sanitizeOutcomeError(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no detail"
	}
	// Strip everything from the first { or [ onward — that's where the
	// JSON/CLI envelope starts. Keep the human prefix.
	if i := strings.IndexAny(s, "{["); i >= 0 {
		prefix := strings.TrimRight(strings.TrimSpace(s[:i]), ": ")
		if prefix != "" {
			s = prefix + " (see logs)"
		} else {
			s = "(see logs)"
		}
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// outcomeDedupeWindow is how long an identical action outcome counts
// as "just posted" — within this window, repeated successful execs of
// the same action by the same agent in the same channel are silenced
// so the channel doesn't fan out into "✅ ✅ ✅" spam. Failures are
// never deduped (silence-on-failure is the bug we never want).
const outcomeDedupeWindow = 8 * time.Second

var (
	outcomeDedupeMu  sync.Mutex
	outcomeDedupeMap = map[string]time.Time{}
)

// recentlyPostedOutcome returns true when the same (slug, action,
// channel) tuple has been posted in the last outcomeDedupeWindow.
// Also stamps the current time on the key, so callers don't need to
// record-then-check separately.
func recentlyPostedOutcome(slug, actionID, channel string) bool {
	key := strings.ToLower(strings.TrimSpace(slug)) + "|" +
		strings.ToLower(strings.TrimSpace(actionID)) + "|" +
		strings.ToLower(strings.TrimSpace(channel))
	now := time.Now()
	outcomeDedupeMu.Lock()
	defer outcomeDedupeMu.Unlock()
	if last, ok := outcomeDedupeMap[key]; ok && now.Sub(last) < outcomeDedupeWindow {
		return true
	}
	outcomeDedupeMap[key] = now
	// Opportunistic GC: prune entries older than 10x the window so the
	// map doesn't grow unbounded across long sessions.
	cutoff := now.Add(-outcomeDedupeWindow * 10)
	for k, t := range outcomeDedupeMap {
		if t.Before(cutoff) {
			delete(outcomeDedupeMap, k)
		}
	}
	return false
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
	provider, err := selectedActionProvider(action.CapabilityRelayEvents)
	if err != nil {
		return toolError(err), nil, nil
	}
	result, err := provider.ListRelayEvents(ctx, action.RelayEventsOptions{
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

// brokerPostActionOutcomeMessage posts a system-authored chat message
// summarising the outcome of an external action that just ran. Without
// this, the human's only signal that an approved action actually
// executed was the agent's *next* message — and if the agent went
// silent (timed out, lost context, didn't follow the prompt rule to
// surface outcomes), the human saw nothing. This guarantees a visible
// trace in the channel whenever an action runs, success or failure.
//
// Channel posting goes through the broker as actor=ceo so it routes
// through normal channel auth + the message ends up in the chat
// surface the human is already watching. `system` would be cleaner
// semantically but isn't a registered agent slug — the broker rejects
// posts from unknown senders, so we use the lead.
func brokerPostActionOutcomeMessage(ctx context.Context, channel, actor, content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	ch := resolveChannel(channel)
	from := strings.TrimSpace(actor)
	if from == "" {
		from = "ceo"
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"from":    from,
		"channel": ch,
		"kind":    "action_outcome",
		"content": strings.TrimSpace(content),
	}, &resp); err != nil {
		// Posting outcome messages is best-effort; failures here would
		// drop the chat trace but not block execution. Returning an empty
		// id surfaces to the audit-entry writer as "no deep link" rather
		// than blocking the audit record entirely.
		return ""
	}
	return strings.TrimSpace(resp.ID)
}

// brokerPostApprovalAudit ships an audit entry to the broker. Best-effort:
// errors are swallowed because audit failures must never block the action
// result from returning to the agent (which is already either committed or
// rolled back at this point).
func brokerPostApprovalAudit(ctx context.Context, entry team.ApprovalAuditEntry) {
	if strings.TrimSpace(entry.ApprovalRequestID) == "" {
		return
	}
	_ = brokerPostJSON(ctx, "/approval-audit", entry, nil)
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
