package team

// broker_review.go wires the Lane C promotion review surface onto the
// broker — HTTP endpoints for submit / list / get / approve / reject /
// request-changes / resubmit / comment, plus the SSE event publisher and
// the periodic TickExpiry loop.
//
// Route map (registered in broker.go):
//
//	POST /notebook/promote                      — submit a new promotion
//	GET  /review/list?scope={all|mine|<slug>}   — list reviews
//	GET  /review/{id}                           — single review + thread
//	POST /review/{id}/approve                   — reviewer approves
//	POST /review/{id}/request-changes           — reviewer asks for changes
//	POST /review/{id}/reject                    — author withdraws
//	POST /review/{id}/resubmit                  — author re-submits
//	POST /review/{id}/comment                   — add a comment
//
// SSE event `review:state_change` is fanned out to all subscribers after
// every transition (including TickExpiry auto-advances).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// ReviewStateChangeEvent is the SSE payload for every promotion transition.
// Kept narrow on purpose — the frontend re-fetches the full review to get
// comment threads and such.
type ReviewStateChangeEvent struct {
	ID        string         `json:"id"`
	OldState  PromotionState `json:"old_state"`
	NewState  PromotionState `json:"new_state"`
	ActorSlug string         `json:"actor_slug"`
	Timestamp string         `json:"timestamp"`
}

type reviewCommentResponse struct {
	ID         string `json:"id"`
	AuthorSlug string `json:"author_slug"`
	BodyMD     string `json:"body_md"`
	TS         string `json:"ts"`
}

type reviewItemResponse struct {
	ID               string                  `json:"id"`
	AgentSlug        string                  `json:"agent_slug"`
	EntrySlug        string                  `json:"entry_slug"`
	EntryTitle       string                  `json:"entry_title"`
	ProposedWikiPath string                  `json:"proposed_wiki_path"`
	Excerpt          string                  `json:"excerpt"`
	ReviewerSlug     string                  `json:"reviewer_slug"`
	State            PromotionState          `json:"state"`
	SubmittedTS      string                  `json:"submitted_ts"`
	UpdatedTS        string                  `json:"updated_ts"`
	Comments         []reviewCommentResponse `json:"comments"`
}

// SubscribeReviewEvents returns a channel of review state-change events
// plus an unsubscribe func. Used by the web UI SSE stream.
func (b *Broker) SubscribeReviewEvents(buffer int) (<-chan ReviewStateChangeEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reviewSubscribers == nil {
		b.reviewSubscribers = make(map[int]chan ReviewStateChangeEvent)
	}
	id := b.nextSubscriberID
	b.nextSubscriberID++
	ch := make(chan ReviewStateChangeEvent, buffer)
	b.reviewSubscribers[id] = ch
	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.reviewSubscribers[id]; ok {
			delete(b.reviewSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// publishReviewEvent fans out one state-change event.
func (b *Broker) publishReviewEvent(evt ReviewStateChangeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.reviewSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// ReviewLog returns the active ReviewLog or nil when the markdown backend
// is not wired. Exposed for tests and for future admin tooling.
func (b *Broker) ReviewLog() *ReviewLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reviewLog
}

// SetReviewerResolver lets the launcher inject the active blueprint's
// ResolveReviewer when the markdown backend comes online. Safe to call
// before ensureReviewLog — the resolver is captured at ReviewLog
// construction time.
func (b *Broker) SetReviewerResolver(resolver ReviewerResolver) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reviewResolver = resolver
	if b.reviewLog != nil {
		b.reviewLog.resolver = resolver
	}
}

// ensureReviewLog initializes the on-disk JSONL + in-memory cache. Idempotent.
// Requires the wiki worker to be up so the reviews dir lands inside
// ~/.wuphf/wiki/.reviews/.
func (b *Broker) ensureReviewLog() {
	b.mu.Lock()
	if b.reviewLog != nil {
		b.mu.Unlock()
		return
	}
	worker := b.wikiWorker
	resolver := b.reviewResolver
	b.mu.Unlock()
	if worker == nil {
		return
	}

	path := ReviewLogPath(worker.Repo().Root())
	rl, err := NewReviewLog(path, resolver, nil)
	if err != nil {
		log.Printf("review log: init failed: %v", err)
		return
	}
	b.mu.Lock()
	b.reviewLog = rl
	b.mu.Unlock()
}

// startReviewExpiryLoop runs a background goroutine that fires TickExpiry
// every 10 minutes. Auto-transitions are broadcast via the SSE event feed.
func (b *Broker) startReviewExpiryLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.runReviewTick()
			}
		}
	}()
}

