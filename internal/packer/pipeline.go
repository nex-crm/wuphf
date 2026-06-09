package packer

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrEnvelopeHeld is returned by Classify when an envelope field (Ask /
// ReturnPact) cannot be sanitized. Those fields are "never dropped", so an
// unprovable secret in one of them holds the WHOLE delegation rather than
// shipping it without an ask. A human must look at the task.
var ErrEnvelopeHeld = errors.New("packer: delegation held — envelope field failed egress redaction")

// BrainHandle is the office/tenant brain seam. OSS self-hosted: a singleton
// adapter over the broker. Nex cloud: one per tenant. The packer never touches
// global state directly. Every retrieval method is TASK-SCOPED — the packer
// never drives a free, intent-text query, so foreign-tainted intent cannot reach
// retrieval.
type BrainHandle interface {
	// PlanStep returns the human-approved plan step / IssueDraftSpec text.
	PlanStep(taskID string) (string, error)
	// TaskLearnings returns task-scoped learnings, already AND-scoped to taskID
	// (exact match, never a fuzzy fallback), highest-confidence first.
	TaskLearnings(taskID string, limit int) ([]BrainItem, error)
	// TaskWikiRefs returns the bodies of articles EXPLICITLY linked to the task
	// (teamTask.WikiRefs). Never free WikiIndex.Search.
	TaskWikiRefs(taskID string) ([]BrainItem, error)
	// Roster returns roster lines for the task.
	Roster(taskID string) ([]BrainItem, error)
}

// BrainItem is a retrieved brain candidate.
type BrainItem struct {
	Ref  string
	Body string
}

// GatherOptions tunes retrieval. ReturnPact and Guards are CEO-authored and
// passed in (they are not retrieved from the brain).
type GatherOptions struct {
	ReturnPact    string
	Guards        []string
	LearningLimit int
}

// Gather retrieves candidates, task-scoped, for the AUDIENCE tier (the
// least-trusted reader, computed by Pack), not the target's own tier — a
// downgraded audience must not even pull first-party content into the pipeline.
// Tainted intent does NOT drive retrieval: retrieval is keyed by task id, and
// the Ask carries the (CEO-restated) intent text only as an export to be
// classified, never as a query. An untrusted audience gets only the envelope +
// the approved plan step; first-party (and hosted) additionally get task-scoped
// learnings, task-linked wiki refs, and roster lines.
func Gather(brain BrainHandle, req ContextRequest, opts GatherOptions, audienceTier BotTrust) (RawBundle, error) {
	rb := RawBundle{
		Ask:        req.Intent.Text,
		ReturnPact: opts.ReturnPact,
		Guards:     append([]string(nil), opts.Guards...),
	}

	plan, err := brain.PlanStep(req.TaskID)
	if err != nil {
		return RawBundle{}, fmt.Errorf("gather plan step: %w", err)
	}
	if strings.TrimSpace(plan) != "" {
		rb.Items = append(rb.Items, RawItem{Ref: "plan:" + req.TaskID, Kind: KindPlan, Body: plan})
	}

	if audienceTier == BotUntrusted {
		// Untrusted audience gets envelope + approved plan step only. No
		// learnings, no wiki, no roster — nothing retrieved from the brain at
		// large, regardless of the target bot's own tier.
		return rb, nil
	}

	limit := opts.LearningLimit
	if limit <= 0 {
		limit = defaultLearningLimit
	}
	learnings, err := brain.TaskLearnings(req.TaskID, limit)
	if err != nil {
		return RawBundle{}, fmt.Errorf("gather learnings: %w", err)
	}
	for _, l := range learnings {
		rb.Items = append(rb.Items, RawItem{Ref: l.Ref, Kind: KindLearning, Body: l.Body})
	}

	wiki, err := brain.TaskWikiRefs(req.TaskID)
	if err != nil {
		return RawBundle{}, fmt.Errorf("gather wiki refs: %w", err)
	}
	for _, w := range wiki {
		rb.Items = append(rb.Items, RawItem{Ref: w.Ref, Kind: KindWiki, Body: w.Body})
	}

	roster, err := brain.Roster(req.TaskID)
	if err != nil {
		return RawBundle{}, fmt.Errorf("gather roster: %w", err)
	}
	for i, r := range roster {
		if i >= maxRosterLines {
			break
		}
		rb.Items = append(rb.Items, RawItem{Ref: r.Ref, Kind: KindRoster, Body: r.Body})
	}

	return rb, nil
}

// Classify is the egress boundary. It classifies and redacts the WHOLE
// delegation envelope (Ask, ReturnPact, Guards), not just Items, because those
// fields are "never dropped" by Budget — an unclassified envelope is a direct
// exfiltration channel. audience is the trust tier the content is classified
// against: the LEAST-trusted reader present for a shared thread, or the target
// for an ephemeral/DM delivery. It returns ErrEnvelopeHeld if Ask or ReturnPact
// cannot be sanitized (the delegation is held, never sent partial); a Guard line
// or an Item that cannot be sanitized is dropped, with an audit row.
func Classify(raw RawBundle, audience BotTrust, policy EgressPolicy, sc SecretScanner) (ContextBundle, []ItemAudit, error) {
	out := ContextBundle{}
	audit := make([]ItemAudit, 0, len(raw.Items)+3)

	// Ask — critical envelope field. Failure holds the whole delegation.
	ask, askRed, err := scanEnvelopeField(raw.Ask, KindAsk, audience, policy, sc)
	if err != nil {
		return ContextBundle{}, nil, fmt.Errorf("ask: %w", err)
	}
	out.Ask = ask
	if strings.TrimSpace(raw.Ask) != "" {
		audit = append(audit, ItemAudit{Ref: "ask", Kind: KindAsk, Class: ExportRedacted, Redactions: askRed})
	}

	// ReturnPact — critical envelope field. Failure holds the whole delegation.
	pact, pactRed, err := scanEnvelopeField(raw.ReturnPact, KindReturnPact, audience, policy, sc)
	if err != nil {
		return ContextBundle{}, nil, fmt.Errorf("return pact: %w", err)
	}
	out.ReturnPact = pact
	if strings.TrimSpace(raw.ReturnPact) != "" {
		audit = append(audit, ItemAudit{Ref: "returnpact", Kind: KindReturnPact, Class: ExportRedacted, Redactions: pactRed})
	}

	// Guards — advisory list. Each line is policy-classified like any item (so a
	// policy/tenant that denies KindGuard is enforced) and then scanned. A guard
	// line that is denied or cannot be sanitized is dropped — not a held
	// delegation, since guards are advisory.
	for i, g := range raw.Guards {
		if strings.TrimSpace(g) == "" {
			continue
		}
		ref := fmt.Sprintf("guard:%d", i)
		class := policy.Classify(KindGuard, audience)
		if class == ExportDenied {
			audit = append(audit, ItemAudit{Ref: ref, Kind: KindGuard, Class: ExportDenied})
			continue
		}
		res := sc.Scan(g)
		if !res.OK {
			audit = append(audit, ItemAudit{Ref: ref, Kind: KindGuard, Class: ExportDenied})
			continue
		}
		out.Guards = append(out.Guards, res.Content)
		audit = append(audit, ItemAudit{Ref: ref, Kind: KindGuard, Class: class, Redactions: res.Redactions})
	}

	// Items — drop ExportDenied (policy) and anything the scanner cannot prove
	// clean. This runs BEFORE budgeting so denied content is gone before ranking.
	for _, it := range raw.Items {
		class := policy.Classify(it.Kind, audience)
		if class == ExportDenied {
			audit = append(audit, ItemAudit{Ref: it.Ref, Kind: it.Kind, Class: ExportDenied})
			continue
		}
		res := sc.Scan(it.Body)
		if !res.OK {
			audit = append(audit, ItemAudit{Ref: it.Ref, Kind: it.Kind, Class: ExportDenied})
			continue
		}
		body := res.Content
		if class == ExportAllowed {
			body = it.Body // hosted tier: emit as-is (still scanned for safety)
		}
		out.Items = append(out.Items, ContextItem{Ref: it.Ref, Kind: it.Kind, Body: body, Class: class, Redactions: res.Redactions})
		audit = append(audit, ItemAudit{Ref: it.Ref, Kind: it.Kind, Class: class, Redactions: res.Redactions})
	}

	return out, audit, nil
}

// scanEnvelopeField classifies and redacts a single critical envelope field.
// An empty field is a no-op. A policy-denied or unsanitizable field returns an
// error so the caller holds the whole delegation.
func scanEnvelopeField(text string, kind ItemKind, audience BotTrust, policy EgressPolicy, sc SecretScanner) (string, int, error) {
	if strings.TrimSpace(text) == "" {
		return "", 0, nil
	}
	if policy.Classify(kind, audience) == ExportDenied {
		return "", 0, fmt.Errorf("%w: policy denied %s", ErrEnvelopeHeld, kind)
	}
	res := sc.Scan(text)
	if !res.OK {
		return "", 0, fmt.Errorf("%w: %s (%s)", ErrEnvelopeHeld, kind, res.Reason)
	}
	return res.Content, res.Redactions, nil
}

// budget ranks the surviving items and trims them to the profile's token tier.
// The ESSENTIALS — Ask, ReturnPact, Guards, and the approved plan step — are
// never dropped: the bot cannot act without them. They are length-capped
// instead, and only the non-essential items (learning -> wiki -> roster ->
// skill) are trimmed from the bottom to fit whatever budget remains. This means
// a large ReturnPact can never evict the plan step.
func budget(b ContextBundle, p BotProfile) ContextBundle {
	limit := mentionTokenCap
	if p.ReadScope == ReadThread {
		limit = threadTokenCap
	}

	// Cap the critical envelope fields so essentials always fit.
	b.Ask = capTokens(b.Ask, askTokenCap)
	b.ReturnPact = capTokens(b.ReturnPact, returnPactTokenCap)
	b.Guards = capGuards(b.Guards, guardsTokenCap)

	// Split essentials (plan) from trimmable items, preserving plan unconditionally.
	var plan []ContextItem
	var trimmable []ContextItem
	for _, it := range b.Items {
		if it.Kind == KindPlan {
			plan = append(plan, ContextItem{Ref: it.Ref, Kind: it.Kind, Class: it.Class, Redactions: it.Redactions, Body: capTokens(it.Body, planTokenCap)})
			continue
		}
		trimmable = append(trimmable, it)
	}

	sort.SliceStable(trimmable, func(i, j int) bool {
		return kindRank(trimmable[i].Kind) < kindRank(trimmable[j].Kind)
	})

	used := estimateTokens(b.Ask) + estimateTokens(b.ReturnPact)
	for _, g := range b.Guards {
		used += estimateTokens(g)
	}
	for _, it := range plan {
		used += estimateTokens(it.Body)
	}

	kept := plan
	for _, it := range trimmable {
		t := estimateTokens(it.Body)
		if used+t > limit {
			continue // drop lower-ranked non-essential items that do not fit
		}
		used += t
		kept = append(kept, it)
	}
	b.Items = kept
	return b
}

// kindRank orders items for trimming. Lower is kept longer.
func kindRank(k ItemKind) int {
	switch k {
	case KindPlan:
		return 0
	case KindLearning:
		return 1
	case KindWiki:
		return 2
	case KindRoster:
		return 3
	case KindSkill:
		return 4
	default:
		return 5
	}
}
