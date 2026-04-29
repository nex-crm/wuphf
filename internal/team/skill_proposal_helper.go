package team

// skill_proposal_helper.go provides the unified funnel for all skill-write
// paths: the Stage A scanner, MCP handler, and future synthesiser all call
// writeSkillProposalLocked. The helper enforces Anthropic frontmatter
// validation, system-author whitelisting, deduplication, and the deadlock-safe
// lock-release-Enqueue-reacquire pattern required because WikiWorker.Enqueue
// triggers PublishWikiEvent, which takes b.mu.

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// skillSystemAuthors is the whitelist of identities that may create skill
// proposals without being registered as team members. These are internal
// service identities, not human agents.
var skillSystemAuthors = map[string]bool{
	"archivist": true,
	"scanner":   true,
	"system":    true,
}

// skillSlugRegex validates the Anthropic Agent Skills slug format.
var skillSlugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// errSkillSimilarToExisting is the sentinel error writeSkillProposalLocked
// returns when the similarity gate (PR 7 task #13) flags the candidate as
// near-duplicate of an existing active skill. Callers (scanner, synthesizer,
// MCP handlers) recognise it via errors.As and route the proposal into a
// new "enhance_skill_proposal" interview instead of treating the failure
// as a generic write error.
//
// All fields are flat scalars (no *teamSkill pointer) so the error stays
// safe to propagate after b.mu is released. The integration in
// writeSkillProposalLocked snapshots the existing skill into these fields
// while still holding b.mu.
type errSkillSimilarToExisting struct {
	// Slug is the existing skill's slug (lowercase, kebab).
	Slug string
	// Title is the existing skill's display title at the time of detection.
	Title string
	// Description is the existing skill's one-line description.
	Description string
	// Score is the similarity score in [0,1].
	Score float64
	// Method is "embedding-cosine" or "jaccard-tokens".
	Method string
}

// Error satisfies the error interface so errors.As callers receive a
// human-readable message when they don't unwrap the sentinel.
func (e *errSkillSimilarToExisting) Error() string {
	return fmt.Sprintf("skill similar to existing %q (score=%.3f via %s); enhance instead of creating new",
		e.Slug, e.Score, e.Method)
}

// proposalOpts toggles non-default behavior of the unified write funnel
// (PR 7 task #15). Default zero-value is the legacy contract.
type proposalOpts struct {
	// bypassSimilarity skips Step 3b's similarity gate so the candidate
	// always lands as a normal proposed skill. Set by the answer handler
	// when the human picks "approve_anyway" on an enhance_skill_proposal
	// interview — the human has explicitly overridden the gate's verdict.
	bypassSimilarity bool
}

// writeSkillProposalLocked is the unified write funnel for all skill proposals.
//
// The caller MUST hold b.mu on entry. The function releases b.mu while calling
// WikiWorker.Enqueue (to avoid a deadlock with PublishWikiEvent which also
// acquires b.mu), then re-acquires b.mu before updating b.skills and emitting
// SSE. It re-checks deduplication after re-acquiring in case a concurrent
// writer created the same slug while the lock was released.
//
// Steps:
//  1. Validate Anthropic frontmatter: non-empty Name + Description, slug regex.
//  2. System-author whitelist: bypass findMemberLocked for archivist/scanner/system.
//  3. Dedup: return existing skill if findSkillByNameLocked matches.
//  3b. Similarity gate (skipped when proposalOpts.bypassSimilarity is set).
//  4. Render skill markdown via RenderSkillMarkdown.
//  5. Release b.mu → WikiWorker.Enqueue → re-acquire b.mu.
//  6. Re-check dedup (race window).
//  7. Append to b.skills, emit SSE via appendSkillProposalRequestLocked.
func (b *Broker) writeSkillProposalLocked(spec teamSkill) (*teamSkill, error) {
	return b.writeSkillProposalWithOptsLocked(spec, proposalOpts{})
}

