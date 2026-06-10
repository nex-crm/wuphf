package team

// skill_compile_writer.go provides the unified write funnel for playbook
// compilation — the ONLY path that creates skills (core-loop R5). The wiki
// scanner calls writeCompiledSkillLocked. The helper enforces Anthropic
// frontmatter validation, system-author whitelisting, update-first
// consolidation (enhance an existing skill instead of creating a near
// duplicate, within the skillUpdateFirstMaxBytes threshold), deduplication,
// and the deadlock-safe lock-release-Enqueue-reacquire pattern required
// because WikiWorker.Enqueue triggers PublishWikiEvent, which takes b.mu.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// skillSystemAuthors is the whitelist of identities that may write compiled
// skills without being registered as team members. These are internal
// service identities, not human agents.
var skillSystemAuthors = map[string]bool{
	"archivist": true,
	"scanner":   true,
	"system":    true,
}

// skillSlugRegex validates the Anthropic Agent Skills slug format.
var skillSlugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// skillInvariantBlockRegex matches a protected invariant region inside a
// skill body. The mechanism follows SkillOpt's `SLOW_UPDATE_START/END`
// pattern (arXiv 2605.23904 §3): step-level edits MUST NOT touch lines
// inside the block. We enforce this by extracting every invariant block
// from the existing body before an enhance merge and verifying that the
// resulting merged body still contains each block verbatim. Authors
// declare protected regions like this:
//
//	<!-- INVARIANT-START -->
//	Always validate inputs before persisting.
//	Never auto-approve destructive actions.
//	<!-- INVARIANT-END -->
//
// Removing the SkillOpt analog cost 22.5 points on SpreadsheetBench, so
// the gate is worth the small parsing surface.
var skillInvariantBlockRegex = regexp.MustCompile(`(?s)<!-- ?INVARIANT-START ?-->(.*?)<!-- ?INVARIANT-END ?-->`)

// extractSkillInvariantBlocks returns every invariant block found in body,
// in document order. Each returned string contains the full block
// (including the START/END markers) so the verification step can match
// it verbatim without rebuilding the wrapper.
func extractSkillInvariantBlocks(body string) []string {
	if !strings.Contains(body, "INVARIANT-START") {
		return nil
	}
	matches := skillInvariantBlockRegex.FindAllString(body, -1)
	if len(matches) == 0 {
		return nil
	}
	return append([]string(nil), matches...)
}

// verifySkillInvariantsPreserved checks that every invariant block in
// previousBody is still present verbatim in mergedBody. Returns nil when
// the existing body had no invariant blocks (the common case) or when
// every block survived. Returns a descriptive error otherwise. Used by
// the enhance / rename paths to reject edits that drop or mutate a
// protected region.
func verifySkillInvariantsPreserved(previousBody, mergedBody string) error {
	blocks := extractSkillInvariantBlocks(previousBody)
	if len(blocks) == 0 {
		return nil
	}
	for i, block := range blocks {
		if !strings.Contains(mergedBody, block) {
			return fmt.Errorf("enhancement violated protected invariant block #%d (must survive verbatim across edits)", i+1)
		}
	}
	return nil
}

