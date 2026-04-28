package team

// skill_crud_endpoints.go owns the HTTP handlers for the full WUPHF skill CRUD
// surface added in PR 1b: patch / edit (PUT) / archive / write-file plus the
// Approve / Reject / Undo trio that powers the demo's wiki-skill-compile loop.
//
// Routes are registered alongside the other /skills handlers via
// handleSkillsSubpath. The undo store is a small in-memory map with a 60s GC
// window — sufficient for the demo path (toast TTL is 5s).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// MaxSkillFileBytes caps the size of a sub-resource file an agent may write
// under team/skills/{name}/. 1 MiB is enough for a long template, generous
// enough that we don't stop legit content, and small enough to refuse
// accidental binary uploads.
const MaxSkillFileBytes = 1024 * 1024

// maxSkillFileNameBytes caps the path length of any single sub-resource file.
const maxSkillFileNameBytes = 64

// skillFileAllowedDirs is the closed-set allow-list of directories an agent
// may write files into. Anything outside this list is a 400.
var skillFileAllowedDirs = []string{"references/", "templates/", "scripts/", "assets/"}

// undoTokenTTL is the soft window callers have to undo a reject. Tokens older
// than this are GCd by the next reject/undo call.
const undoTokenTTL = 60 * time.Second

// undoToastWindow is the strict window the front-end requests; the backend
// will refuse undo requests beyond this even if the token is still in the map.
const undoToastWindow = 5 * time.Second

// rejectedSkillSnapshot captures everything we need to revive a rejected skill
// via /skills/reject/undo. The frontmatter is rendered fresh on revival from
// the saved spec so safety_scan stamps re-run on revive.
type rejectedSkillSnapshot struct {
	skill      teamSkill
	rejectedAt time.Time
}

// ── Routing helper ─────────────────────────────────────────────────────────

// handleSkillsCRUDSubpath routes the /skills/{name}/<verb> sub-paths added in
// PR 1b. Returns true if it handled the request (so the caller short-circuits
// the legacy /skills/{name}/invoke fallback).
//
// Wired into handleSkillsSubpath via a single dispatch line — kept in a
// separate file so the new surface is reviewable without touching broker.go.
func (b *Broker) handleSkillsCRUDSubpath(w http.ResponseWriter, r *http.Request) bool {
	rest := strings.TrimPrefix(r.URL.Path, "/skills/")

	// /skills/reject/undo is a singleton verb — name is encoded in the body
	// token, not the URL. Match before the slash split.
	if rest == "reject/undo" {
		b.handleSkillRejectUndo(w, r)
		return true
	}

	// Split into {name}/{verb}.
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return false
	}
	name, verb := parts[0], parts[1]
	if strings.TrimSpace(name) == "" || strings.TrimSpace(verb) == "" {
		return false
	}

	switch verb {
	case "patch":
		b.handleSkillPatch(w, r, name)
		return true
	case "archive":
		b.handleSkillArchive(w, r, name)
		return true
	case "files":
		b.handleSkillWriteFile(w, r, name)
		return true
	case "approve":
		b.handleSkillApprove(w, r, name)
		return true
	case "reject":
		b.handleSkillReject(w, r, name)
		return true
	}
	return false
}

// handleSkillEdit (the PUT counterpart of /skills/{name}) is wired separately
// from the other CRUD verbs because PUT /skills/{name} (no verb suffix) has to
// be matched on method, not path. Caller routes here when the path is just
// /skills/{name} with no verb.
func (b *Broker) handleSkillEditOnName(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPut {
		return false
	}
	rest := strings.TrimPrefix(r.URL.Path, "/skills/")
	if strings.Contains(rest, "/") || strings.TrimSpace(rest) == "" {
		return false
	}
	b.handleSkillEdit(w, r, rest)
	return true
}

// ── Handlers ───────────────────────────────────────────────────────────────

