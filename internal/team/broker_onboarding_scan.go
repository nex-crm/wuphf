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
		log.Printf("onboarding scan: load state: %v", err)
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
		WebsiteURL: websiteURL,
		OwnerName:  s.FormAnswers.OwnerName,
		OwnerRole:  s.FormAnswers.OwnerRole,
		Completer:  brokerCompleter{},
		WikiRoot:   wikiRoot,
	}

	ctx, cancel := context.WithTimeout(b.lifecycleCtx, scanTimeout)
	defer cancel()
	result, scanErr := operations.SeedCompanyContext(ctx, input)

	success := scanErr == nil && result != nil && !result.NeedsRetry
	if !success {
		if scanErr != nil {
			log.Printf("onboarding scan: %v", scanErr)
		}
		b.postScanChipUpdate(dmSlug, websiteURL, "failed",
			fmt.Sprintf("Couldn’t read %s — skipping the scan.", displayURL(websiteURL)))
		b.advanceAfterScan(websiteURL, false)
		return
	}

	b.postScanChipUpdate(dmSlug, websiteURL, "done", "Wiki updated ✓")

	// Stagger one chat bubble per article written. Each bubble is a plain
	// text CEO message so it renders as a normal CEO line with the ✓ prefix.
	for _, article := range result.ArticlesWritten {
		select {
		case <-ctx.Done():
			break
		case <-time.After(scanRevealStagger):
		}
		b.postScanArticleLine(dmSlug, article)
	}

	b.advanceAfterScan(websiteURL, true)
}

// postScanChipUpdate appends a second ceo_scan_chip message into the CEO DM
// reflecting the terminal status of the scan (done | failed). It is a new
// message (not an in-place mutation of the original chip) so the existing
// SSE/append-only message log stays append-only.
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
func (b *Broker) advanceAfterScan(websiteURL string, success bool) {
	s, err := onboarding.Load()
	if err != nil {
		log.Printf("onboarding scan: load for advance: %v", err)
		return
	}
	// Only auto-advance from PhaseScan — protect against the user already
	// having clicked through some other path during the scan window.
	if s.Phase != onboarding.PhaseScan {
		return
	}
	s.FormAnswers.ScanComplete = success
	s.Phase = onboarding.PhaseBlueprint
	if err := onboarding.Save(s); err != nil {
		log.Printf("onboarding scan: persist blueprint transition: %v", err)
		return
	}
	if err := b.advancePhase(s, onboarding.PhaseBlueprint); err != nil {
		log.Printf("onboarding scan: advance to blueprint: %v", err)
	}
	_ = websiteURL // reserved for future telemetry; keep param for clarity
}
