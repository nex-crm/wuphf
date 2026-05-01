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
