package team

// broker_onboarding_scan.go — async website scan for PhaseScan.
//
// runScanPhase is launched by advancePhase(PhaseScan) in its own goroutine
// so the chat can keep flowing while operations.SeedCompanyContext does the
// HTTP fetch + LLM extraction (typically 5-30 seconds). It then:
//
//   1. Posts a status-update chip (status="done" or "failed").
//   2. Stagger-posts one "✓ <article>" message per wiki page written —
//      the chat-mode equivalent of the old Step3bAnalysis reveal animation.
//   3. Persists FormAnswers.ScanComplete = true.
//   4. Auto-advances the phase machine to PhaseBlueprint (the user is
//      sitting on a read-only chip; no other actor will advance them).
//
// All message writes go through appendMessageLocked under b.mu so the
// SSE fanout to the OnboardingChat client stays serialized with the
// rest of the broker.

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
)

// scanRevealStagger is the delay between successive "✓ <article>" bubbles
// during the post-scan reveal. Short enough to feel responsive, long
// enough that each line registers as a discrete moment.
const scanRevealStagger = 180 * time.Millisecond

// scanTimeout bounds the whole scan-and-reveal goroutine. The underlying
// fetch+LLM call has its own internal budget; this is a hard ceiling so
// a stuck scan does not strand the user on a "scanning…" chip forever.
const scanTimeout = 90 * time.Second

// runScanPhase runs operations.SeedCompanyContext, posts the result
// messages into the CEO DM, and auto-advances to PhaseBlueprint.
//
// Safe to call without holding b.mu. Acquires the lock per write.
func (b *Broker) runScanPhase(dmSlug string) {
	// Re-load state so we read the latest FormAnswers.WebsiteURL rather
	// than relying on a snapshot from advancePhase's caller. The user
	// committed website_url via POST /onboarding/answer just before the
	// transition fired; that write is durable by the time we get here.
	s, err := onboarding.Load()
	if err != nil {
		// Don't strand the user on a spinning "Scanning…" chip. Post a
		// failed chip with recovery CTAs — the load failure is local to
		// our state file and skipping is the only safe path forward.
		log.Printf("onboarding scan: load state: %v", err)
		b.postScanChipFailure(dmSlug, "",
			"Couldn’t read onboarding state.",
			"Local state file is unreadable; skip the scan to continue.")
		return
	}
	websiteURL := strings.TrimSpace(s.FormAnswers.WebsiteURL)
	if websiteURL == "" {
		// Defensive: the transition guard should have routed an empty
		// website straight to PhaseBlueprint. If we got here anyway,
		// just advance and skip the scan.
		b.advanceAfterScan(websiteURL, false)
		return
	}

	home := strings.TrimSpace(config.RuntimeHomeDir())
	wikiRoot := ""
	if home != "" {
		wikiRoot = filepath.Join(home, ".wuphf", "wiki")
	}

	input := operations.CompanySeedInput{
		WebsiteURL:  websiteURL,
		CompanyName: s.FormAnswers.CompanyName,
		OwnerName:   s.FormAnswers.OwnerName,
		OwnerRole:   s.FormAnswers.OwnerRole,
		Completer:   brokerCompleter{},
		WikiRoot:    wikiRoot,
	}

	ctx, cancel := context.WithTimeout(b.lifecycleCtx, scanTimeout)
	defer cancel()
	result, scanErr := operations.SeedCompanyContext(ctx, input)

	success := scanErr == nil && result != nil && !result.NeedsRetry
	if !success {
		if scanErr != nil {
			log.Printf("onboarding scan: %v", scanErr)
		}
		reason := scanFailureReason(scanErr, result)
		// Surface the reason inline on the failed chip so the user knows
		// what went wrong, and leave them in PhaseScan with recovery CTAs
		// rather than silently advancing to PhaseBlueprint. (#934)
		label := fmt.Sprintf("Couldn’t read %s.", displayURL(websiteURL))
		b.postScanChipFailure(dmSlug, websiteURL, label, reason)
		return
	}

	// Re-check phase before each side-effect: if a concurrent resume/tab has
	// already moved onboarding past PhaseScan, don't append stale "Wiki
	// updated ✓" / "✓ <article>" messages on top of the new phase's chat.
	// (CodeRabbit on #911.)
	if !b.phaseStillScan() {
		return
	}

	// SeedCompanyContext writes team/about/{README,owner,company}.md
	// directly to disk via atomicWrite — it does not pass through the
	// WikiWorker, so (*Repo).Commit's per-commit IndexRegen never fires
	// for these files. Regenerate index/all.md here so the post-scan
	// snapshot reflects the README path that always lands. (#941)
	//
	// Use a fresh timeout against the broker lifecycle, not the scan ctx,
	// so a near-deadline scan does not cause regen to silently no-op.
	regenCtx, regenCancel := context.WithTimeout(b.lifecycleCtx, 15*time.Second)
	b.regenWikiIndexAfterSeed(regenCtx, "website scan")
	regenCancel()

	b.postScanChipUpdate(dmSlug, websiteURL, "done", "Wiki updated ✓")

	// Stagger one chat bubble per article written. Each bubble is a plain
	// text CEO message so it renders as a normal CEO line with the ✓ prefix.
	//
	// The labeled `break revealLoop` is load-bearing: a bare `break` inside
	// the select would only exit the select and the for loop would keep
	// posting article bubbles after the goroutine's context was cancelled
	// (caught by staticcheck SA4011 and the CodeRabbit review on #911).
revealLoop:
	for _, article := range result.ArticlesWritten {
		select {
		case <-ctx.Done():
			break revealLoop
		case <-time.After(scanRevealStagger):
		}
		if !b.phaseStillScan() {
			break revealLoop
		}
		b.postScanArticleLine(dmSlug, article)
	}

	b.advanceAfterScan(websiteURL, true)
}

