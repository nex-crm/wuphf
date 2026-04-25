package team

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestDLQ(t *testing.T) *DLQ {
	t.Helper()
	return NewDLQ(t.TempDir())
}

// readPermanentEntries scans permanent-failures.jsonl and returns DLQEntry rows.
func readPermanentEntries(t *testing.T, dlq *DLQ) []DLQEntry {
	t.Helper()
	path := filepath.Join(dlq.root, ".dlq", "permanent-failures.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("permanent-failures.jsonl not found: %v", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	var out []DLQEntry
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e DLQEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if e.ArtifactSHA != "" {
			out = append(out, e)
		}
	}
	return out
}

func TestDLQ_EnqueueAndReadyForReplay(t *testing.T) {
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "aaa111",
		ArtifactPath:       "wiki/artifacts/chat/aaa111.md",
		Kind:               "chat",
		LastError:          "json_parse_error",
		ErrorCategory:      DLQCategoryParse,
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second), // already eligible
	}

	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ready, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	if len(ready) != 1 {
		t.Fatalf("want 1 ready entry; got %d", len(ready))
	}
	if ready[0].ArtifactSHA != "aaa111" {
		t.Errorf("sha: want %q got %q", "aaa111", ready[0].ArtifactSHA)
	}
}

func TestDLQ_BackoffWindowNotElapsed(t *testing.T) {
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "bbb222",
		ArtifactPath:       "wiki/artifacts/chat/bbb222.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(time.Hour), // not yet eligible
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	ready, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	if len(ready) != 0 {
		t.Errorf("want 0 ready entries (window not elapsed); got %d", len(ready))
	}
}

func TestDLQ_RecordAttempt_IncrementsRetryCount(t *testing.T) {
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "ccc333",
		ArtifactPath:       "wiki/artifacts/chat/ccc333.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if err := dlq.RecordAttempt(ctx, "ccc333", errors.New("parse fail again"), "parse"); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	// After RecordAttempt, retry_count=1 → backoff ≈ 20 min → not eligible now.
	ready, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	for _, r := range ready {
		if r.ArtifactSHA == "ccc333" {
			t.Error("entry should be in backoff after RecordAttempt, not ready")
		}
	}
}

func TestDLQ_MaxRetries_PromotesToPermanent(t *testing.T) {
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "ddd444",
		ArtifactPath:       "wiki/artifacts/chat/ddd444.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		RetryCount:         4, // already at 4/5
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// One more attempt → retry_count becomes 5 = max_retries → promotion.
	if err := dlq.RecordAttempt(ctx, "ddd444", errors.New("still broken"), "parse"); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	// Must not appear in ReadyForReplay.
	ready, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	for _, r := range ready {
		if r.ArtifactSHA == "ddd444" {
			t.Error("promoted artifact must not appear in ReadyForReplay")
		}
	}

	// permanent-failures.jsonl must contain the entry.
	perms := readPermanentEntries(t, dlq)
	found := false
	for _, e := range perms {
		if e.ArtifactSHA == "ddd444" {
			found = true
		}
	}
	if !found {
		t.Error("promoted artifact must appear in permanent-failures.jsonl")
	}
}

func TestDLQ_ValidationCategory_MaxRetriesCoercedToOne(t *testing.T) {
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "eee555",
		ArtifactPath:       "wiki/artifacts/chat/eee555.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryValidation,
		MaxRetries:         5, // coerced to 1 by Enqueue
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// A single RecordAttempt crosses max_retries=1 → promoted.
	if err := dlq.RecordAttempt(ctx, "eee555", errors.New("schema violation"), "validation"); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	ready, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	for _, r := range ready {
		if r.ArtifactSHA == "eee555" {
			t.Error("validation artifact should be promoted after 1 attempt")
		}
	}
}

func TestDLQ_MarkResolved_SkipsInReadyForReplay(t *testing.T) {
	dlq := newTestDLQ(t)
	ctx := context.Background()

	entry := DLQEntry{
		ArtifactSHA:        "fff666",
		ArtifactPath:       "wiki/artifacts/chat/fff666.md",
		Kind:               "chat",
		ErrorCategory:      DLQCategoryParse,
		MaxRetries:         5,
		NextRetryNotBefore: time.Now().UTC().Add(-time.Second),
	}
	if err := dlq.Enqueue(ctx, entry); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := dlq.MarkResolved(ctx, "fff666"); err != nil {
		t.Fatalf("MarkResolved: %v", err)
	}

	ready, err := dlq.ReadyForReplay(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadyForReplay: %v", err)
	}
	for _, r := range ready {
		if r.ArtifactSHA == "fff666" {
			t.Error("resolved artifact must not appear in ReadyForReplay")
		}
	}
}
