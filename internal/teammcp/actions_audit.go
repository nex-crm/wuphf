package teammcp

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

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
	cutoff := now.Add(-outcomeDedupeWindow * 10)
	for k, t := range outcomeDedupeMap {
		if t.Before(cutoff) {
			delete(outcomeDedupeMap, k)
		}
	}
	return false
}

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
