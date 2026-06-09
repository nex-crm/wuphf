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

// ErrUnsealed is returned when Deliver is handed a PackedDelegation that did not
// come from Pack. Only render() sets the seal, so an unsealed delegation skipped
// classification and must never be delivered.
var ErrUnsealed = errors.New("packer: refusing to deliver an unsealed delegation (must come from Pack)")

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

	// 0. Only a sealed delegation (produced by Pack -> render) may be delivered.
	//    A hand-constructed PackedDelegation skipped Classify, so it is refused.
	if !d.sealed {
		return InjectionRecord{}, ErrUnsealed
	}

	// 1. Idempotency. A SENT record short-circuits (already delivered). A PENDING
	//    record means an attempt is in flight or crashed mid-flight: do NOT
	//    re-post — return it for the caller/reconciler to resolve. Only an absent
	//    or FAILED record proceeds to a fresh attempt. The sink's Lookup+Write
	//    must be atomic in the real adapter (broker mutex / row lock) to fully
	//    close the concurrent-send window, and the bridge Post must be idempotent
	//    on the key so a post-success / write-failure retry cannot duplicate.
	if prior, ok := sink.Lookup(req.IdempotencyKey); ok {
		switch prior.Status {
		case DeliverySent, DeliveryPending:
			return prior, nil
		}
	}

	rec := d.Injection
	rec.IdempotencyKey = req.IdempotencyKey
	rec.Timestamp = now

	// 2. Bind delivery to the PACKED snapshot. The delegation's own
	//    InjectionRecord records the task / plan / profile / policy / identity /
	//    destination it was built for; refuse if the caller paired it with a
	//    different req (e.g. a stale high-trust payload + a fresh downgraded req).
	if err := snapshotMatches(d.Injection, req); err != nil {
		rec.Status = DeliveryFailed
		rec.FailureReason = "snapshot mismatch: " + err.Error()
		_ = sink.Write(rec)
		return rec, fmt.Errorf("packer: delivery aborted: %w", err)
	}

	// 3. Re-validate against LIVE state. A trust downgrade or task edit between
	//    Classify and Deliver must not ship stale-authorized context.
	if val != nil {
		if err := val.Validate(req); err != nil {
			rec.Status = DeliveryFailed
			rec.FailureReason = "snapshot stale: " + err.Error()
			_ = sink.Write(rec)
			return rec, fmt.Errorf("packer: delivery aborted: %w", err)
		}
	}

	// 4. Final redaction scan over the EXACT bytes that will leave. Render can
	//    reintroduce raw refs/titles a pre-render item scan never saw.
	finalMention := sc.Scan(d.MentionText)
	finalThread := sc.Scan(d.ThreadContext)
	if !finalMention.OK || !finalThread.OK {
		rec.Status = DeliveryFailed
		rec.FailureReason = "final egress scan: " + firstNonEmpty(finalMention.Reason, finalThread.Reason)
		_ = sink.Write(rec)
		return rec, ErrFinalScanFailed
	}

	final := PackedDelegation{
		MentionText:   finalMention.Content,
		ThreadContext: finalThread.Content,
		sealed:        true,
	}
	rec.RenderedHash = hashBytes(final.MentionText, final.ThreadContext)
	rec.TokenCount = estimateTokens(final.MentionText) + estimateTokens(final.ThreadContext)

	// 5. Pending -> post -> sent/failed.
	rec.Status = DeliveryPending
	if err := sink.Write(rec); err != nil {
		return rec, fmt.Errorf("packer: write pending audit: %w", err)
	}

	ts, err := br.Post(ctx, final, req.IdempotencyKey)
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

// snapshotMatches refuses a delivery whose packed snapshot does not correspond
// to the req it is being delivered under. This stops a caller from pairing a
// stale, higher-trust PackedDelegation with a fresh, downgraded req that happens
// to pass the live validator.
func snapshotMatches(rec InjectionRecord, req ContextRequest) error {
	switch {
	case rec.TaskID != req.TaskID:
		return fmt.Errorf("task id %q != %q", rec.TaskID, req.TaskID)
	case rec.TaskUpdatedAt != req.TaskUpdatedAt:
		return fmt.Errorf("task updated-at %q != %q", rec.TaskUpdatedAt, req.TaskUpdatedAt)
	case rec.PlanID != req.PlanID:
		return fmt.Errorf("plan id %q != %q", rec.PlanID, req.PlanID)
	case rec.PlanVersion != req.PlanVersion:
		return fmt.Errorf("plan version %d != %d", rec.PlanVersion, req.PlanVersion)
	case rec.BotTrust != req.Target.Trust:
		return fmt.Errorf("trust tier %d != %d", rec.BotTrust, req.Target.Trust)
	case rec.ProfileVersion != req.Target.Version:
		return fmt.Errorf("profile version %d != %d", rec.ProfileVersion, req.Target.Version)
	case rec.PolicyVersion != req.EgressPolicyVer:
		return fmt.Errorf("policy version %d != %d", rec.PolicyVersion, req.EgressPolicyVer)
	case rec.Identity != req.Target.Identity:
		return fmt.Errorf("identity tuple mismatch")
	case rec.WorkspaceID != req.Thread.WorkspaceID || rec.ChannelID != req.Thread.ChannelID || rec.ThreadTS != req.Thread.ThreadTS:
		return fmt.Errorf("destination mismatch")
	}
	return nil
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
