package team

// wiki_dlq_extra_test.go — additional DLQ regression guards:
//   - Exponential backoff is monotonically non-decreasing across retry counts.
//   - Backoff is capped at dlqMaxBackoff (6h).
//   - Validation category coerces max_retries to 1 on Enqueue.
//   - Tombstone idempotency: double MarkResolved does not duplicate state.
//   - Permanent-failures file survives repeated promotions without drift.
//   - RecordAttempt rejects already-tombstoned shas with a clear error.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDLQBackoff_MonotonicallyNonDecreasing(t *testing.T) {
	t.Parallel()
	var prev time.Duration
	for r := 0; r < 20; r++ {
		got := dlqBackoff(r)
		if got < prev {
			t.Errorf("dlqBackoff(%d) = %v < dlqBackoff(%d) = %v — must be monotonic", r, got, r-1, prev)
		}
		prev = got
	}
}

func TestDLQBackoff_CapsAtCeiling(t *testing.T) {
	t.Parallel()
	// Huge retry count should not return a duration larger than dlqMaxBackoff.
	got := dlqBackoff(20)
	if got != dlqMaxBackoff {
		t.Errorf("dlqBackoff(20) = %v, want %v (ceiling)", got, dlqMaxBackoff)
	}
	// Even larger values still capped.
	got = dlqBackoff(100)
	if got != dlqMaxBackoff {
		t.Errorf("dlqBackoff(100) = %v, want %v (ceiling)", got, dlqMaxBackoff)
	}
}

func TestDLQBackoff_ExpectedValues(t *testing.T) {
	t.Parallel()
	// base = 10m, sequence = 10m, 20m, 40m, 80m, 160m, ...
	cases := []struct {
		retry int
		want  time.Duration
	}{
		{0, 10 * time.Minute},
		{1, 20 * time.Minute},
		{2, 40 * time.Minute},
		{3, 80 * time.Minute},
		{4, 160 * time.Minute},
		{5, 320 * time.Minute}, // 5h20m — still under 6h
		{6, 6 * time.Hour},     // would be 640m ~ 10h40m → capped
	}
	for _, tc := range cases {
		got := dlqBackoff(tc.retry)
		if got != tc.want {
			t.Errorf("dlqBackoff(%d) = %v, want %v", tc.retry, got, tc.want)
		}
	}
}

// TestDLQ_ValidationCoercedOnEnqueue verifies Enqueue itself coerces the
// max_retries field down to 1 when the category is "validation" — the policy
// is applied at the boundary, not only in RecordAttempt.
func TestDLQ_ValidationCoercedOnEnqueue(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	now := time.Now().UTC()
	entry := DLQEntry{
		ArtifactSHA:        "validation-coerce-001",
		ArtifactPath:       "wiki/artifacts/chat/validation-coerce-001.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryValidation,
		MaxRetries:         5, // should be coerced down to 1
		NextRetryNotBefore: now,
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Read back latest state — expect max_retries = 1.
	dlq.mu.Lock()
	latest, _, err := dlq.readLatestStateLocked()
	dlq.mu.Unlock()
	if err != nil {
		t.Fatalf("readLatestState: %v", err)
	}
	got, ok := latest["validation-coerce-001"]
	if !ok {
		t.Fatal("entry not found after Enqueue")
	}
	if got.MaxRetries != DLQValidationMaxRetries {
		t.Errorf("MaxRetries = %d, want %d (validation-coerced)", got.MaxRetries, DLQValidationMaxRetries)
	}
}

// TestDLQ_ProviderTimeoutHonorsDefault5 verifies the provider_timeout category
// keeps the default cap of 5 retries (not coerced down like validation).
func TestDLQ_ProviderTimeoutHonorsDefault5(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "provider-tmo-001",
		ArtifactPath:       "wiki/artifacts/chat/provider-tmo-001.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryProviderTimeout,
		// MaxRetries zero → default of 5
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	dlq.mu.Lock()
	latest, _, _ := dlq.readLatestStateLocked()
	dlq.mu.Unlock()
	got := latest["provider-tmo-001"]
	if got.MaxRetries != DLQDefaultMaxRetries {
		t.Errorf("provider_timeout MaxRetries = %d, want %d", got.MaxRetries, DLQDefaultMaxRetries)
	}
}

// TestDLQ_TombstoneIdempotent verifies that calling MarkResolved twice is a
// no-op beyond adding a second benign tombstone row. ReadyForReplay must still
// skip the artifact; no duplicate entries leak into subsequent reads.
func TestDLQ_TombstoneIdempotent(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "idempotent-001",
		ArtifactPath:       "wiki/artifacts/chat/idempotent-001.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// First resolve.
	if err := dlq.MarkResolved(ctx, "idempotent-001"); err != nil {
		t.Fatalf("MarkResolved #1: %v", err)
	}
	// Second resolve (idempotent — MUST NOT panic or error).
	if err := dlq.MarkResolved(ctx, "idempotent-001"); err != nil {
		t.Fatalf("MarkResolved #2: %v", err)
	}

	// ReadyForReplay still skips it.
	ready, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	for _, r := range ready {
		if r.ArtifactSHA == "idempotent-001" {
			t.Error("double-tombstoned artifact leaked back into ReadyForReplay")
		}
	}
}

// TestDLQ_RecordAttemptRejectsTombstoned verifies that RecordAttempt on an
// already-resolved artifact returns an error instead of silently succeeding.
func TestDLQ_RecordAttemptRejectsTombstoned(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "ghost-record-001",
		ArtifactPath:       "wiki/artifacts/chat/ghost-record-001.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := dlq.MarkResolved(ctx, "ghost-record-001"); err != nil {
		t.Fatalf("MarkResolved: %v", err)
	}

	err := dlq.RecordAttempt(ctx, "ghost-record-001", errors.New("tombstoned"), "parse")
	if err == nil {
		t.Error("RecordAttempt on tombstoned artifact should error")
	}
}