// writeSkillProposalWithOptsLocked is the inner implementation that takes
// the proposalOpts knob. Most callers should use writeSkillProposalLocked.
func (b *Broker) writeSkillProposalWithOptsLocked(spec teamSkill, opts proposalOpts) (*teamSkill, error) {
	// --- Step 1: Validate ---
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, fmt.Errorf("writeSkillProposalLocked: name is required")
	}
	if strings.TrimSpace(spec.Description) == "" {
		return nil, fmt.Errorf("writeSkillProposalLocked: description is required for skill %q", name)
	}
	slug := skillSlug(name)
	if !skillSlugRegex.MatchString(slug) {
		return nil, fmt.Errorf("writeSkillProposalLocked: slug %q does not match ^[a-z0-9][a-z0-9-]*$", slug)
	}

	// --- Step 2: System-author check ---
	createdBy := strings.TrimSpace(spec.CreatedBy)
	if !skillSystemAuthors[createdBy] {
		// Non-system author must be a registered team member.
		if b.findMemberLocked(createdBy) == nil {
			return nil, fmt.Errorf("writeSkillProposalLocked: created_by %q is not a registered team member", createdBy)
		}
	}

	// --- Step 3: Pre-lock dedup ---
	if existing := b.findSkillByNameLocked(name); existing != nil {
		slog.Debug("writeSkillProposalLocked: skill already exists, skipping",
			"name", name, "existing_status", existing.Status)
		return existing, nil
	}

	// --- Step 3a: Owner validation (PR 7 step 5) ---
	// Strip any OwnerAgents slug that does not correspond to an active office
	// member. Graceful degradation: an unknown slug becomes a no-op (the skill
	// stays scoped to the remaining valid owners, or falls back to lead-only
	// when the entire list ends up empty). We do NOT reject the skill — that
	// would block the user's stated intent over a typo.
	spec.OwnerAgents = b.validateOwnerAgentsLocked(name, spec.OwnerAgents)

	// --- Step 3b: Similarity gate (PR 7 task #13) ---
	// Compare the candidate against active skills before spending budget on
	// the safety scan + wiki render. Three branches:
	//
	//   create_new        — proceed normally.
	//   enhance_existing  — return errSkillSimilarToExisting; caller routes
	//                       to an "enhance_skill_proposal" interview.
	//   ambiguous         — proceed BUT stash the closest match in the
	//                       frontmatter so the interview UI can flag it.
	//
	// IO discipline: findSimilarActiveSkillLocked may call b.skillEmbedder
	// while b.mu is held. The 5s timeout ceiling caps the lock-hold even if
	// the provider hangs; on timeout the helper falls through to Jaccard
	// inside the same call (Method becomes "jaccard-tokens" automatically).
	//
	// PR 7 task #15: opts.bypassSimilarity skips the gate entirely. The
	// answer handler sets this when the human picks "approve_anyway" on an
	// enhance_skill_proposal interview — the gate already fired once and
	// the human has explicitly overridden it; firing again would deadlock
	// the override flow.
	var verdict SimilarityVerdict
	if !opts.bypassSimilarity {
		simCtx, simCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer simCancel() // PR 7 /review P1: ensure cancel on every return path
		// PR 7 /review P1: findSimilarActiveSkillLocked takes b.mu internally
		// (snapshots candidates, releases for embedding IO, reacquires for
		// scoring). Release here so it can take the lock cleanly; reacquire
		// before the rest of the funnel resumes its lock-held invariants.
		b.mu.Unlock()
		verdict = b.findSimilarActiveSkillLocked(simCtx, spec)
		b.mu.Lock()
		// Re-check dedup since b.skills could have grown while the lock was
		// momentarily dropped (a concurrent writer could have created a slug
		// match). Mirrors Step 6 in the WikiWorker.Enqueue release-reacquire.
		if existing := b.findSkillByNameLocked(name); existing != nil {
			slog.Debug("writeSkillProposalLocked: skill created during similarity gate, returning existing",
				"name", name, "existing_status", existing.Status)
			return existing, nil
		}
	}
	switch verdict.Recommendation {
	case "enhance_existing":
		// PR 7 /review P1: SimilarityVerdict now carries flat ExistingSlug /
		// ExistingTitle / ExistingDescription strings (no pointer back into
		// b.skills) so this snapshot copy is just struct-field assignment.
		atomic.AddInt64(&b.skillCompileMetrics.EnhancementCandidatesTotal, 1)
		err := &errSkillSimilarToExisting{
			Slug:        verdict.ExistingSlug,
			Title:       verdict.ExistingTitle,
			Description: verdict.ExistingDescription,
			Score:       verdict.Score,
			Method:      verdict.Method,
		}
		slog.Info("writeSkillProposalLocked: similarity gate diverted to enhance",
			"candidate", name, "existing", err.Slug, "score", verdict.Score, "method", verdict.Method)
		return nil, err
	case "ambiguous":
		// Annotation happens after teamSkillToFrontmatter below; the verdict
		// already carries flat slug+score+method strings.
	}

	// Ambiguous-band annotation. The flat verdict fields are safe to copy
	// directly — no pointer escape.
	var ambiguousSimRef *SkillSimilarRef
	if verdict.Recommendation == "ambiguous" && verdict.ExistingSlug != "" {
		ambiguousSimRef = &SkillSimilarRef{
			Slug:   verdict.ExistingSlug,
			Score:  verdict.Score,
			Method: verdict.Method,
		}
	}

	// --- Step 4: Build in-memory struct + render markdown ---
	now := time.Now().UTC().Format(time.RFC3339)
	channel := strings.TrimSpace(spec.Channel)
	if channel == "" {
		channel = "general"
	}
	status := strings.TrimSpace(spec.Status)
	if status == "" {
		status = "proposed"
	}
	title := strings.TrimSpace(spec.Title)
	if title == "" {
		title = name
	}

	b.counter++
	sk := teamSkill{
		ID:                  fmt.Sprintf("skill-%s", slug),
		Name:                name,
		Title:               title,
		Description:         strings.TrimSpace(spec.Description),
		Content:             strings.TrimSpace(spec.Content),
		CreatedBy:           createdBy,
		Channel:             channel,
		Tags:                append([]string(nil), spec.Tags...),
		Trigger:             strings.TrimSpace(spec.Trigger),
		WorkflowProvider:    strings.TrimSpace(spec.WorkflowProvider),
		WorkflowKey:         strings.TrimSpace(spec.WorkflowKey),
		WorkflowDefinition:  strings.TrimSpace(spec.WorkflowDefinition),
		WorkflowSchedule:    strings.TrimSpace(spec.WorkflowSchedule),
		RelayID:             strings.TrimSpace(spec.RelayID),
		RelayPlatform:       strings.TrimSpace(spec.RelayPlatform),
		RelayEventTypes:     append([]string(nil), spec.RelayEventTypes...),
		LastExecutionAt:     strings.TrimSpace(spec.LastExecutionAt),
		LastExecutionStatus: strings.TrimSpace(spec.LastExecutionStatus),
		UsageCount:          0,
		Status:              status,
		SourceArticles:      append([]string(nil), spec.SourceArticles...),
		OwnerAgents:         append([]string(nil), spec.OwnerAgents...),
		SimilarToExisting:   ambiguousSimRef,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	// --- Step 4a: Skill guard (PR 1b) ---
	// Trust ladder: archivist/scanner == community (Stage A wiki source).
	// Future synth-from-LLM authors will use agent_created.
	trust := TrustCommunity
	switch createdBy {
	case "archivist", "scanner":
		trust = TrustCommunity
	case "system":
		trust = TrustTrusted
	}
	fm := teamSkillToFrontmatter(sk)
	// Annotate ambiguous-similarity verdict captured at Step 3b. The interview
	// UI flips to a "review-near-duplicate" affordance when this is non-nil.
	if ambiguousSimRef != nil {
		fm.Metadata.Wuphf.SimilarToExisting = ambiguousSimRef
	}
	scan := ScanSkill(fm, sk.Content, trust)
	fm.Metadata.Wuphf.SafetyScan = &SkillSafetyScan{
		Verdict:    string(scan.Verdict),
		Findings:   append([]string(nil), scan.Findings...),
		TrustLevel: string(scan.TrustLevel),
		Summary:    scan.Summary,
	}
	// Trust-ladder gate: dangerous always rejected for non-builtin/trusted.
	if scan.Verdict == VerdictDangerous && trust != TrustBuiltin && trust != TrustTrusted {
		atomic.AddInt64(&b.skillCompileMetrics.ProposalsRejectedByGuardTotal, 1)
		return nil, fmt.Errorf("skill_guard: rejected — %s", scan.Summary)
	}
	// agent_created trust requires safe verdict; caution + dangerous are rejected.
	if trust == TrustAgentCreated && scan.Verdict != VerdictSafe {
		atomic.AddInt64(&b.skillCompileMetrics.ProposalsRejectedByGuardTotal, 1)
		return nil, fmt.Errorf("skill_guard: rejected (agent_created trust requires safe verdict) — %s", scan.Summary)
	}
	if scan.Verdict == VerdictCaution {
		slog.Warn("writeSkillProposalLocked: caution verdict allowed under trust",
			"name", name, "trust", string(trust), "summary", scan.Summary)
	}

	// Render the markdown to write to the wiki.
	mdBytes, err := RenderSkillMarkdown(fm, sk.Content)
	if err != nil {
		return nil, fmt.Errorf("writeSkillProposalLocked: render markdown for %q: %w", name, err)
	}

	wikiPath := "team/skills/" + slug + ".md"
	commitMsg := "archivist: propose skill " + slug

	// --- Step 5: DEADLOCK FIX — release b.mu before Enqueue ---
	// WikiWorker.Enqueue blocks on its reply channel. The drain goroutine calls
	// PublishWikiEvent after a successful commit, and PublishWikiEvent acquires
	// b.mu. Holding b.mu here while waiting on Enqueue would deadlock.
	wikiWorker := b.wikiWorker
	b.mu.Unlock()

	var sha string
	if wikiWorker != nil {
		sha, _, err = wikiWorker.Enqueue(
			context.Background(),
			slug,
			wikiPath,
			string(mdBytes),
			"replace",
			commitMsg,
		)
		if err != nil {
			// Re-acquire before returning so deferred unlock works correctly.
			b.mu.Lock()
			slog.Warn("writeSkillProposalLocked: WikiWorker.Enqueue failed",
				"slug", slug, "err", err)
			return nil, fmt.Errorf("writeSkillProposalLocked: wiki enqueue for %q: %w", name, err)
		}
	}

	// --- Step 6: Re-acquire and re-check dedup (race window) ---
	b.mu.Lock()
	if existing := b.findSkillByNameLocked(name); existing != nil {
		slog.Debug("writeSkillProposalLocked: skill created concurrently, returning existing",
			"name", name, "sha", sha)
		return existing, nil
	}

	// --- Step 7: Commit to in-memory index and emit SSE ---
	b.skills = append(b.skills, sk)
	// Note: writeSkillProposalLocked never enters the enhance_skill_proposal
	// path on its own — that branch returns errSkillSimilarToExisting earlier.
	// Scanner / synth callers that catch the sentinel produce the
	// enhance_skill_proposal interview directly via this helper after their
	// own bookkeeping; pass "" here for the legacy "Activate it?" framing.
	b.appendSkillProposalRequestLocked(sk, channel, now, "")

	result := &b.skills[len(b.skills)-1]
	slog.Info("writeSkillProposalLocked: skill proposal created",
		"name", name, "slug", slug, "created_by", createdBy, "sha", sha)
	return result, nil
}

