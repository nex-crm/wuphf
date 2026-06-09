package packer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrFinalScanFailed is returned when the final redaction scan over the exact
// rendered bytes refuses the content. Render can reintroduce raw refs/titles a
// pre-render item scan never saw, so the last scan is over what actually leaves.
var ErrFinalScanFailed = errors.New("packer: delivery aborted — final egress scan failed")

// SlackBridge posts a packed delegation and returns the Slack message ts. The
// real implementation lives in the Slack bridge adapter (a separate spec); tests
// use a fake.
type SlackBridge interface {
	Post(ctx context.Context, d PackedDelegation, idempotencyKey string) (messageTS string, err error)
}

// SnapshotValidator re-checks, at delivery time, that the task/profile/policy
// snapshot the delegation was built against is still current. A trust downgrade
// or a version bump between Classify and Deliver must abort the send.
type SnapshotValidator interface {
	Validate(req ContextRequest) error
}

// InjectionSink is the append-only audit store. Write is called on every state
// transition (pending -> sent/failed). Lookup powers idempotency: a delegation
// already delivered for an idempotency key is never sent twice.
type InjectionSink interface {
	Lookup(idempotencyKey string) (InjectionRecord, bool)
	Write(rec InjectionRecord) error
}

// Clock yields the current time. Injectable so tests are deterministic.
type Clock func() time.Time

// SystemClock is the default wall clock.
func SystemClock() time.Time { return time.Now().UTC() }

// Deliver is idempotent on req.IdempotencyKey. It (1) returns the prior record if
// this key already delivered; (2) re-validates the snapshot and aborts on a
// stale task / trust downgrade / version bump; (3) runs a FINAL redaction scan
// over the exact rendered bytes; (4) writes the InjectionRecord pending first,
// posts via the bridge, then updates to sent (with the Slack ts) or failed (with
// a reason). It deliberately does NOT use the generic outbound dispatcher, which
// marks delivered on dequeue and drops failures — for an egress boundary, dropped
// or duplicated context is security-relevant.
func Deliver(
	ctx context.Context,
	br SlackBridge,
	val SnapshotValidator,
	sc SecretScanner,
	sink InjectionSink,
	clock Clock,
	d PackedDelegation,
	req ContextRequest,
) (InjectionRecord, error) {
	if clock == nil {
		clock = SystemClock
	}
	now := clock().Format(time.RFC3339Nano)

	// 1. Idempotency: never ship the same delegation twice. Only a SENT record
	//    short-circuits — a prior pending/failed attempt may be retried. (The
	//    real broker-backed sink uses the pending marker as a lock to also close
	//    the concurrent-send window; the in-process broker mutex covers it here.)
	if prior, ok := sink.Lookup(req.IdempotencyKey); ok && prior.Status == DeliverySent {
		return prior, nil
	}

	rec := d.Injection
	rec.IdempotencyKey = req.IdempotencyKey
	rec.Timestamp = now

	// 2. Re-validate the snapshot. A downgrade/edit between Classify and Deliver
	//    must not ship stale-authorized context.
	if val != nil {
		if err := val.Validate(req); err != nil {
			rec.Status = DeliveryFailed
			rec.FailureReason = "snapshot stale: " + err.Error()
			_ = sink.Write(rec)
			return rec, fmt.Errorf("packer: delivery aborted: %w", err)
		}
	}

	// 3. Final redaction scan over the EXACT bytes that will leave.
	finalMention := sc.Scan(d.MentionText)
	finalThread := sc.Scan(d.ThreadContext)
	if !finalMention.OK || !finalThread.OK {
		rec.Status = DeliveryFailed
		rec.FailureReason = "final egress scan: " + firstNonEmpty(finalMention.Reason, finalThread.Reason)
		_ = sink.Write(rec)
		return rec, ErrFinalScanFailed
	}

	sealed := PackedDelegation{
		MentionText:   finalMention.Content,
		ThreadContext: finalThread.Content,
	}
	rec.RenderedHash = hashBytes(sealed.MentionText, sealed.ThreadContext)
	rec.TokenCount = estimateTokens(sealed.MentionText) + estimateTokens(sealed.ThreadContext)

	// 4. Pending -> post -> sent/failed.
	rec.Status = DeliveryPending
	if err := sink.Write(rec); err != nil {
		return rec, fmt.Errorf("packer: write pending audit: %w", err)
	}

	ts, err := br.Post(ctx, sealed, req.IdempotencyKey)
	if err != nil {
		rec.Status = DeliveryFailed
		rec.FailureReason = err.Error()
		_ = sink.Write(rec)
		return rec, fmt.Errorf("packer: bridge post: %w", err)
	}

	rec.MessageTS = ts
	rec.Status = DeliverySent
	if err := sink.Write(rec); err != nil {
		return rec, fmt.Errorf("packer: write sent audit: %w", err)
	}
	return rec, nil
}

// hashBytes hashes exactly what was sent (mention + thread) with a NUL separator
// so two fields cannot collide by concatenation.
func hashBytes(mention, thread string) string {
	h := sha256.New()
	h.Write([]byte(mention))
	h.Write([]byte{0})
	h.Write([]byte(thread))
	return hex.EncodeToString(h.Sum(nil))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return "unknown"
}
