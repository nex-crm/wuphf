package packer

import "strings"

// Budget tiers (approximate tokens). The security pass runs first; these only
// govern how much of what SURVIVED classification is ranked in. See the spec's
// budget heuristic.
const (
	mentionTokenCap = 600  // ReadMentionOnly hard cap
	threadTokenCap  = 2400 // ReadThread: ~400 mention + up to ~2000 thread block

	// Essentials are never dropped, only length-capped so they always fit.
	askTokenCap        = 220
	returnPactTokenCap = 160
	guardsTokenCap     = 160 // total across all guard lines
	planTokenCap       = 600

	defaultLearningLimit = 6
	maxRosterLines       = 3
)

// estimateTokens is a cheap char-based token estimate (~4 chars/token). The
// packer never needs exact token counts — only a stable budget heuristic.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// capTokens truncates s to roughly maxTokens, on a word boundary where possible.
// A truncated string is marked so the reader knows it was cut.
func capTokens(s string, maxTokens int) string {
	if estimateTokens(s) <= maxTokens {
		return s
	}
	maxChars := maxTokens * 4
	if maxChars >= len(s) {
		return s
	}
	cut := s[:maxChars]
	if idx := strings.LastIndexAny(cut, " \n\t"); idx > maxChars/2 {
		cut = cut[:idx]
	}
	return strings.TrimRight(cut, " \n\t") + " …"
}

// render shapes a classified, budgeted bundle into a neutral PackedDelegation.
// It is unexported and sets the `sealed` marker, so a delivered delegation can
// only come from Pack — never a hand-constructed bundle that skipped Classify.
// The Slack-specific surface (Block Kit) is the bridge's job; here we produce
// MentionText (always carries the essentials: ask, return pact, guards, plan)
// and ThreadContext (the remaining items, only when the bot is verified to read
// the thread). The InjectionRecord is seeded DeliveryPending; Deliver fills
// RenderedHash, TokenCount, MessageTS, and the final status.
func render(b ContextBundle, req ContextRequest, audit []ItemAudit, audienceTier BotTrust) PackedDelegation {
	mention := renderMention(b, req.Target.ReadScope)

	thread := ""
	if req.Target.ReadScope == ReadThread {
		thread = renderThread(b)
	}

	rec := InjectionRecord{
		IdempotencyKey: req.IdempotencyKey,
		TaskID:         req.TaskID,
		TaskUpdatedAt:  req.TaskUpdatedAt,
		PlanID:         req.PlanID,
		PlanVersion:    req.PlanVersion,
		Identity:       req.Target.Identity,
		BotTrust:       req.Target.Trust,
		AudienceTrust:  audienceTier,
		ProfileVersion: req.Target.Version,
		PolicyVersion:  req.EgressPolicyVer,
		WorkspaceID:    req.Thread.WorkspaceID,
		ChannelID:      req.Thread.ChannelID,
		ThreadTS:       req.Thread.ThreadTS,
		Items:          audit,
		Status:         DeliveryPending,
	}

	return PackedDelegation{MentionText: mention, ThreadContext: thread, Injection: rec, sealed: true}
}

// capGuards trims a guard list so its combined token estimate stays under total,
// dropping whole lines from the end (guards are advisory, so the earliest /
// most-important survive).
func capGuards(guards []string, total int) []string {
	used := 0
	kept := make([]string, 0, len(guards))
	for _, g := range guards {
		t := estimateTokens(g)
		if used+t > total {
			break
		}
		used += t
		kept = append(kept, g)
	}
	return kept
}

// renderMention builds the always-delivered mention. For ReadMentionOnly bots it
// carries the essentials plus the approved plan step (everything they will ever
// see). For ReadThread bots it stays lean — the bulk goes in the thread block.
func renderMention(b ContextBundle, scope ReadScope) string {
	var sb strings.Builder
	writeSection(&sb, "ASK", b.Ask)
	writeSection(&sb, "RETURN", b.ReturnPact)
	if len(b.Guards) > 0 {
		writeSection(&sb, "GUARDS", "- "+strings.Join(b.Guards, "\n- "))
	}
	// The approved plan step always rides in the mention — even a mention-only
	// bot needs the scoped step to act.
	for _, it := range b.Items {
		if it.Kind == KindPlan {
			writeSection(&sb, "PLAN", it.Body)
		}
	}
	if scope == ReadMentionOnly {
		// Mention-only bots get the non-plan survivors inline too, since they
		// never read the thread block.
		for _, it := range b.Items {
			if it.Kind != KindPlan {
				writeSection(&sb, strings.ToUpper(string(it.Kind)), it.Body)
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// renderThread builds the channel-visible thread block: the non-plan survivors
// (learnings, wiki, roster, skills) for a thread-reading bot.
func renderThread(b ContextBundle) string {
	var sb strings.Builder
	for _, it := range b.Items {
		if it.Kind == KindPlan {
			continue
		}
		writeSection(&sb, strings.ToUpper(string(it.Kind)), it.Body)
	}
	return strings.TrimSpace(sb.String())
}

func writeSection(sb *strings.Builder, label, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	sb.WriteString("== ")
	sb.WriteString(label)
	sb.WriteString(" ==\n")
	sb.WriteString(strings.TrimSpace(body))
	sb.WriteString("\n\n")
}