// runReviewTick fires one TickExpiry pass + publishes resulting SSE events.
// Extracted so tests can invoke it deterministically.
func (b *Broker) runReviewTick() {
	rl := b.ReviewLog()
	if rl == nil {
		return
	}
	transitions := rl.TickExpiry(time.Now())
	for _, t := range transitions {
		b.publishReviewEvent(ReviewStateChangeEvent{
			ID:        t.PromotionID,
			OldState:  t.OldState,
			NewState:  t.NewState,
			ActorSlug: t.Actor,
			Timestamp: t.Timestamp.UTC().Format(time.RFC3339),
		})
	}
}

// handleNotebookPromote is the submission entry point called by the MCP
// tool. Validates the request, creates a new pending promotion, and
// publishes a review:state_change SSE event.
//
//	POST /notebook/promote
//	{ "my_slug": "...", "source_path": "...", "target_wiki_path": "...",
//	  "rationale": "...", "reviewer_slug": "..." (optional override) }
func (b *Broker) handleNotebookPromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rl := b.ReviewLog()
	if rl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "review backend is not active"})
		return
	}
	var body struct {
		MySlug         string `json:"my_slug"`
		SourcePath     string `json:"source_path"`
		TargetWikiPath string `json:"target_wiki_path"`
		Rationale      string `json:"rationale"`
		ReviewerSlug   string `json:"reviewer_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	// Pre-flight: target must not already exist (state machine re-checks
	// atomically during approve, but rejecting early keeps the common
	// case from creating a dangling pending review).
	worker := b.WikiWorker()
	if worker != nil {
		if _, err := readArticle(worker.Repo(), body.TargetWikiPath); err == nil {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": fmt.Sprintf("target wiki path %q already exists", body.TargetWikiPath),
			})
			return
		}
	}
	promotion, err := rl.SubmitPromotion(SubmitPromotionRequest{
		SourceSlug:       body.MySlug,
		SourcePath:       body.SourcePath,
		TargetPath:       body.TargetWikiPath,
		Rationale:        body.Rationale,
		ReviewerOverride: body.ReviewerSlug,
	})
	if err != nil {
		writeJSON(w, reviewStatusForError(err), map[string]string{"error": err.Error()})
		return
	}
	b.publishReviewEvent(ReviewStateChangeEvent{
		ID:        promotion.ID,
		OldState:  "",
		NewState:  promotion.State,
		ActorSlug: promotion.SourceSlug,
		Timestamp: promotion.CreatedAt.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"promotion_id":  promotion.ID,
		"reviewer_slug": promotion.ReviewerSlug,
		"state":         promotion.State,
		"human_only":    promotion.HumanOnly,
	})
}

// handleReviewList returns all active reviews or a scoped slice.
//
//	GET /review/list?scope=all         — every non-archived review
//	GET /review/list?scope=<slug>      — reviews assigned to slug
//	GET /review/list?scope=mine        — alias for the caller's slug
//	                                     (requires X-WUPHF-Agent header)
//
// Response envelope matches what the frontend expects: { reviews: [...] }.
func (b *Broker) handleReviewList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rl := b.ReviewLog()
	if rl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "review backend is not active"})
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "mine" {
		// "mine" maps to the caller's agent slug via the standard
		// X-WUPHF-Agent header the MCP server sets on every broker call.
		scope = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
		if scope == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope=mine requires X-WUPHF-Agent"})
			return
		}
	}
	reviews := rl.List(scope)
	out := make([]reviewItemResponse, 0, len(reviews))
	for _, p := range reviews {
		out = append(out, b.reviewItemForPromotion(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": out})
}

func (b *Broker) reviewItemForPromotion(p *Promotion) reviewItemResponse {
	if p == nil {
		return reviewItemResponse{}
	}
	sourceBody := ""
	if worker := b.WikiWorker(); worker != nil {
		if bytes, err := worker.NotebookRead(p.SourcePath); err == nil {
			sourceBody = string(bytes)
		}
	}
	entrySlug := notebookEntrySlug(p.SourcePath)
	comments := make([]reviewCommentResponse, 0, len(p.Comments))
	for _, c := range p.Comments {
		comments = append(comments, reviewCommentResponse{
			ID:         c.ID,
			AuthorSlug: c.AuthorSlug,
			BodyMD:     c.Body,
			TS:         c.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return reviewItemResponse{
		ID:               p.ID,
		AgentSlug:        p.SourceSlug,
		EntrySlug:        entrySlug,
		EntryTitle:       markdownTitle(sourceBody, entrySlug),
		ProposedWikiPath: p.TargetPath,
		Excerpt:          markdownExcerpt(sourceBody, p.Rationale),
		ReviewerSlug:     p.ReviewerSlug,
		State:            p.State,
		SubmittedTS:      p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedTS:        p.UpdatedAt.UTC().Format(time.RFC3339),
		Comments:         comments,
	}
}

func notebookEntrySlug(sourcePath string) string {
	base := filepath.Base(filepath.ToSlash(sourcePath))
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func markdownTitle(body, fallbackSlug string) string {
	for _, line := range strings.Split(stripFrontmatter(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	if fallbackSlug == "" {
		return "Review"
	}
	return titleFromSlug(fallbackSlug)
}

func titleFromSlug(slug string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(slug, "-", " "), "_", " "))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func markdownExcerpt(body, fallback string) string {
	var parts []string
	for _, line := range strings.Split(stripFrontmatter(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}
		parts = append(parts, line)
		if len(strings.Join(parts, " ")) >= 220 {
			break
		}
	}
	text := strings.Join(parts, " ")
	if strings.TrimSpace(text) == "" {
		text = firstLine(fallback)
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 220 {
		text = text[:217] + "..."
	}
	return text
}

// handleReviewSubpath dispatches /review/{id}[/{verb}] to the right action.
// Keeping one handler avoids adding six mux entries when the verbs are
// fully enumerable and share a lot of boilerplate.
func (b *Broker) handleReviewSubpath(w http.ResponseWriter, r *http.Request) {
	rl := b.ReviewLog()
	if rl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "review backend is not active"})
		return
	}
	// Trim the leading /review/ and split the remainder.
	rest := strings.TrimPrefix(r.URL.Path, "/review/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "review id is required"})
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id := strings.TrimSpace(parts[0])
	verb := ""
	if len(parts) == 2 {
		verb = strings.TrimSpace(parts[1])
	}

	// Verify the promotion exists before routing — saves repeat lookups
	// inside each handler.
	if _, err := rl.Get(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	switch verb {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p, err := rl.Get(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, b.reviewItemForPromotion(p))
		return
	case "approve":
		b.reviewApprove(w, r, id)
		return
	case "request-changes":
		b.reviewRequestChanges(w, r, id)
		return
	case "reject":
		b.reviewReject(w, r, id)
		return
	case "resubmit":
		b.reviewResubmit(w, r, id)
		return
	case "comment":
		b.reviewComment(w, r, id)
		return
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown verb " + verb})
	}
}

func (b *Broker) reviewApprove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ActorSlug string `json:"actor_slug"`
		Rationale string `json:"rationale"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	rl := b.ReviewLog()
	p, err := rl.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	// Authorize the actor BEFORE doing the atomic wiki commit. Without this
	// pre-check any authenticated agent could force a wiki write by POSTing
	// /review/{id}/approve with their own slug — ApplyPromotion would
	// succeed then Approve would reject with ErrWrongReviewer, leaving
	// the wiki dirty and the state machine inconsistent.
	if authErr := rl.CanApprove(id, body.ActorSlug); authErr != nil {
		writeJSON(w, reviewStatusForError(authErr), map[string]string{"error": authErr.Error()})
		return
	}
	// Executing the atomic promote commit happens BEFORE recording the
	// state transition so the state machine's approved state always
	// coincides with a real commit on disk. If the commit fails, the
	// state stays in-review and the reviewer sees a 500/409.
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	sha, commitErr := worker.Repo().ApplyPromotion(r.Context(), p, body.ActorSlug)
	if commitErr != nil {
		if errors.Is(commitErr, ErrPromotionTargetExists) {
			// Bounce the reviewer into `changes-requested` so the author
			// can pick a different target path.
			updated, _, tErr := rl.RequestChanges(id, body.ActorSlug, "target path already exists: "+p.TargetPath)
			if tErr == nil {
				b.publishReviewEvent(ReviewStateChangeEvent{
					ID:        updated.ID,
					OldState:  PromotionInReview,
					NewState:  PromotionChangesRequested,
					ActorSlug: body.ActorSlug,
					Timestamp: updated.UpdatedAt.Format(time.RFC3339),
				})
			}
			writeJSON(w, http.StatusConflict, map[string]string{"error": commitErr.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": commitErr.Error()})
		return
	}

	updated, t, err := rl.Approve(id, body.ActorSlug, body.Rationale, sha)
	if err != nil {
		writeJSON(w, reviewStatusForError(err), map[string]string{"error": err.Error()})
		return
	}
	b.publishReviewEvent(ReviewStateChangeEvent{
		ID:        updated.ID,
		OldState:  t.OldState,
		NewState:  t.NewState,
		ActorSlug: body.ActorSlug,
		Timestamp: t.Timestamp.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, updated)
}

func (b *Broker) reviewRequestChanges(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ActorSlug string `json:"actor_slug"`
		Rationale string `json:"rationale"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if strings.TrimSpace(body.Rationale) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rationale is required"})
		return
	}
	updated, t, err := b.ReviewLog().RequestChanges(id, body.ActorSlug, body.Rationale)
	if err != nil {
		writeJSON(w, reviewStatusForError(err), map[string]string{"error": err.Error()})
		return
	}
	b.publishReviewEvent(ReviewStateChangeEvent{
		ID:        updated.ID,
		OldState:  t.OldState,
		NewState:  t.NewState,
		ActorSlug: body.ActorSlug,
		Timestamp: t.Timestamp.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, updated)
}

func (b *Broker) reviewReject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ActorSlug string `json:"actor_slug"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	updated, t, err := b.ReviewLog().Reject(id, body.ActorSlug)
	if err != nil {
		writeJSON(w, reviewStatusForError(err), map[string]string{"error": err.Error()})
		return
	}
	b.publishReviewEvent(ReviewStateChangeEvent{
		ID:        updated.ID,
		OldState:  t.OldState,
		NewState:  t.NewState,
		ActorSlug: body.ActorSlug,
		Timestamp: t.Timestamp.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, updated)
}

func (b *Broker) reviewResubmit(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ActorSlug string `json:"actor_slug"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	updated, t, err := b.ReviewLog().Resubmit(id, body.ActorSlug)
	if err != nil {
		writeJSON(w, reviewStatusForError(err), map[string]string{"error": err.Error()})
		return
	}
	b.publishReviewEvent(ReviewStateChangeEvent{
		ID:        updated.ID,
		OldState:  t.OldState,
		NewState:  t.NewState,
		ActorSlug: body.ActorSlug,
		Timestamp: t.Timestamp.Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, updated)
}

func (b *Broker) reviewComment(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ActorSlug string `json:"actor_slug"`
		Body      string `json:"body"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	updated, _, err := b.ReviewLog().AddComment(id, body.ActorSlug, body.Body)
	if err != nil {
		writeJSON(w, reviewStatusForError(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// reviewStatusForError maps state-machine errors to the right HTTP code.
// Unknown errors fall through to 500.
func reviewStatusForError(err error) int {
	switch {
	case errors.Is(err, ErrPromotionNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrIllegalTransition),
		errors.Is(err, ErrPromotionAlreadyApproved):
		return http.StatusConflict
	case errors.Is(err, ErrHumanOnlyReviewRequired),
		errors.Is(err, ErrWrongReviewer),
		errors.Is(err, ErrWrongAuthor):
		return http.StatusForbidden
	case strings.Contains(err.Error(), "is required"),
		strings.Contains(err.Error(), "must be"),
		strings.Contains(err.Error(), "invalid"):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