// writeCompiledSkillLocked is the unified write funnel for compiled skills.
//
// The caller MUST hold b.mu on entry. The function releases b.mu while calling
// WikiWorker.Enqueue (to avoid a deadlock with PublishWikiEvent which also
// acquires b.mu), then re-acquires b.mu before updating b.skills. It re-checks
// deduplication after re-acquiring in case a concurrent writer created the
// same slug while the lock was released.
//
// Steps:
//  1. Validate Anthropic frontmatter: non-empty Name + Description, slug regex.
//  2. System-author whitelist: bypass findMemberLocked for archivist/scanner/system.
//  3. Dedup: return existing skill if findSkillByNameLocked matches.
//     3b. Update-first: enhance a semantically similar existing skill in
//     place while the merged body stays under skillUpdateFirstMaxBytes;
//     fall through to a new skill once the threshold is exceeded.
//  4. Render skill markdown via RenderSkillMarkdown.
//  5. Release b.mu → WikiWorker.Enqueue → re-acquire b.mu.
//  6. Re-check dedup (race window).
//  7. Append to b.skills (status active, auto-assigned to the office roster)
//     and persist.
func (b *Broker) writeCompiledSkillLocked(spec teamSkill) (*teamSkill, error) {
	// --- Step 1: Validate ---
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return nil, fmt.Errorf("writeCompiledSkillLocked: name is required")
	}
	if strings.TrimSpace(spec.Description) == "" {
		return nil, fmt.Errorf("writeCompiledSkillLocked: description is required for skill %q", name)
	}
	slug := skillSlug(name)
	if !skillSlugRegex.MatchString(slug) {
		return nil, fmt.Errorf("writeCompiledSkillLocked: slug %q does not match ^[a-z0-9][a-z0-9-]*$", slug)
	}

	// --- Step 2: System-author check ---
	createdBy := strings.TrimSpace(spec.CreatedBy)
	if !skillSystemAuthors[createdBy] {
		// Non-system author must be a registered team member.
		if b.findMemberLocked(createdBy) == nil {
			return nil, fmt.Errorf("writeCompiledSkillLocked: created_by %q is not a registered team member", createdBy)
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
			slog.Info("skill_compile: backfilled source_article on existing dedup",
				"name", name, "source_article", incoming)
			if err := b.saveLocked(); err != nil {
				slog.Warn("skill_compile: saveLocked after source_article backfill failed",
					"name", name, "err", err)
			}
		} else {
			slog.Debug("writeCompiledSkillLocked: skill already exists, skipping",
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

		// Update-first (core-loop step 7.3): if the candidate has substantive
		// new content, enhance the existing skill instead of creating a near
		// duplicate — as long as the merged body stays under the
		// skillUpdateFirstMaxBytes threshold.
		thresholdExceeded := false
		if candidateContent != "" && candidateContent != existingContent {
			enhanced, enhErr := b.enhanceSkillLocked(best.Skill.Name, candidateContent, spec.Description, slug)
			if enhErr == nil && enhanced != nil {
				atomic.AddInt64(&b.skillCompileMetrics.SkillEnhancementsTotal, 1)
				slog.Info("writeCompiledSkillLocked: enhanced existing skill",
					"candidate", name, "existing", best.Skill.Name,
					"tier", best.Tier)
				return enhanced, nil
			}
			if errors.Is(enhErr, errSkillEnhanceTooLarge) {
				// The existing skill has hit the update-first size threshold.
				// Per the spec, the candidate now becomes a NEW skill instead
				// of growing the existing one further — fall through to the
				// create path below.
				thresholdExceeded = true
				slog.Info("writeCompiledSkillLocked: update-first threshold reached, creating new skill",
					"candidate", name, "existing", best.Skill.Name)
			} else if enhErr != nil {
				// Enhancement failed — surface the error rather than silently
				// dropping the candidate's new content into the dedup discard
				// path. A transient render/wiki failure should be retryable by
				// the caller, not converted into a permanent loss of detail.
				return nil, fmt.Errorf("writeCompiledSkillLocked: enhance %q from candidate %q: %w",
					best.Skill.Name, name, enhErr)
			}
		}

		if !thresholdExceeded {
			// Pure duplicate — record the dedup linkage on the existing skill
			// and persist both the broker state AND the wiki markdown so the
			// on-disk frontmatter stays in sync with related_skills.
			best.Skill.RelatedSkills = appendUnique(best.Skill.RelatedSkills, slug)
			best.Skill.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := b.saveLocked(); err != nil {
				slog.Warn("writeCompiledSkillLocked: saveLocked after dedup linkage failed",
					"name", name, "err", err)
			}
			// Re-render the existing skill's wiki markdown so related_skills
			// (surfaced in frontmatter per skill_frontmatter.go) does not drift
			// from broker state. Failure here is logged but non-fatal: state
			// is already persisted and the dedup decision itself is sound.
			b.rerenderSkillWikiLocked(best.Skill, "dedup-linkage")
			atomic.AddInt64(&b.skillCompileMetrics.SemanticDedupHitsTotal, 1)
			slog.Info("writeCompiledSkillLocked: semantic dedup hit",
				"candidate", name, "existing", best.Skill.Name,
				"tier", best.Tier, "slug_score", best.SlugScore,
				"desc_score", best.DescScore, "embed_cos", best.EmbedCos)
			return best.Skill, nil
		}
	}

	// --- Step 4: Build in-memory struct + render markdown ---
	now := time.Now().UTC().Format(time.RFC3339)
	channel := strings.TrimSpace(spec.Channel)
	if channel == "" {
		channel = "general"
	}
	// Compiled skills go live immediately — there is no proposal/approval
	// state in the compile path (core-loop R5). Humans curate after the
	// fact via the Skills page (archive / restore / edit / per-agent
	// assignment).
	status := strings.TrimSpace(spec.Status)
	if status == "" {
		status = "active"
	}
	title := strings.TrimSpace(spec.Title)
	if title == "" {
		title = name
	}

	// Soft bloat warn (SkillOpt): the body is accepted but logged so the
	// team can see when compilation is producing oversized skills.
	if len(strings.TrimSpace(spec.Content)) > skillCompactnessSoftLimitBytes {
		slog.Warn("writeCompiledSkillLocked: body above compactness soft limit",
			"name", name, "body_bytes", len(spec.Content),
			"soft_limit_bytes", skillCompactnessSoftLimitBytes)
	}

	// Auto-assignment (core-loop step 8): a compiled skill is assigned to
	// the relevant agents at creation so it is loaded into their system
	// context on the next prompt build. Until a finer relevance signal
	// exists, "relevant" = the whole office roster; the human or CEO can
	// narrow it afterwards via the per-agent enable/disable endpoints.
	ownerAgents := append([]string(nil), spec.OwnerAgents...)
	if len(ownerAgents) == 0 {
		ownerAgents = b.allMemberSlugsLocked()
	}

	b.counter++
	sk := teamSkill{
		ID:                  fmt.Sprintf("skill-%s", slug),
		Name:                name,
		Title:               title,
		Description:         strings.TrimSpace(spec.Description),
		Content:             strings.TrimSpace(spec.Content),
		CreatedBy:           createdBy,
		OwnerAgents:         ownerAgents,
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
		slog.Warn("writeCompiledSkillLocked: caution verdict allowed under trust",
			"name", name, "trust", string(trust), "summary", scan.Summary)
	}

	// Render the markdown to write to the wiki.
	mdBytes, err := RenderSkillMarkdown(fm, sk.Content)
	if err != nil {
		return nil, fmt.Errorf("writeCompiledSkillLocked: render markdown for %q: %w", name, err)
	}

	wikiPath := "team/skills/" + slug + ".md"
	commitMsg := "archivist: compile skill " + slug

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
			slog.Warn("writeCompiledSkillLocked: WikiWorker.Enqueue failed",
				"slug", slug, "err", err)
			return nil, fmt.Errorf("writeCompiledSkillLocked: wiki enqueue for %q: %w", name, err)
		}
	}

	// --- Step 6: Re-acquire and re-check dedup (race window) ---
	b.mu.Lock()
	if existing := b.findSkillByNameLocked(name); existing != nil {
		slog.Debug("writeCompiledSkillLocked: skill created concurrently, returning existing",
			"name", name, "sha", sha)
		return existing, nil
	}

	// --- Step 7: Commit to in-memory index and persist ---
	b.skills = append(b.skills, sk)
	b.appendActionLocked("skill_update", "office", channel, createdBy,
		truncateSummary(sk.Title+" [compiled]", 140), sk.ID)
	if err := b.saveLocked(); err != nil {
		slog.Warn("writeCompiledSkillLocked: saveLocked failed",
			"name", name, "err", err)
	}

	result := &b.skills[len(b.skills)-1]
	slog.Info("writeCompiledSkillLocked: compiled skill created",
		"name", name, "slug", slug, "created_by", createdBy, "sha", sha,
		"owner_agents", len(result.OwnerAgents))
	return result, nil
}

