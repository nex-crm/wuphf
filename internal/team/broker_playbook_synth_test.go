package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// newSynthBroker wires a broker with a live wiki worker + execution log +
// synthesizer backed by a stub LLM. Returns the broker and a teardown.
func newSynthBroker(
	t *testing.T,
	llm func(ctx context.Context, sys, user string) (string, error),
) (*Broker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	b.ensurePlaybookExecutionLog()
	execLog := b.PlaybookExecutionLog()
	if execLog == nil {
		t.Fatalf("execution log did not initialize")
	}

	synth := NewPlaybookSynthesizer(worker, execLog, b, PlaybookSynthesizerConfig{
		Threshold: 2,
		Timeout:   5 * time.Second,
		LLMCall:   llm,
	})
	synth.Start(context.Background())
	b.SetPlaybookSynthesizer(synth)

	teardown := func() {
		synth.Stop()
		cancel()
		worker.Stop()
	}
	return b, teardown
}

func TestHandlePlaybookSynthesize_EnqueuesJob(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- from test.\n", nil
	}
	b, teardown := newSynthBroker(t, stub)
	defer teardown()

	// Seed a source playbook so the synthesizer has something to update.
	writePlaybookSource(t, b.WikiWorker(), "reactivation", seededPlaybookBody)
	if _, err := b.PlaybookExecutionLog().Append(
		context.Background(), "reactivation", PlaybookOutcomeSuccess, "a run.", "", "cmo",
	); err != nil {
		t.Fatalf("append: %v", err)
	}

	srv := httptest.NewServer(b.requireAuth(b.handlePlaybookSynthesize))
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"slug":       "reactivation",
		"actor_slug": "human",
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := out["synthesis_id"]; !ok {
		t.Errorf("response missing synthesis_id: %v", out)
	}
	if _, ok := out["queued_at"]; !ok {
		t.Errorf("response missing queued_at: %v", out)
	}
}

func TestHandlePlaybookSynthesize_RejectsBadSlug(t *testing.T) {
	b, teardown := newSynthBroker(t, func(context.Context, string, string) (string, error) {
		return "## What we've learned\n\n- x\n", nil
	})
	defer teardown()

	srv := httptest.NewServer(b.requireAuth(b.handlePlaybookSynthesize))
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"slug": "Bad Slug With Spaces"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for bad slug; got %d", resp.StatusCode)
	}
}

func TestHandlePlaybookSynthesisStatus_ReflectsFrontmatter(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- initial synthesis.\n", nil
	}
	b, teardown := newSynthBroker(t, stub)
	defer teardown()

	writePlaybookSource(t, b.WikiWorker(), "status-demo", seededPlaybookBody)
	execLog := b.PlaybookExecutionLog()
	for i := 0; i < 3; i++ {
		if _, err := execLog.Append(
			context.Background(), "status-demo", PlaybookOutcomeSuccess, "run.", "", "cmo",
		); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// Fire a synthesis + wait for the SSE event to confirm commit landed.
	ch, unsub := b.SubscribePlaybookSynthesizedEvents(4)
	defer unsub()
	if _, err := b.PlaybookSynthesizer().SynthesizeNow(
		context.Background(), "status-demo", "human",
	); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for synthesis event")
	}

	srv := httptest.NewServer(b.requireAuth(b.handlePlaybookSynthesisStatus))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?slug=status-demo", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(raw))
	}

	var out struct {
		Slug                         string `json:"slug"`
		ExecutionCount               int    `json:"execution_count"`
		LastSynthesizedTS            string `json:"last_synthesized_ts"`
		LastSynthesizedSHA           string `json:"last_synthesized_sha"`
		ExecutionsSinceLastSynthesis int    `json:"executions_since_last_synthesis"`
		Threshold                    int    `json:"threshold"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Slug != "status-demo" {
		t.Errorf("slug = %q, want %q", out.Slug, "status-demo")
	}
	if out.ExecutionCount != 3 {
		t.Errorf("execution_count = %d, want 3", out.ExecutionCount)
	}
	if out.LastSynthesizedTS == "" {
		t.Errorf("last_synthesized_ts is empty; frontmatter not stamped")
	}
	if out.Threshold != 2 {
		t.Errorf("threshold = %d, want 2 (fixture override)", out.Threshold)
	}
	if out.ExecutionsSinceLastSynthesis != 0 {
		t.Errorf("expected 0 new executions since last synthesis; got %d", out.ExecutionsSinceLastSynthesis)
	}
}

func TestHandlePlaybookSynthesisStatus_MissingSource404(t *testing.T) {
	b, teardown := newSynthBroker(t, func(context.Context, string, string) (string, error) {
		return "", nil
	})
	defer teardown()

	srv := httptest.NewServer(b.requireAuth(b.handlePlaybookSynthesisStatus))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"?slug=nope", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for missing source; got %d", resp.StatusCode)
	}
}

func TestOnExecutionRecorded_ThresholdCrossingViaBrokerPublishes(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "## What we've learned\n\n- synthesis fired.\n", nil
	}
	b, teardown := newSynthBroker(t, stub)
	defer teardown()
	writePlaybookSource(t, b.WikiWorker(), "auto-through-broker", seededPlaybookBody)

	ch, unsub := b.SubscribePlaybookSynthesizedEvents(4)
	defer unsub()

	synth := b.PlaybookSynthesizer()
	execLog := b.PlaybookExecutionLog()

	// First execution: below threshold=2.
	if _, err := execLog.Append(
		context.Background(), "auto-through-broker", PlaybookOutcomeSuccess, "a.", "", "cmo",
	); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	synth.OnExecutionRecorded("auto-through-broker")

	// Second execution: crosses threshold.
	if _, err := execLog.Append(
		context.Background(), "auto-through-broker", PlaybookOutcomeSuccess, "b.", "", "cmo",
	); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	synth.OnExecutionRecorded("auto-through-broker")

	select {
	case evt := <-ch:
		if evt.Slug != "auto-through-broker" {
			t.Errorf("wrong slug: %q", evt.Slug)
		}
		if evt.ExecutionCount != 2 {
			t.Errorf("execution_count = %d, want 2", evt.ExecutionCount)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for synthesis event")
	}
}