// phaseStillScan reports whether the persisted onboarding state is still in
// PhaseScan. It is called before each post-scan side effect (terminal chip +
// each article reveal line) so a concurrent resume/tab that already moved
// onboarding forward does not get stale "Wiki updated ✓" + "✓ <article>"
// CEO messages appended on top of the new phase's chat. On load failure we
// return false (bail out is safer than posting stale content).
func (b *Broker) phaseStillScan() bool {
	s, err := onboarding.Load()
	if err != nil {
		log.Printf("onboarding scan: phase recheck load: %v", err)
		return false
	}
	return s.Phase == onboarding.PhaseScan
}

// postScanChipUpdate appends a second ceo_scan_chip message into the CEO DM
// reflecting the terminal status of the scan (done | failed). It is a new
// message (not an in-place mutation of the original chip) so the existing
// SSE/append-only message log stays append-only.
//
// Used by the success path only. The failure path takes postScanChipFailure,
// which adds the error_reason field + leaves the user with recovery CTAs.
func (b *Broker) postScanChipUpdate(dmSlug, websiteURL, status, label string) {
	rawPayload := mustMarshalRaw(map[string]interface{}{
		"url":          websiteURL,
		"status":       status,
		"done_label":   label,
		"failed_label": label,
	})
	payload := ceoMessagePayload{
		Kind:              "ceo_scan_chip",
		Content:           label,
		SuggestionID:      "scan-progress-" + urlToSuggestionID(websiteURL) + "-" + status,
		SuggestionPayload: rawPayload,
	}
	sanitized, err := sanitizeCEOPayload(payload)
	if err != nil {
		log.Printf("onboarding scan: sanitize chip update: %v", err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "ceo",
		Channel:   dmSlug,
		Kind:      payload.Kind,
		Content:   payload.Content,
		Payload:   sanitized,
		Tagged:    []string{},
		Timestamp: now,
	})
	if err := b.saveLocked(); err != nil {
		log.Printf("onboarding scan: persist chip update: %v", err)
	}
}

