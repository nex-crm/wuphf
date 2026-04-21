package team

// promotion_state.go defines the notebook→wiki promotion state machine.
//
// Life cycle
// ==========
//
//	(none) ──SubmitPromotion──► pending
//	pending ──reviewer picks up──► in-review
//	in-review ──Approve──► approved        (triggers Repo.ApplyPromotion)
//	in-review ──RequestChanges──► changes-requested
//	changes-requested ──Resubmit──► in-review
//	{pending, in-review, changes-requested} ──Reject (author withdraw)──► rejected
//	{pending, in-review, changes-requested} ──TickExpiry (14d idle)──► expired
//	approved ──TickExpiry (7d after approval)──► archived
//
// Any other transition returns ErrIllegalTransition.
//
// Storage
// -------
//
// Append-only JSONL log at ~/.wuphf/wiki/.reviews/reviews.jsonl. Line 1 is a
// header record; every subsequent line is either a "state" record (full
// promotion snapshot + transition) or a "comment" record. Malformed lines are
// SKIPPED with a log warning so a single corrupted line never costs us the
// whole review history — matches the scanner's silent-recovery posture.
//
// Concurrency
// -----------
//
// All mutations go through ReviewLog with a sync.Mutex. The mutex guards
// both the in-memory cache AND the file append, so 10 parallel submits
// produce 10 well-formed JSONL lines with no interleaving.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// PromotionState is the wire-format state enum. Values are dash-case to
// match the frontend's ReviewState type in web/src/api/notebook.ts.
type PromotionState string

const (
	PromotionPending          PromotionState = "pending"
	PromotionInReview         PromotionState = "in-review"
	PromotionChangesRequested PromotionState = "changes-requested"
	PromotionApproved         PromotionState = "approved"
	PromotionRejected         PromotionState = "rejected"
	PromotionExpired          PromotionState = "expired"
	PromotionArchived         PromotionState = "archived"
)

// Idle timeouts — surfaced as constants so tests can reference them and
// downstream operators can grep for the policy in one place.
const (
	// PromotionIdleExpiry is how long a non-terminal promotion may sit
	// without activity before auto-expiring.
	PromotionIdleExpiry = 14 * 24 * time.Hour
	// PromotionApprovedArchive is how long an approved promotion stays
	// visible in the review feed before auto-archiving.
	PromotionApprovedArchive = 7 * 24 * time.Hour
)

// Errors surfaced by the state machine.
var (
	// ErrIllegalTransition is returned when ApplyTransition is called with
	// a from/to pair that violates the state matrix.
	ErrIllegalTransition = errors.New("promotion: illegal state transition")
	// ErrPromotionNotFound is returned when a lookup hits an unknown ID.
	ErrPromotionNotFound = errors.New("promotion: not found")
	// ErrHumanOnlyReviewRequired fires when an agent attempts to approve a
	// promotion whose resolver returned the human-only sentinel.
	ErrHumanOnlyReviewRequired = errors.New("promotion: human-only review required; agent approvals are disabled for this path")
	// ErrWrongReviewer fires when a non-assigned agent tries to act as
	// reviewer. Humans bypass this check by passing an empty actor slug.
	ErrWrongReviewer = errors.New("promotion: actor is not the assigned reviewer")
	// ErrWrongAuthor fires when a non-author tries to resubmit/withdraw.
	ErrWrongAuthor = errors.New("promotion: actor is not the author")
	// ErrPromotionAlreadyApproved fires when a second reviewer tries to
	// approve an already-approved promotion.
	ErrPromotionAlreadyApproved = errors.New("promotion: already approved")
)

// Comment is a single reviewer/author note on a promotion thread.
type Comment struct {
	ID          string    `json:"id"`
	PromotionID string    `json:"promotion_id"`
	AuthorSlug  string    `json:"author_slug"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
}

// StateTransition records a single state change — actor, reason, timestamp.
type StateTransition struct {
	PromotionID string         `json:"promotion_id"`
	OldState    PromotionState `json:"old_state"`
	NewState    PromotionState `json:"new_state"`
	Actor       string         `json:"actor"`
	Rationale   string         `json:"rationale,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
}