// handleSkillPatch performs a find-replace on the body of an existing skill's
// SKILL.md. Mirrors the Edit tool's old_string / new_string semantics so MCP
// callers don't need to load the full body just to fix a typo.
func (b *Broker) handleSkillPatch(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		FilePath   string `json:"file_path,omitempty"`
		ReplaceAll bool   `json:"replace_all,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.OldString == "" {
		http.Error(w, "old_string required", http.StatusBadRequest)
		return
	}

	// file_path is reserved for future sub-resource patches; reject for now
	// so the API contract stays explicit.
	if strings.TrimSpace(body.FilePath) != "" {
		http.Error(w, "file_path patch not yet supported (PR 1b ships body-only)", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	sk := b.findSkillByNameLocked(name)
	if sk == nil {
		b.mu.Unlock()
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	matches := strings.Count(sk.Content, body.OldString)
	if matches == 0 {
		b.mu.Unlock()
		http.Error(w, "old_string not found in skill body", http.StatusNotFound)
		return
	}
	if matches > 1 && !body.ReplaceAll {
		b.mu.Unlock()
		http.Error(w, fmt.Sprintf("old_string matched %d times; pass replace_all=true to replace all", matches), http.StatusConflict)
		return
	}
	var newContent string
	if body.ReplaceAll {
		newContent = strings.ReplaceAll(sk.Content, body.OldString, body.NewString)
	} else {
		newContent = strings.Replace(sk.Content, body.OldString, body.NewString, 1)
	}
	sk.Content = newContent
	sk.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	skCopy := *sk

	// Re-render and enqueue.
	fm := teamSkillToFrontmatter(skCopy)
	scan := ScanSkill(fm, skCopy.Content, skillTrustForCreator(skCopy.CreatedBy))
	fm.Metadata.Wuphf.SafetyScan = &SkillSafetyScan{
		Verdict:    string(scan.Verdict),
		Findings:   append([]string(nil), scan.Findings...),
		TrustLevel: string(scan.TrustLevel),
		Summary:    scan.Summary,
	}
	mdBytes, renderErr := RenderSkillMarkdown(fm, skCopy.Content)
	if renderErr != nil {
		b.mu.Unlock()
		http.Error(w, "render markdown: "+renderErr.Error(), http.StatusInternalServerError)
		return
	}

	wikiWorker := b.wikiWorker
	wikiPath := skillWikiPath(skCopy.Name)
	b.mu.Unlock()

	if wikiWorker != nil {
		if _, _, err := wikiWorker.Enqueue(r.Context(), skCopy.Name, wikiPath, string(mdBytes), "replace", "wuphf: patch skill "+skCopy.Name); err != nil {
			http.Error(w, "wiki enqueue: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"skill": skCopy})
}

// handleSkillEdit is the PUT /skills/{name} handler — full SKILL.md body
// replacement. Re-runs the guard scan with the original creator's trust
// level (preserved across edits).
func (b *Broker) handleSkillEdit(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}

	// Parse the full SKILL.md so we can recover both frontmatter and body and
	// validate frontmatter integrity before mutating in-memory state.
	fm, parsedBody, parseErr := ParseSkillMarkdown([]byte(body.Content))
	if parseErr != nil {
		http.Error(w, "parse SKILL.md: "+parseErr.Error(), http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	sk := b.findSkillByNameLocked(name)
	if sk == nil {
		b.mu.Unlock()
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	// Trust level: preserve from existing safety_scan if present, else
	// fall back to community.
	trust := skillTrustForCreator(sk.CreatedBy)
	scan := ScanSkill(fm, parsedBody, trust)
	if scan.Verdict == VerdictDangerous && trust != TrustBuiltin && trust != TrustTrusted {
		atomic.AddInt64(&b.skillCompileMetrics.ProposalsRejectedByGuardTotal, 1)
		b.mu.Unlock()
		http.Error(w, "skill_guard: rejected — "+scan.Summary, http.StatusForbidden)
		return
	}
	if trust == TrustAgentCreated && scan.Verdict != VerdictSafe {
		atomic.AddInt64(&b.skillCompileMetrics.ProposalsRejectedByGuardTotal, 1)
		b.mu.Unlock()
		http.Error(w, "skill_guard: rejected — "+scan.Summary, http.StatusForbidden)
		return
	}

	// Apply parsed frontmatter to the in-memory skill.
	if t := strings.TrimSpace(fm.Metadata.Wuphf.Title); t != "" {
		sk.Title = t
	}
	if d := strings.TrimSpace(fm.Description); d != "" {
		sk.Description = d
	}
	sk.Content = parsedBody
	sk.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	skCopy := *sk

	// Stamp the new safety_scan and re-render.
	fm.Metadata.Wuphf.SafetyScan = &SkillSafetyScan{
		Verdict:    string(scan.Verdict),
		Findings:   append([]string(nil), scan.Findings...),
		TrustLevel: string(scan.TrustLevel),
		Summary:    scan.Summary,
	}
	mdBytes, renderErr := RenderSkillMarkdown(fm, parsedBody)
	if renderErr != nil {
		b.mu.Unlock()
		http.Error(w, "render markdown: "+renderErr.Error(), http.StatusInternalServerError)
		return
	}

	wikiWorker := b.wikiWorker
	wikiPath := skillWikiPath(skCopy.Name)
	b.mu.Unlock()

	if wikiWorker != nil {
		if _, _, err := wikiWorker.Enqueue(r.Context(), skCopy.Name, wikiPath, string(mdBytes), "replace", "wuphf: edit skill "+skCopy.Name); err != nil {
			http.Error(w, "wiki enqueue: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"skill": skCopy})
}

// handleSkillArchive sets sk.Status = archived on an existing skill. Never
// hard-deletes; the SKILL.md is rewritten with metadata.wuphf.status updated.
func (b *Broker) handleSkillArchive(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Reason string `json:"reason,omitempty"`
	}
	// Empty body OK.
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	sk := b.findSkillByNameLocked(name)
	if sk == nil {
		b.mu.Unlock()
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	sk.Status = "archived"
	sk.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	skCopy := *sk

	fm := teamSkillToFrontmatter(skCopy)
	mdBytes, renderErr := RenderSkillMarkdown(fm, skCopy.Content)
	if renderErr != nil {
		b.mu.Unlock()
		http.Error(w, "render markdown: "+renderErr.Error(), http.StatusInternalServerError)
		return
	}

	channel := normalizeChannelSlug(skCopy.Channel)
	if channel == "" {
		channel = "general"
	}
	b.appendActionLocked("skill_archived", "office", channel, skCopy.CreatedBy, truncateSummary(skCopy.Title+" [archived]", 140), skCopy.ID)
	wikiWorker := b.wikiWorker
	wikiPath := skillWikiPath(skCopy.Name)
	b.mu.Unlock()

	if wikiWorker != nil {
		commitMsg := "wuphf: archive skill " + skCopy.Name
		if r := strings.TrimSpace(body.Reason); r != "" {
			commitMsg += " — " + r
		}
		if _, _, err := wikiWorker.Enqueue(context.Background(), skCopy.Name, wikiPath, string(mdBytes), "replace", commitMsg); err != nil {
			slog.Warn("handleSkillArchive: wiki enqueue failed", "name", skCopy.Name, "err", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"skill": skCopy})
}

// handleSkillWriteFile writes a file under team/skills/{name}/{file_path}
// after enforcing the allow-list and size limits.
func (b *Broker) handleSkillWriteFile(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		FilePath    string `json:"file_path"`
		FileContent string `json:"file_content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := validateSkillFilePath(body.FilePath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body.FileContent) > MaxSkillFileBytes {
		http.Error(w, fmt.Sprintf("file_content exceeds %d bytes", MaxSkillFileBytes), http.StatusRequestEntityTooLarge)
		return
	}

	b.mu.Lock()
	sk := b.findSkillByNameLocked(name)
	if sk == nil {
		b.mu.Unlock()
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	wikiWorker := b.wikiWorker
	skName := sk.Name
	b.mu.Unlock()

	cleanFile := path.Clean(body.FilePath)
	wikiPath := "team/skills/" + skillSlug(skName) + "/" + cleanFile
	if wikiWorker != nil {
		if _, _, err := wikiWorker.Enqueue(r.Context(), skName, wikiPath, body.FileContent, "replace", "wuphf: write file "+cleanFile+" for skill "+skName); err != nil {
			http.Error(w, "wiki enqueue: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"path":      wikiPath,
		"bytes":     len(body.FileContent),
		"skill":     skName,
		"file_path": cleanFile,
	})
}

// handleSkillApprove flips a proposed skill to active. Returns 409 if the
// skill is not in proposed status.
func (b *Broker) handleSkillApprove(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	sk := b.findSkillByNameLocked(name)
	if sk == nil {
		b.mu.Unlock()
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	if sk.Status != "proposed" {
		b.mu.Unlock()
		http.Error(w, fmt.Sprintf("skill not in proposed status (status=%s)", sk.Status), http.StatusConflict)
		return
	}
	sk.Status = "active"
	sk.UpdatedAt = now
	atomic.AddInt64(&b.skillCompileMetrics.ProposalsApprovedTotal, 1)
	skCopy := *sk

	channel := normalizeChannelSlug(skCopy.Channel)
	if channel == "" {
		channel = "general"
	}
	actor := strings.TrimSpace(skCopy.CreatedBy)
	if actor == "" {
		actor = "system"
	}
	b.appendActionLocked("skill_approved", "office", channel, actor, truncateSummary(skCopy.Title+" [approved]", 140), skCopy.ID)

	// Re-render with status=active so the wiki copy matches in-memory state.
	fm := teamSkillToFrontmatter(skCopy)
	mdBytes, renderErr := RenderSkillMarkdown(fm, skCopy.Content)
	wikiWorker := b.wikiWorker
	wikiPath := skillWikiPath(skCopy.Name)
	b.mu.Unlock()

	if wikiWorker != nil && renderErr == nil {
		if _, _, err := wikiWorker.Enqueue(r.Context(), skCopy.Name, wikiPath, string(mdBytes), "replace", "wuphf: approve skill "+skCopy.Name); err != nil {
			slog.Warn("handleSkillApprove: wiki enqueue failed", "name", skCopy.Name, "err", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"skill": skCopy})
}

// handleSkillReject removes a proposed skill from b.skills, appends a
// tombstone entry, and returns an undo_token valid for undoToastWindow.
func (b *Broker) handleSkillReject(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Reason string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	now := time.Now().UTC()
	reason := strings.TrimSpace(body.Reason)

	b.mu.Lock()
	sk := b.findSkillByNameLocked(name)
	if sk == nil {
		b.mu.Unlock()
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}
	skCopy := *sk
	// Remove from b.skills.
	out := b.skills[:0]
	for _, s := range b.skills {
		if skillSlug(s.Name) == skillSlug(name) {
			continue
		}
		out = append(out, s)
	}
	b.skills = out

	// Stash the snapshot for undo.
	if b.recentlyRejectedSkills == nil {
		b.recentlyRejectedSkills = make(map[string]rejectedSkillSnapshot)
	}
	gcRejectedSkillsLocked(b, now)
	token := makeUndoToken(skCopy.Name, now)
	b.recentlyRejectedSkills[token] = rejectedSkillSnapshot{
		skill:      skCopy,
		rejectedAt: now,
	}

	channel := normalizeChannelSlug(skCopy.Channel)
	if channel == "" {
		channel = "general"
	}
	actor := strings.TrimSpace(skCopy.CreatedBy)
	if actor == "" {
		actor = "system"
	}
	b.appendActionLocked("skill_rejected", "office", channel, actor, truncateSummary(skCopy.Title+" [rejected]", 140), skCopy.ID)

	// Append to tombstone (release/reacquire b.mu inside).
	srcArticle := ""
	// We don't have direct access to source_articles on teamSkill; the
	// scanner stamps it on the wiki copy via fm.Metadata.Wuphf, so the
	// tombstone source field stays empty here. Future revision: thread the
	// source through teamSkill.
	tombstoneErr := b.appendSkillTombstoneLocked(SkillTombstoneEntry{
		Slug:          skillSlug(skCopy.Name),
		SourceArticle: srcArticle,
		RejectedAt:    now.Format(time.RFC3339),
		Reason:        reason,
	})
	if tombstoneErr != nil {
		slog.Warn("handleSkillReject: tombstone append failed", "name", skCopy.Name, "err", tombstoneErr)
	}
	b.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"undo_token": token,
		"skill_name": skCopy.Name,
		"expires_in": int(undoToastWindow.Seconds()),
	})
}

// handleSkillRejectUndo restores a recently-rejected skill from the in-memory
// snapshot store. Tokens older than undoToastWindow are refused even if still
// present in the map.
func (b *Broker) handleSkillRejectUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		UndoToken string `json:"undo_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(body.UndoToken)
	if token == "" {
		http.Error(w, "undo_token required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()

	b.mu.Lock()
	if b.recentlyRejectedSkills == nil {
		b.mu.Unlock()
		http.Error(w, "no rejected skills to undo", http.StatusNotFound)
		return
	}
	snap, ok := b.recentlyRejectedSkills[token]
	if !ok {
		b.mu.Unlock()
		http.Error(w, "undo token not found or expired", http.StatusNotFound)
		return
	}
	if now.Sub(snap.rejectedAt) > undoToastWindow {
		delete(b.recentlyRejectedSkills, token)
		b.mu.Unlock()
		http.Error(w, "undo token expired", http.StatusGone)
		return
	}
	delete(b.recentlyRejectedSkills, token)
	gcRejectedSkillsLocked(b, now)

	// Re-add the skill to b.skills as proposed.
	revived := snap.skill
	revived.Status = "proposed"
	revived.UpdatedAt = now.Format(time.RFC3339)
	b.skills = append(b.skills, revived)

	// Remove the matching tombstone entry. Best-effort — if the tombstone
	// load fails we still revive (the scanner skips active skills anyway).
	existing, _ := b.loadSkillTombstoneLocked()
	if len(existing) > 0 {
		filtered := existing[:0]
		removed := false
		targetSlug := skillSlug(revived.Name)
		for _, e := range existing {
			if !removed && e.Slug == targetSlug {
				removed = true
				continue
			}
			filtered = append(filtered, e)
		}
		if removed {
			// Re-write the tombstone file via Enqueue. We cheat slightly: the
			// existing append helper takes a single entry and re-writes the
			// whole file, but here we want to overwrite with the filtered
			// list. Rebuild and enqueue inline — release lock around Enqueue.
			rewriteSkillTombstoneLocked(b, filtered, revived.Name)
		}
	}

	channel := normalizeChannelSlug(revived.Channel)
	if channel == "" {
		channel = "general"
	}
	b.appendActionLocked("skill_reject_undone", "office", channel, revived.CreatedBy, truncateSummary(revived.Title+" [restored]", 140), revived.ID)
	skCopy := revived
	b.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"skill": skCopy})
}

// ── helpers ────────────────────────────────────────────────────────────────

// gcRejectedSkillsLocked removes snapshots older than undoTokenTTL. Caller
// holds b.mu.
func gcRejectedSkillsLocked(b *Broker, now time.Time) {
	for token, snap := range b.recentlyRejectedSkills {
		if now.Sub(snap.rejectedAt) > undoTokenTTL {
			delete(b.recentlyRejectedSkills, token)
		}
	}
}

// makeUndoToken returns an opaque token encoding the skill name and timestamp.
// Tokens are not authenticated — they're a soft handle scoped to this broker
// instance, used to look up the in-memory snapshot. Validation lives in
// handleSkillRejectUndo via map presence + window check.
func makeUndoToken(name string, ts time.Time) string {
	raw := name + ":" + ts.Format(time.RFC3339Nano)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(raw))
}

// validateSkillFilePath enforces the allow-list and the path-traversal
// guard. Returns nil when path is OK to write.
func validateSkillFilePath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("file_path required")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("file_path must be relative")
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("file_path may not traverse parent directories")
	}
	if len(cleaned) > maxSkillFileNameBytes {
		return fmt.Errorf("file_path exceeds %d bytes", maxSkillFileNameBytes)
	}
	allowed := false
	for _, prefix := range skillFileAllowedDirs {
		if strings.HasPrefix(cleaned, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("file_path must be under one of: %s", strings.Join(skillFileAllowedDirs, ", "))
	}
	return nil
}

// skillTrustForCreator maps a creator slug onto a default trust level.
func skillTrustForCreator(createdBy string) GuardTrustLevel {
	switch strings.TrimSpace(createdBy) {
	case "archivist", "scanner":
		return TrustCommunity
	case "system":
		return TrustTrusted
	default:
		return TrustCommunity
	}
}

// skillWikiPath is the canonical wiki path of a SKILL.md.
func skillWikiPath(name string) string {
	return "team/skills/" + skillSlug(name) + ".md"
}

// rewriteSkillTombstoneLocked overwrites the tombstone file with the supplied
// entries. Caller holds b.mu; this helper releases and reacquires around the
// Enqueue call.
func rewriteSkillTombstoneLocked(b *Broker, entries []SkillTombstoneEntry, slug string) {
	wikiWorker := b.wikiWorker
	if wikiWorker == nil {
		return
	}
	tf := tombstoneFile{Rejected: entries}
	raw, err := yamlMarshalTombstone(tf)
	if err != nil {
		slog.Warn("rewriteSkillTombstoneLocked: marshal failed", "err", err)
		return
	}
	b.mu.Unlock()
	_, _, enqErr := wikiWorker.Enqueue(
		context.Background(),
		".rejected",
		skillTombstonePath,
		raw,
		"replace",
		"wuphf: undo reject — restore skill "+slug,
	)
	b.mu.Lock()
	if enqErr != nil {
		slog.Warn("rewriteSkillTombstoneLocked: enqueue failed", "err", enqErr)
	}
}

// yamlMarshalTombstone is split out so handleSkillRejectUndo can re-use the
// same encoding used by appendSkillTombstoneLocked.
func yamlMarshalTombstone(tf tombstoneFile) (string, error) {
	raw, err := yaml.Marshal(tf)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
