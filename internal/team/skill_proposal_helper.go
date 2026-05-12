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
//  4. Render skill markdown via RenderSkillMarkdown.
//  5. Release b.mu → WikiWorker.Enqueue → re-acquire b.mu.
//  6. Re-check dedup (race window).
//  7. Append to b.skills, emit SSE via appendSkillProposalRequestLocked.
func (b *Broker) writeSkillProposalLocked(spec teamSkill) (*teamSkill, error) {
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
		// Backfill missing source_article on existing skills. Stage A skills
		// created before the provenance fix landed will not heal otherwise:
		// the dedup short-circuit returns before Step 4 ever copies
		// spec.SourceArticle into the new struct, so /skills and the
		// rendered SKILL.md stay permanently empty until the skill is
		// deleted and recreated.
		incoming := strings.TrimSpace(spec.SourceArticle)
		if existing.SourceArticle == "" && incoming != "" {
			existing.SourceArticle = incoming
			existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			slog.Info("skill_proposal: backfilled source_article on existing dedup",
				"name", name, "source_article", incoming)
			if err := b.saveLocked(); err != nil {
				slog.Warn("skill_proposal: saveLocked after source_article backfill failed",
					"name", name, "err", err)
			}
		} else {
			slog.Debug("writeSkillProposalLocked: skill already exists, skipping",
				"name", name, "existing_status", existing.Status)
		}
		return existing, nil
	}

	// --- Step 3b: Semantic dedup ---
	// Check for semantically similar skills that differ only in name.
	if similar := b.findSimilarSkillsLocked(name, spec.Description); len(similar) > 0 {
		best := similar[0]
		candidateContent := strings.TrimSpace(spec.Content)
		existingContent := strings.TrimSpace(best.Skill.Content)

		// If the candidate has substantive new content, enhance the existing
		// skill instead of discarding the candidate.
		if candidateContent != "" && candidateContent != existingContent {
			enhanced, enhErr := b.enhanceSkillLocked(best.Skill.Name, candidateContent, spec.Description, slug)
			if enhErr == nil && enhanced != nil {
				atomic.AddInt64(&b.skillCompileMetrics.SkillEnhancementsTotal, 1)
				slog.Info("writeSkillProposalLocked: enhanced existing skill",
					"candidate", name, "existing", best.Skill.Name,
					"tier", best.Tier)
				return enhanced, nil
			}
			// Enhancement failed — surface the error rather than silently
			// dropping the candidate's new content into the dedup discard
			// path. A transient render/wiki failure should be retryable by
			// the caller, not converted into a permanent loss of detail.
			if enhErr != nil {
				return nil, fmt.Errorf("writeSkillProposalLocked: enhance %q from candidate %q: %w",
					best.Skill.Name, name, enhErr)
			}
		}

		// Pure duplicate — record the dedup linkage on the existing skill
		// and persist both the broker state AND the wiki markdown so the
		// on-disk frontmatter stays in sync with related_skills.
		best.Skill.RelatedSkills = appendUnique(best.Skill.RelatedSkills, slug)
		best.Skill.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := b.saveLocked(); err != nil {
			slog.Warn("writeSkillProposalLocked: saveLocked after dedup linkage failed",
				"name", name, "err", err)
		}
		// Re-render the existing skill's wiki markdown so related_skills
		// (surfaced in frontmatter per skill_frontmatter.go) does not drift
		// from broker state. Failure here is logged but non-fatal: state
		// is already persisted and the dedup decision itself is sound.
		b.rerenderSkillWikiLocked(best.Skill, "dedup-linkage")
		atomic.AddInt64(&b.skillCompileMetrics.SemanticDedupHitsTotal, 1)
		slog.Info("writeSkillProposalLocked: semantic dedup hit",
			"candidate", name, "existing", best.Skill.Name,
			"tier", best.Tier, "slug_score", best.SlugScore,
			"desc_score", best.DescScore, "embed_cos", best.EmbedCos)
		return best.Skill, nil
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
		SourceArticle:       strings.TrimSpace(spec.SourceArticle),
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
		DisabledFromStatus:  strings.TrimSpace(spec.DisabledFromStatus),
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
	b.appendSkillProposalRequestLocked(sk, channel, now)

	result := &b.skills[len(b.skills)-1]
	slog.Info("writeSkillProposalLocked: skill proposal created",
		"name", name, "slug", slug, "created_by", createdBy, "sha", sha)
	return result, nil
}