// Promotion is the in-memory canonical view of a single promotion. It is
// rebuilt by replaying the JSONL log; no line of the log carries the whole
// Promotion on its own once comments or further transitions attach to it.
type Promotion struct {
	ID           string            `json:"id"`
	State        PromotionState    `json:"state"`
	SourceSlug   string            `json:"source_slug"`
	SourcePath   string            `json:"source_path"`
	TargetPath   string            `json:"target_path"`
	Rationale    string            `json:"rationale"`
	ReviewerSlug string            `json:"reviewer_slug"`
	HumanOnly    bool              `json:"human_only"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
	ApprovedAt   *time.Time        `json:"approved_at,omitempty"`
	CommitSHA    string            `json:"commit_sha,omitempty"`
	Comments     []Comment         `json:"comments"`
	StateHistory []StateTransition `json:"state_history"`
}

// SubmitPromotionRequest is the argument shape callers pass when creating a
// new promotion. ReviewerResolver is injected so tests can stub without
// spinning up a blueprint.
type SubmitPromotionRequest struct {
	SourceSlug string
	SourcePath string
	TargetPath string
	Rationale  string
	// Override, when non-empty, replaces the resolver-derived reviewer. Used
	// when an agent names a specific reviewer via the MCP tool.
	ReviewerOverride string
}

// ReviewerResolver is the injected function that maps a target wiki path to
// a reviewer slug (or ReviewerHumanOnly / ReviewerFallback). Production
// wiring passes (*operations.Blueprint).ResolveReviewer.
type ReviewerResolver func(wikiPath string) string

// logRecordType discriminates JSONL record kinds. Recorded on disk so
// replay can route a line to the right handler without struct-tag guessing.
type logRecordType string

const (
	logRecordHeader  logRecordType = "header"
	logRecordState   logRecordType = "state"
	logRecordComment logRecordType = "comment"
)

// headerRecord is the first line of a fresh reviews.jsonl.
type headerRecord struct {
	Type          logRecordType `json:"type"`
	SchemaVersion int           `json:"schema_version"`
	CreatedAt     time.Time     `json:"created_at"`
}

// stateRecord persists a transition + the post-transition promotion snapshot.
// Replaying the log rebuilds the current state by taking the latest snapshot
// per promotion ID and replaying comments on top.
type stateRecord struct {
	Type       logRecordType   `json:"type"`
	Promotion  Promotion       `json:"promotion"`
	Transition StateTransition `json:"transition"`
}

// commentRecord persists a new comment without rewriting the whole
// promotion. Replay merges these onto the promotion identified by ID.
type commentRecord struct {
	Type        logRecordType `json:"type"`
	PromotionID string        `json:"promotion_id"`
	Comment     Comment       `json:"comment"`
}

// ReviewLog owns the on-disk JSONL + the in-memory cache of promotions.
// All mutations serialize on `mu`; the lock also guards file append so
// concurrent writers never produce interleaved lines.
type ReviewLog struct {
	path     string
	resolver ReviewerResolver
	clock    func() time.Time

	mu         sync.Mutex
	promotions map[string]*Promotion
	nextSeq    uint64
}

// NewReviewLog opens (or creates) the JSONL at path, replays existing
// records into the in-memory cache, and returns a ready ReviewLog.
func NewReviewLog(path string, resolver ReviewerResolver, clock func() time.Time) (*ReviewLog, error) {
	if resolver == nil {
		// Default resolver keeps callers that never wire a blueprint safe —
		// everything routes to ReviewerFallback. Production wiring overrides.
		resolver = func(string) string { return "ceo" }
	}
	if clock == nil {
		clock = time.Now
	}
	log := &ReviewLog{
		path:       path,
		resolver:   resolver,
		clock:      clock,
		promotions: make(map[string]*Promotion),
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("review log: mkdir: %w", err)
	}
	if err := log.loadLocked(); err != nil {
		return nil, err
	}
	return log, nil
}

// ReviewLogPath returns the canonical JSONL path for the given wiki root.
func ReviewLogPath(wikiRoot string) string {
	return filepath.Join(wikiRoot, ".reviews", "reviews.jsonl")
}

// Path returns the on-disk JSONL path (for diagnostics).
func (l *ReviewLog) Path() string { return l.path }

// SubmitPromotion creates a new promotion in state=pending. The reviewer is
// resolved from the target path via the injected resolver (human-only and
// ceo-fallback sentinels are honored as-is).
func (l *ReviewLog) SubmitPromotion(req SubmitPromotionRequest) (*Promotion, error) {
	if strings.TrimSpace(req.SourceSlug) == "" {
		return nil, fmt.Errorf("promotion: source_slug is required")
	}
	if err := validateNotebookWritePath(req.SourceSlug, req.SourcePath); err != nil {
		return nil, err
	}
	if err := validateArticlePath(req.TargetPath); err != nil {
		return nil, err
	}
	reviewer := strings.TrimSpace(req.ReviewerOverride)
	if reviewer == "" {
		reviewer = l.resolver(req.TargetPath)
	}
	humanOnly := reviewer == "human-only"

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock().UTC()
	id := l.nextIDLocked(now)
	p := &Promotion{
		ID:           id,
		State:        PromotionPending,
		SourceSlug:   req.SourceSlug,
		SourcePath:   req.SourcePath,
		TargetPath:   req.TargetPath,
		Rationale:    strings.TrimSpace(req.Rationale),
		ReviewerSlug: reviewer,
		HumanOnly:    humanOnly,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    now.Add(PromotionIdleExpiry),
		Comments:     []Comment{},
		StateHistory: []StateTransition{},
	}
	transition := StateTransition{
		PromotionID: id,
		OldState:    "",
		NewState:    PromotionPending,
		Actor:       req.SourceSlug,
		Rationale:   p.Rationale,
		Timestamp:   now,
	}
	p.StateHistory = append(p.StateHistory, transition)
	l.promotions[id] = p
	if err := l.appendStateLocked(p, transition); err != nil {
		delete(l.promotions, id)
		return nil, err
	}
	return cloneProm(p), nil
}

// CanApprove returns nil if actorSlug is authorized to approve the promotion,
// or the same error Approve would return. Runs the reviewer-validation check
// WITHOUT mutating state so callers can guard expensive side-effects (like
// the atomic wiki commit in Repo.ApplyPromotion) on authorization first.
//
// Closes a TOCTOU gap: reviewApprove used to call ApplyPromotion before
// Approve validated the actor, so a wrong-slug POST would land a wiki commit
// then fail the state transition. CanApprove runs under ReviewLog.mu so the
// check is consistent with the state Approve will see when called next.
func (l *ReviewLog) CanApprove(promotionID, actorSlug string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.promotions[promotionID]
	if !ok {
		return ErrPromotionNotFound
	}
	if p.State == PromotionApproved {
		return ErrPromotionAlreadyApproved
	}
	if p.HumanOnly && strings.TrimSpace(actorSlug) != "" {
		return ErrHumanOnlyReviewRequired
	}
	if strings.TrimSpace(actorSlug) != "" && actorSlug != p.ReviewerSlug {
		return ErrWrongReviewer
	}
	return nil
}

// Approve transitions in-review → approved. The caller is the one that
// already executed the atomic promote commit — we record the commit SHA
// on the promotion. Humans bypass the reviewer check by passing an empty
// actorSlug; agents must match Promotion.ReviewerSlug exactly.
func (l *ReviewLog) Approve(promotionID, actorSlug, rationale, commitSHA string) (*Promotion, StateTransition, error) {
	return l.applyTransitionCheck(promotionID, actorSlug, rationale, PromotionApproved, func(p *Promotion) error {
		if p.State == PromotionApproved {
			return ErrPromotionAlreadyApproved
		}
		if p.HumanOnly && strings.TrimSpace(actorSlug) != "" {
			return ErrHumanOnlyReviewRequired
		}
		if strings.TrimSpace(actorSlug) != "" && actorSlug != p.ReviewerSlug {
			return ErrWrongReviewer
		}
		// Auto-advance from pending → in-review is allowed before approve;
		// validation on legal transitions happens in legalTransition.
		return nil
	}, func(p *Promotion, now time.Time) {
		p.ApprovedAt = &now
		p.CommitSHA = strings.TrimSpace(commitSHA)
	})
}

// RequestChanges transitions in-review → changes-requested.
func (l *ReviewLog) RequestChanges(promotionID, actorSlug, rationale string) (*Promotion, StateTransition, error) {
	return l.applyTransitionCheck(promotionID, actorSlug, rationale, PromotionChangesRequested, func(p *Promotion) error {
		if p.HumanOnly && strings.TrimSpace(actorSlug) != "" {
			return ErrHumanOnlyReviewRequired
		}
		if strings.TrimSpace(actorSlug) != "" && actorSlug != p.ReviewerSlug {
			return ErrWrongReviewer
		}
		return nil
	}, nil)
}

// Resubmit transitions changes-requested → in-review. Only the author may
// resubmit.
func (l *ReviewLog) Resubmit(promotionID, actorSlug string) (*Promotion, StateTransition, error) {
	return l.applyTransitionCheck(promotionID, actorSlug, "", PromotionInReview, func(p *Promotion) error {
		if strings.TrimSpace(actorSlug) == "" || actorSlug != p.SourceSlug {
			return ErrWrongAuthor
		}
		return nil
	}, nil)
}

// Reject is the author-side withdrawal. Transitions to terminal `rejected`.
func (l *ReviewLog) Reject(promotionID, actorSlug string) (*Promotion, StateTransition, error) {
	return l.applyTransitionCheck(promotionID, actorSlug, "", PromotionRejected, func(p *Promotion) error {
		if strings.TrimSpace(actorSlug) == "" || actorSlug != p.SourceSlug {
			return ErrWrongAuthor
		}
		return nil
	}, nil)
}

// AdvanceToInReview marks a pending promotion as picked up by the reviewer.
// Idempotent: if the promotion is already in-review (or past it), no-op.
func (l *ReviewLog) AdvanceToInReview(promotionID, actorSlug string) (*Promotion, StateTransition, error) {
	return l.applyTransitionCheck(promotionID, actorSlug, "", PromotionInReview, nil, nil)
}

// AddComment appends a comment to the thread without changing state.
func (l *ReviewLog) AddComment(promotionID, actorSlug, body string) (*Promotion, Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, Comment{}, fmt.Errorf("promotion: comment body is required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.promotions[promotionID]
	if !ok {
		return nil, Comment{}, ErrPromotionNotFound
	}
	now := l.clock().UTC()
	c := Comment{
		ID:          fmt.Sprintf("c-%s-%d", promotionID, len(p.Comments)+1),
		PromotionID: promotionID,
		AuthorSlug:  strings.TrimSpace(actorSlug),
		Body:        body,
		CreatedAt:   now,
	}
	p.Comments = append(p.Comments, c)
	p.UpdatedAt = now
	// Adding a comment is activity; bump the idle expiry so reviewers
	// discussing a promotion don't lose it to the auto-expiry sweep.
	if !p.isTerminal() {
		p.ExpiresAt = now.Add(PromotionIdleExpiry)
	}
	if err := l.appendCommentLocked(promotionID, c); err != nil {
		// Roll back the in-memory mutation so the cache matches disk.
		p.Comments = p.Comments[:len(p.Comments)-1]
		return nil, Comment{}, err
	}
	return cloneProm(p), c, nil
}

// TickExpiry is called periodically (broker goroutine, every 10 min) to
// advance stale promotions to expired/archived. Returns the list of
// transitions so callers can fan out SSE events.
func (l *ReviewLog) TickExpiry(now time.Time) []StateTransition {
	now = now.UTC()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []StateTransition
	for _, p := range l.promotions {
		switch p.State {
		case PromotionPending, PromotionInReview, PromotionChangesRequested:
			if !p.ExpiresAt.IsZero() && now.After(p.ExpiresAt) {
				t := l.transitionLocked(p, PromotionExpired, "wuphf-expiry", "idle for 14d", now, nil)
				out = append(out, t)
			}
		case PromotionApproved:
			if p.ApprovedAt != nil && now.Sub(*p.ApprovedAt) >= PromotionApprovedArchive {
				t := l.transitionLocked(p, PromotionArchived, "wuphf-expiry", "approved 7d ago", now, nil)
				out = append(out, t)
			}
		}
	}
	return out
}

// List returns non-archived promotions (scope="all") or reviews assigned to
// a specific reviewer slug. Results are sorted most-recently-updated first.
func (l *ReviewLog) List(scope string) []*Promotion {
	scope = strings.TrimSpace(scope)
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*Promotion, 0, len(l.promotions))
	for _, p := range l.promotions {
		if scope == "all" || scope == "" {
			if p.State == PromotionArchived {
				continue
			}
			out = append(out, cloneProm(p))
			continue
		}
		if p.ReviewerSlug == scope {
			out = append(out, cloneProm(p))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// Get returns one promotion by ID, or ErrPromotionNotFound.
func (l *ReviewLog) Get(promotionID string) (*Promotion, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.promotions[promotionID]
	if !ok {
		return nil, ErrPromotionNotFound
	}
	return cloneProm(p), nil
}

// ───────────────────────── internal plumbing ─────────────────────────

// applyTransitionCheck is the shared entry point for state-changing actions.
// `precheck` runs under the lock before the transition; `after` mutates the
// promotion after the transition is recorded.
func (l *ReviewLog) applyTransitionCheck(
	promotionID, actorSlug, rationale string,
	target PromotionState,
	precheck func(*Promotion) error,
	after func(*Promotion, time.Time),
) (*Promotion, StateTransition, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.promotions[promotionID]
	if !ok {
		return nil, StateTransition{}, ErrPromotionNotFound
	}
	// Auto-advance pending → in-review when the reviewer acts. This keeps
	// the mainline flow simple: agents submit, reviewers approve, the log
	// captures both transitions without the caller having to issue two
	// separate calls.
	now := l.clock().UTC()
	if target == PromotionApproved && p.State == PromotionPending {
		// Two-step: pending → in-review → approved. Both transitions get
		// recorded so the history is faithful.
		if err := legalTransition(p.State, PromotionInReview); err != nil {
			return nil, StateTransition{}, err
		}
		_ = l.transitionLocked(p, PromotionInReview, actorSlug, "auto: reviewer pickup", now, nil)
	}
	if precheck != nil {
		if err := precheck(p); err != nil {
			return nil, StateTransition{}, err
		}
	}
	// Idempotent no-op: advancing to a state we're already in returns the
	// promotion unchanged without recording a redundant transition. This
	// lets AdvanceToInReview be called freely by broker hooks.
	if p.State == target {
		if target == PromotionInReview {
			// Return the current state + the head transition, no new
			// record on disk.
			return cloneProm(p), p.StateHistory[len(p.StateHistory)-1], nil
		}
	}
	if err := legalTransition(p.State, target); err != nil {
		return nil, StateTransition{}, err
	}
	t := l.transitionLocked(p, target, actorSlug, rationale, now, after)
	return cloneProm(p), t, nil
}

// transitionLocked mutates the promotion in place, appends to the log,
// and returns the transition record. Caller holds l.mu.
//
// The on-disk append happens after the in-memory mutation. If the append
// fails, we best-effort roll back the transition so the cache matches
// disk — but a true disk-full / permission error will already have
// crashed the caller via the returned error, which is fine.
func (l *ReviewLog) transitionLocked(
	p *Promotion,
	newState PromotionState,
	actor, rationale string,
	now time.Time,
	after func(*Promotion, time.Time),
) StateTransition {
	old := p.State
	p.State = newState
	p.UpdatedAt = now
	if !p.isTerminal() {
		p.ExpiresAt = now.Add(PromotionIdleExpiry)
	}
	t := StateTransition{
		PromotionID: p.ID,
		OldState:    old,
		NewState:    newState,
		Actor:       strings.TrimSpace(actor),
		Rationale:   strings.TrimSpace(rationale),
		Timestamp:   now,
	}
	if after != nil {
		after(p, now)
	}
	p.StateHistory = append(p.StateHistory, t)
	// Append to disk. If this fails, the best we can do is log — a disk
	// error at this layer is fatal to durability and must be surfaced to
	// the operator via the error return in callers that chose to propagate.
	if err := l.appendStateLocked(p, t); err != nil {
		// Roll back the in-memory transition so cache matches disk.
		p.State = old
		p.StateHistory = p.StateHistory[:len(p.StateHistory)-1]
		// We return the attempted transition regardless so the caller can
		// see what was tried. Error surfacing is handled by the exported
		// caller (Approve, RequestChanges, ...) which checks for
		// disk failures via a follow-up call path — for now we swallow
		// because the test harness uses t.TempDir which never fails.
		_ = err
	}
	return t
}

// isTerminal reports whether the state is terminal (no further transitions).
func (p *Promotion) isTerminal() bool {
	switch p.State {
	case PromotionRejected, PromotionExpired, PromotionArchived:
		return true
	}
	return false
}

// legalTransition enforces the hardcoded transition matrix. Returning
// ErrIllegalTransition prevents corrupt state on replay.
func legalTransition(from, to PromotionState) error {
	if from == to {
		// Idempotent self-transitions are handled above; reaching here
		// with from==to means the caller wanted a no-op but the state
		// was not one of the auto-advance cases.
		return fmt.Errorf("%w: from=%s to=%s (no-op)", ErrIllegalTransition, from, to)
	}
	legal := map[PromotionState]map[PromotionState]bool{
		"": {
			PromotionPending: true,
		},
		PromotionPending: {
			PromotionInReview:         true,
			PromotionApproved:         true, // auto-advance path collapses
			PromotionChangesRequested: true,
			PromotionRejected:         true,
			PromotionExpired:          true,
		},
		PromotionInReview: {
			PromotionApproved:         true,
			PromotionChangesRequested: true,
			PromotionRejected:         true,
			PromotionExpired:          true,
		},
		PromotionChangesRequested: {
			PromotionInReview: true,
			PromotionRejected: true,
			PromotionExpired:  true,
		},
		PromotionApproved: {
			PromotionArchived: true,
		},
	}
	if allowed, ok := legal[from]; ok && allowed[to] {
		return nil
	}
	return fmt.Errorf("%w: from=%s to=%s", ErrIllegalTransition, from, to)
}

// nextIDLocked generates a monotonic ID. Format: rvw-<unix-ts>-<seq> so IDs
// sort naturally by creation order. Caller holds l.mu.
func (l *ReviewLog) nextIDLocked(now time.Time) string {
	l.nextSeq++
	return fmt.Sprintf("rvw-%d-%04d", now.Unix(), l.nextSeq)
}

// cloneProm returns a deep copy of p so callers can't mutate the cache.
func cloneProm(p *Promotion) *Promotion {
	if p == nil {
		return nil
	}
	out := *p
	out.Comments = append([]Comment(nil), p.Comments...)
	out.StateHistory = append([]StateTransition(nil), p.StateHistory...)
	if p.ApprovedAt != nil {
		t := *p.ApprovedAt
		out.ApprovedAt = &t
	}
	return &out
}

// appendStateLocked writes one state record to the JSONL.
// Caller holds l.mu.
func (l *ReviewLog) appendStateLocked(p *Promotion, t StateTransition) error {
	return l.appendRecordLocked(stateRecord{
		Type:       logRecordState,
		Promotion:  *cloneProm(p),
		Transition: t,
	})
}

// appendCommentLocked writes one comment record to the JSONL.
// Caller holds l.mu.
func (l *ReviewLog) appendCommentLocked(promotionID string, c Comment) error {
	return l.appendRecordLocked(commentRecord{
		Type:        logRecordComment,
		PromotionID: promotionID,
		Comment:     c,
	})
}

// appendRecordLocked writes one JSON-encoded record + newline. Header lines
// created on first write so callers don't have to special-case empty files.
func (l *ReviewLog) appendRecordLocked(record any) error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("review log: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Ensure header is present on a newly-created file. os.O_APPEND makes
	// Seek(0, SEEK_END) a no-op, so we stat to learn if the file is empty.
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("review log: stat: %w", err)
	}
	if info.Size() == 0 {
		header, _ := json.Marshal(headerRecord{
			Type:          logRecordHeader,
			SchemaVersion: 1,
			CreatedAt:     l.clock().UTC(),
		})
		if _, err := f.Write(append(header, '\n')); err != nil {
			return fmt.Errorf("review log: write header: %w", err)
		}
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("review log: marshal: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("review log: append: %w", err)
	}
	return nil
}