// postScanChipFailure appends a terminal ceo_scan_chip with status="failed",
// surfaces the underlying error reason inline, and leaves the user in
// PhaseScan with recovery CTAs (#934).
//
// Unlike the success path, this does NOT call advanceAfterScan: the frontend
// renders a "Try another URL" CTA (→ POST /onboarding/transition phase=website)
// and a "Skip and continue" CTA (→ POST /onboarding/transition phase=blueprint)
// so the user controls the next step. PendingSuggestion is updated to this
// failed chip so resume re-emits the same recovery UI.
//
// The reason string is the verbatim warning surfaced from operations
// (scanFailureReason); the frontend renders it as plain text under the chip.
func (b *Broker) postScanChipFailure(dmSlug, websiteURL, label, reason string) {
	rawPayload := mustMarshalRaw(map[string]interface{}{
		"url":          websiteURL,
		"status":       "failed",
		"failed_label": label,
		"error_reason": reason,
	})
	payload := ceoMessagePayload{
		Kind:              "ceo_scan_chip",
		Content:           label,
		SuggestionID:      "scan-progress-" + urlToSuggestionID(websiteURL) + "-failed",
		SuggestionPayload: rawPayload,
	}
	sanitized, err := sanitizeCEOPayload(payload)
	if err != nil {
		log.Printf("onboarding scan: sanitize chip failure: %v", err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "ceo",
		Channel:   dmSlug,
		Kind:      payload.Kind,
		Content:   payload.Content,
		Payload:   sanitized,
		Tagged:    []string{},
		Timestamp: now,
	})
	// Persist the failed chip as the pending suggestion so a tab refresh
	// resumes on the same recovery UI rather than re-firing a fresh scan.
	if s, loadErr := onboarding.Load(); loadErr == nil {
		s.PendingSuggestion = &onboarding.Suggestion{
			ID:       payload.SuggestionID,
			Phase:    onboarding.PhaseScan,
			Kind:     payload.Kind,
			Payload:  sanitized,
			IssuedAt: time.Now().UTC(),
		}
		if saveErr := onboarding.Save(s); saveErr != nil {
			log.Printf("onboarding scan: persist failed pending: %v", saveErr)
		}
	} else {
		log.Printf("onboarding scan: load for failed pending: %v", loadErr)
	}
	if err := b.saveLocked(); err != nil {
		log.Printf("onboarding scan: persist chip failure: %v", err)
	}
}

// scanFailureReason returns a short, plain-English description of why the
// website scan failed. It pulls verbatim from operations.CompanySeedResult's
// warnings array when available (e.g. "URL fetch failed: ...",
// "LLM extraction failed: ...") and falls back to the raw error otherwise.
//
// Per #934's failure-mode guidance, we do NOT invent a new typed taxonomy
// here — we surface what the underlying layer already produced.
func scanFailureReason(scanErr error, result *operations.CompanySeedResult) string {
	if result != nil {
		for _, w := range result.Warnings {
			w = strings.TrimSpace(w)
			if w != "" {
				return w
			}
		}
	}
	if scanErr != nil {
		return scanErr.Error()
	}
	return "The scanner returned no readable content."
}

// postScanArticleLine appends a single CEO text bubble announcing one wiki
// article written by the scan. The leading ✓ is the chat-mode equivalent
// of the reveal-check span in the old Step3bAnalysis panel.
func (b *Broker) postScanArticleLine(dmSlug, article string) {
	article = strings.TrimSpace(article)
	if article == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "ceo",
		Channel:   dmSlug,
		Kind:      "text",
		Content:   "✓ " + article,
		Tagged:    []string{},
		Timestamp: now,
	})
	if err := b.saveLocked(); err != nil {
		log.Printf("onboarding scan: persist article line: %v", err)
	}
}

// advanceAfterScan persists ScanComplete + Phase=Blueprint and emits the
// blueprint CEO card. Mirrors handleTransition's invariant: Phase is set
// and saved before advancePhase runs the broker-side emission so a resume
// would re-emit the blueprint card, not the scan chip.
//
// The load-check-save is serialized under b.mu (the same mutex advancePhase
// takes while appending messages and saving state) so a concurrent
// broker-internal write cannot land between our Load and Save and get
// clobbered by the stale snapshot. Addresses CodeRabbit on #911.
//
// b.mu is released before calling b.advancePhase: that function acquires
// b.mu itself, and re-entrant locking on sync.Mutex would deadlock. The
// remaining window between our Save and advancePhase's Save is microseconds
// and not user-actionable — the only card the user could submit during
// PhaseScan is the read-only scan chip, which has no submit handler.
func (b *Broker) advanceAfterScan(websiteURL string, success bool) {
	b.mu.Lock()
	s, err := onboarding.Load()
	if err != nil {
		b.mu.Unlock()
		log.Printf("onboarding scan: load for advance: %v", err)
		return
	}
	// Only auto-advance from PhaseScan — protect against the user already
	// having clicked through some other path during the scan window.
	if s.Phase != onboarding.PhaseScan {
		b.mu.Unlock()
		return
	}
	s.FormAnswers.ScanComplete = success
	s.Phase = onboarding.PhaseBlueprint
	if err := onboarding.Save(s); err != nil {
		b.mu.Unlock()
		log.Printf("onboarding scan: persist blueprint transition: %v", err)
		return
	}
	b.mu.Unlock()

	if err := b.advancePhase(s, onboarding.PhaseBlueprint); err != nil {
		log.Printf("onboarding scan: advance to blueprint: %v", err)
	}
	_ = websiteURL // reserved for future telemetry; keep param for clarity
}