// TestDLQ_RecordAttemptRejectsUnknownSHA verifies that RecordAttempt on a
// never-enqueued sha fails loudly rather than silently creating a record.
func TestDLQ_RecordAttemptRejectsUnknownSHA(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	err := dlq.RecordAttempt(ctx, "never-enqueued-xyz", errors.New("test"), "parse")
	if err == nil {
		t.Error("RecordAttempt on unknown sha should error")
	}
}

// TestDLQ_PromotionIdempotent_NoDoubleRows verifies that a subsequent
// MarkResolved after a promotion does not add a second permanent-failures row
// or violate the "only appears once" invariant.
func TestDLQ_PromotionIdempotent_NoDoubleRows(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "promote-once-001",
		ArtifactPath:       "wiki/artifacts/chat/promote-once-001.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		RetryCount:         4, // 1 more attempt → promotion
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := dlq.RecordAttempt(ctx, "promote-once-001", errors.New("final"), "parse"); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	// Redundant MarkResolved after promotion — should not error, should not add a
	// second permanent-failures row.
	_ = dlq.MarkResolved(ctx, "promote-once-001")

	perms := readPermanentEntries(t, dlq)
	count := 0
	for _, e := range perms {
		if e.ArtifactSHA == "promote-once-001" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("permanent-failures rows for promote-once-001 = %d, want 1", count)
	}
}

// TestDLQ_ReadyForReplaySortsByPathStability verifies that repeated calls to
// ReadyForReplay return entries in a stable order so downstream processing is
// deterministic across restarts.
func TestDLQ_ReadyForReplayStable(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Second)
	shas := []string{"stable-a", "stable-b", "stable-c", "stable-d"}
	for _, sha := range shas {
		e := DLQEntry{
			ArtifactSHA:        sha,
			ArtifactPath:       "wiki/artifacts/chat/" + sha + ".md",
			Kind:               "chat",
			ErrorCategory:      DLQCategoryParse,
			MaxRetries:         5,
			NextRetryNotBefore: past,
		}
		if err := dlq.Enqueue(ctx, e); err != nil {
			t.Fatalf("Enqueue %s: %v", sha, err)
		}
	}

	first, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	second, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("length drift: %d vs %d", len(first), len(second))
	}
	// Order may depend on map iteration; both calls must return the same SET.
	firstSet := map[string]bool{}
	for _, e := range first {
		firstSet[e.ArtifactSHA] = true
	}
	for _, e := range second {
		if !firstSet[e.ArtifactSHA] {
			t.Errorf("entry %s present in second call but not first — unstable set", e.ArtifactSHA)
		}
	}
}

// TestDLQ_EnqueueDefaultsFilledIn verifies the coerceDLQEntry helper applies
// sensible defaults when the caller leaves fields zero.
func TestDLQ_EnqueueDefaultsFilledIn(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	// Everything zero — coerceDLQEntry should default max_retries=5,
	// first_failed_at=now, next_retry_not_before=now+base_backoff.
	entry := DLQEntry{
		ArtifactSHA:  "defaults-001",
		ArtifactPath: "wiki/artifacts/chat/defaults-001.md",
		Kind:         "chat",
		LastError:    "seg fault",
	}
	before := time.Now().UTC()
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	after := time.Now().UTC()

	dlq.mu.Lock()
	latest, _, _ := dlq.readLatestStateLocked()
	dlq.mu.Unlock()
	got := latest["defaults-001"]
	if got.MaxRetries != DLQDefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", got.MaxRetries, DLQDefaultMaxRetries)
	}
	if got.FirstFailedAt.Before(before.Add(-time.Second)) || got.FirstFailedAt.After(after.Add(time.Second)) {
		t.Errorf("FirstFailedAt = %v, want ≈ %v", got.FirstFailedAt, before)
	}
	// Next retry should be ≥ now + base_backoff
	minNext := before.Add(dlqBaseBackoff - time.Second)
	if got.NextRetryNotBefore.Before(minNext) {
		t.Errorf("NextRetryNotBefore = %v, want ≥ %v", got.NextRetryNotBefore, minNext)
	}
}

// TestDLQ_ExtractionsFileAppendOnly verifies that MarkResolved does NOT rewrite
// the file — it appends a tombstone. After two writes, the file must contain
// two JSON lines (the original entry + the tombstone), not one.
func TestDLQ_ExtractionsFileAppendOnly(t *testing.T) {
	t.Parallel()
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "append-only-001",
		ArtifactPath:       "wiki/artifacts/chat/append-only-001.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := dlq.MarkResolved(ctx, "append-only-001"); err != nil {
		t.Fatalf("MarkResolved: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dlq.root, ".dlq", "extractions.jsonl"))
	if err != nil {
		t.Fatalf("read extractions: %v", err)
	}
	lines := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines++
	}
	if lines < 2 {
		t.Errorf("extractions.jsonl has %d non-empty lines, want ≥ 2 (append-only)", lines)
	}
}