// validateOwnerAgentsLocked filters owners down to slugs that correspond to a
// registered office member. Unknown slugs are dropped with a WARN log (one per
// unknown slug) so the operator sees the typo without losing the rest of the
// scope. Returns the filtered list — empty means the skill is lead-routable.
//
// Caller MUST hold b.mu.
func (b *Broker) validateOwnerAgentsLocked(skillName string, owners []string) []string {
	if len(owners) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(owners))
	out := make([]string, 0, len(owners))
	for _, raw := range owners {
		slug := strings.ToLower(strings.TrimSpace(raw))
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		// Belt-and-suspenders: reject anything that isn't a well-formed Anthropic
		// Agent Skills slug shape before we hit findMemberLocked. The member
		// table would also miss `../etc/passwd`, but enforcing the regex here
		// closes the path-injection vector even if a future code path looks
		// owners up via something other than the member table.
		if !skillSlugRegex.MatchString(slug) {
			slog.Warn("writeSkillProposalLocked: dropping malformed owner slug",
				"skill", skillName, "owner", slug)
			continue
		}
		if b.findMemberLocked(slug) == nil {
			slog.Warn("writeSkillProposalLocked: dropping unknown owner slug",
				"skill", skillName, "owner", slug)
			continue
		}
		out = append(out, slug)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