// allMemberSlugsLocked returns every registered office member slug, sorted
// for deterministic OwnerAgents assignment. Caller must hold b.mu.
func (b *Broker) allMemberSlugsLocked() []string {
	out := make([]string, 0, len(b.members))
	for _, m := range b.members {
		slug := strings.TrimSpace(m.Slug)
		if slug == "" {
			continue
		}
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// enhanceSkillLocked updates an existing skill's content and description with
// new details from a candidate. The caller MUST hold b.mu on entry. The method
// uses the same deadlock-safe unlock/relock pattern as writeCompiledSkillLocked
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

	// Update-first threshold (core-loop step 7.3): growing an existing
	// skill's scope is preferred over creating a new one, but only while
	// the merged body stays under the file-size threshold. Past it, the
	// caller falls back to creating a new skill.
	if len(strings.TrimSpace(updated.Content)) > skillUpdateFirstMaxBytes {
		return nil, fmt.Errorf("enhanceSkillLocked: %q: %w (merged %d > %d bytes)",
			existingName, errSkillEnhanceTooLarge, len(updated.Content), skillUpdateFirstMaxBytes)
	}

	// Protected-invariant gate (SkillOpt §3): every <!-- INVARIANT-START -->
	// block in the previous body MUST survive the merge verbatim. Step-level
	// edits are not allowed to mutate or drop them. Reject the enhancement
	// here rather than down at the wiki write so we don't ship a broken
	// in-memory state if Enqueue fails afterward.
	if err := verifySkillInvariantsPreserved(sk.Content, updated.Content); err != nil {
		return nil, fmt.Errorf("enhanceSkillLocked: %w", err)
	}

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