// enhanceSkillLocked updates an existing skill's content and description with
// new details from a candidate. The caller MUST hold b.mu on entry. The method
// uses the same deadlock-safe unlock/relock pattern as writeSkillProposalLocked
// for the WikiWorker.Enqueue call.
//
// This is the core of the "enhance instead of discard" behavior: when a new
// skill candidate adds specificity to an existing skill (e.g. "create pitch
// deck for SaaS" enhances "create pitch deck"), the existing skill absorbs
// the new details rather than the candidate being silently dropped.
func (b *Broker) enhanceSkillLocked(existingName, newContent, newDescription, candidateSlug string) (*teamSkill, error) {
	sk := b.findSkillByNameLocked(existingName)
	if sk == nil {
		return nil, fmt.Errorf("enhanceSkillLocked: skill %q not found", existingName)
	}

	newContent = strings.TrimSpace(newContent)
	newDescription = strings.TrimSpace(newDescription)
	if newContent == "" {
		return nil, fmt.Errorf("enhanceSkillLocked: empty content for %q", existingName)
	}

	// Build the candidate state on a local copy so a concurrent append to
	// b.skills (which may reallocate the backing array) cannot leave sk
	// pointing at a stale element while the lock is released for Enqueue.
	updated := *sk
	updated.Content = mergeSkillContent(sk.Content, newContent, "enhanced")
	if newDescription != "" && len(newDescription) > len(updated.Description) {
		updated.Description = newDescription
	}
	if candidateSlug != "" {
		updated.RelatedSkills = appendUnique(append([]string(nil), sk.RelatedSkills...), candidateSlug)
	}
	updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	slug := skillSlug(existingName)
	fm := teamSkillToFrontmatter(updated)
	mdBytes, err := RenderSkillMarkdown(fm, updated.Content)
	if err != nil {
		return nil, fmt.Errorf("enhanceSkillLocked: render markdown for %q: %w", existingName, err)
	}

	wikiPath := "team/skills/" + slug + ".md"
	commitMsg := "archivist: enhance skill " + slug

	// Deadlock-safe: release b.mu before WikiWorker.Enqueue.
	wikiWorker := b.wikiWorker
	b.mu.Unlock()

	if wikiWorker != nil {
		_, _, err = wikiWorker.Enqueue(
			context.Background(),
			slug,
			wikiPath,
			string(mdBytes),
			"replace",
			commitMsg,
		)
		if err != nil {
			b.mu.Lock()
			return nil, fmt.Errorf("enhanceSkillLocked: wiki enqueue for %q: %w", existingName, err)
		}
	}

	b.mu.Lock()

	// Re-resolve after re-acquiring the lock: the slice may have been
	// reallocated or the skill may have been archived concurrently.
	live := b.findSkillByNameLocked(existingName)
	if live == nil {
		return nil, fmt.Errorf("enhanceSkillLocked: skill %q vanished during enqueue", existingName)
	}
	*live = updated
	b.invalidateSkillEmbeddingLocked(slug)

	if saveErr := b.saveLocked(); saveErr != nil {
		slog.Warn("enhanceSkillLocked: saveLocked failed", "slug", slug, "err", saveErr)
	}

	slog.Info("enhanceSkillLocked: skill enhanced",
		"name", existingName, "slug", slug)
	return live, nil
}

// rerenderSkillWikiLocked re-renders the given skill to its wiki markdown so
// the on-disk frontmatter stays consistent with broker state after metadata
// mutations (e.g. RelatedSkills). Caller MUST hold b.mu on entry. The method
// follows the same deadlock-safe unlock/relock pattern as the other write
// paths because WikiWorker.Enqueue ultimately calls PublishWikiEvent which
// re-acquires b.mu.
//
// Failures are logged with the supplied reason but not returned: the calling
// path has already persisted broker state, so a wiki drift is recoverable on
// the next mutation. Returning an error would force callers to handle a
// case that is strictly worse than the previous "never re-render" behavior.
func (b *Broker) rerenderSkillWikiLocked(sk *teamSkill, reason string) {
	if sk == nil {
		return
	}
	wikiWorker := b.wikiWorker
	if wikiWorker == nil {
		return
	}
	slug := skillSlug(sk.Name)
	fm := teamSkillToFrontmatter(*sk)
	mdBytes, err := RenderSkillMarkdown(fm, sk.Content)
	if err != nil {
		slog.Warn("rerenderSkillWikiLocked: render failed",
			"slug", slug, "reason", reason, "err", err)
		return
	}
	wikiPath := "team/skills/" + slug + ".md"
	commitMsg := "archivist: rerender skill " + slug + " (" + reason + ")"
	b.mu.Unlock()
	_, _, enqErr := wikiWorker.Enqueue(
		context.Background(),
		slug,
		wikiPath,
		string(mdBytes),
		"replace",
		commitMsg,
	)
	b.mu.Lock()
	if enqErr != nil {
		slog.Warn("rerenderSkillWikiLocked: wiki enqueue failed",
			"slug", slug, "reason", reason, "err", enqErr)
	}
}
