package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newReviewCandidatesTestServer wires the same broker surface as the
// promotion-demand integration test, but only exposes /notebook/review-candidates
// so the GET/POST contract is exercised in isolation.
func newReviewCandidatesTestServer(t *testing.T, withIndex bool) (*httptest.Server, *Broker, *NotebookDemandIndex, func()) {
	t.Helper()
	b := newTestBroker(t)
	var idx *NotebookDemandIndex
	if withIndex {
		demandPath := filepath.Join(t.TempDir(), "events.jsonl")
		var err error
		idx, err = NewNotebookDemandIndex(demandPath)
		if err != nil {
			t.Fatalf("NewNotebookDemandIndex: %v", err)
		}
		b.mu.Lock()
		b.demandIndex = idx
		b.mu.Unlock()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/review-candidates", b.requireAuth(b.handleNotebookReviewCandidates))
	srv := httptest.NewServer(mux)
	return srv, b, idx, srv.Close
}

func reviewGet(t *testing.T, srv *httptest.Server, token, query string) (*http.Response, []byte) {
	t.Helper()
	url := srv.URL + "/notebook/review-candidates"
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res, body
}

func reviewPost(t *testing.T, srv *httptest.Server, token, agent string, payload any) (*http.Response, []byte) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/notebook/review-candidates", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if agent != "" {
		req.Header.Set("X-WUPHF-Agent", agent)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res, body
}

func TestNotebookReviewCandidates_GET_NoIndex_503(t *testing.T) {
	srv, b, _, cleanup := newReviewCandidatesTestServer(t, false)
	defer cleanup()
	res, body := reviewGet(t, srv, b.Token(), "")
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "demand index not active") {
		t.Fatalf("expected 'demand index not active' in body, got %s", body)
	}
}

func TestNotebookReviewCandidates_GET_EmptyIndex(t *testing.T) {
	srv, b, _, cleanup := newReviewCandidatesTestServer(t, true)
	defer cleanup()
	res, body := reviewGet(t, srv, b.Token(), "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", res.StatusCode, body)
	}
	var resp struct {
		Candidates []DemandCandidate `json:"candidates"`
		Threshold  float64           `json:"threshold"`
		Window     int               `json:"window"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if len(resp.Candidates) != 0 {
		t.Fatalf("candidates = %v, want []", resp.Candidates)
	}
	if resp.Threshold <= 0 {
		t.Fatalf("threshold should be positive, got %v", resp.Threshold)
	}
	if resp.Window <= 0 {
		t.Fatalf("window should be positive, got %v", resp.Window)
	}
}

func TestNotebookReviewCandidates_GET_RankedByScore(t *testing.T) {
	srv, b, idx, cleanup := newReviewCandidatesTestServer(t, true)
	defer cleanup()

	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	idx.SetClockForTest(func() time.Time { return now })

	// pm entry: 2 distinct cross-agent searches → score 2.0
	mustRecord(t, idx, PromotionDemandEvent{
		EntryPath: "agents/pm/notebook/retro.md", OwnerSlug: "pm",
		SearcherSlug: "eng", Signal: DemandSignalCrossAgentSearch, RecordedAt: now,
	})
	mustRecord(t, idx, PromotionDemandEvent{
		EntryPath: "agents/pm/notebook/retro.md", OwnerSlug: "pm",
		SearcherSlug: "design", Signal: DemandSignalCrossAgentSearch, RecordedAt: now,
	})
	// eng entry: one channel-context-ask → score 2.0 (tied with pm) but
	// alphabetic tiebreak puts agents/eng/... first.
	mustRecord(t, idx, PromotionDemandEvent{
		EntryPath: "agents/eng/notebook/auth.md", OwnerSlug: "eng",
		SearcherSlug: "pm", Signal: DemandSignalChannelContextAsk, RecordedAt: now,
	})

	res, body := reviewGet(t, srv, b.Token(), "n=5")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", res.StatusCode, body)
	}
	var resp struct {
		Candidates []DemandCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if len(resp.Candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2; got %+v", len(resp.Candidates), resp.Candidates)
	}
	// Both at score 2.0; alphabetic tiebreak in TopCandidates puts eng first.
	if resp.Candidates[0].EntryPath != "agents/eng/notebook/auth.md" {
		t.Fatalf("candidates[0] = %q, want eng/auth.md", resp.Candidates[0].EntryPath)
	}
	if resp.Candidates[1].EntryPath != "agents/pm/notebook/retro.md" {
		t.Fatalf("candidates[1] = %q, want pm/retro.md", resp.Candidates[1].EntryPath)
	}
}

func TestNotebookReviewCandidates_GET_BadN(t *testing.T) {
	srv, b, _, cleanup := newReviewCandidatesTestServer(t, true)
	defer cleanup()
	res, body := reviewGet(t, srv, b.Token(), "n=not-a-number")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", res.StatusCode, body)
	}
}

func TestNotebookReviewCandidates_POST_RecordsCEOFlag(t *testing.T) {
	srv, b, idx, cleanup := newReviewCandidatesTestServer(t, true)
	defer cleanup()

	res, body := reviewPost(t, srv, b.Token(), "ceo", map[string]any{
		"entry_paths": []string{"agents/pm/notebook/retro.md"},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", res.StatusCode, body)
	}
	var resp struct {
		Recorded []string            `json:"recorded"`
		Skipped  []map[string]string `json:"skipped"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Recorded) != 1 {
		t.Fatalf("recorded = %v, want 1 path", resp.Recorded)
	}
	if got := idx.Score("agents/pm/notebook/retro.md"); got != 1.5 {
		t.Fatalf("score = %v, want 1.5 (single CEO flag)", got)
	}
}

func TestNotebookReviewCandidates_POST_DedupeSameDay(t *testing.T) {
	srv, b, idx, cleanup := newReviewCandidatesTestServer(t, true)
	defer cleanup()

	payload := map[string]any{
		"entry_paths": []string{"agents/pm/notebook/retro.md"},
	}
	for i := 0; i < 3; i++ {
		res, body := reviewPost(t, srv, b.Token(), "ceo", payload)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d status = %d; body=%s", i, res.StatusCode, body)
		}
	}
	if got := idx.Score("agents/pm/notebook/retro.md"); got != 1.5 {
		t.Fatalf("score = %v after 3 same-day flags, want 1.5 (deduped)", got)
	}
}

func TestNotebookReviewCandidates_POST_BadPath(t *testing.T) {
	srv, b, _, cleanup := newReviewCandidatesTestServer(t, true)
	defer cleanup()
	res, body := reviewPost(t, srv, b.Token(), "ceo", map[string]any{
		"entry_paths": []string{"team/already-promoted.md", "agents/pm/notebook/ok.md"},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", res.StatusCode, body)
	}
	var resp struct {
		Recorded []string            `json:"recorded"`
		Skipped  []map[string]string `json:"skipped"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Recorded) != 1 || resp.Recorded[0] != "agents/pm/notebook/ok.md" {
		t.Fatalf("recorded = %v, want one valid path", resp.Recorded)
	}
	if len(resp.Skipped) != 1 || resp.Skipped[0]["path"] != "team/already-promoted.md" {
		t.Fatalf("skipped = %v, want one entry for malformed path", resp.Skipped)
	}
}

func TestNotebookReviewCandidates_POST_EmptyBody(t *testing.T) {
	srv, b, _, cleanup := newReviewCandidatesTestServer(t, true)
	defer cleanup()
	res, body := reviewPost(t, srv, b.Token(), "ceo", map[string]any{
		"entry_paths": []string{},
	})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d; body=%s", res.StatusCode, body)
	}
}

func TestNotebookReviewCandidates_POST_NoIndex_503(t *testing.T) {
	srv, b, _, cleanup := newReviewCandidatesTestServer(t, false)
	defer cleanup()
	res, body := reviewPost(t, srv, b.Token(), "ceo", map[string]any{
		"entry_paths": []string{"agents/pm/notebook/retro.md"},
	})
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", res.StatusCode, body)
	}
}

func TestNotebookReviewCandidates_OwnerSlugExtraction(t *testing.T) {
	cases := []struct {
		path   string
		want   string
		wantOK bool
	}{
		{"agents/pm/notebook/retro.md", "pm", true},
		{"agents/eng/notebook/sub/dir/file.md", "eng", true},
		{"team/already-promoted.md", "", false},
		{"agents/pm/wiki/x.md", "", false},
		{"", "", false},
		{"agents//notebook/x.md", "", false},
		{"agents/pm", "", false},
	}
	for _, c := range cases {
		got, ok := ownerSlugFromNotebookPath(c.path)
		if ok != c.wantOK || got != c.want {
			t.Errorf("ownerSlugFromNotebookPath(%q) = (%q, %v); want (%q, %v)",
				c.path, got, ok, c.want, c.wantOK)
		}
	}
}

// mustRecord is a tiny helper used by review-candidates tests; the
// existing promotion_demand tests have their own assertion patterns and
// we keep these self-contained.
func mustRecord(t *testing.T, idx *NotebookDemandIndex, evt PromotionDemandEvent) {
	t.Helper()
	if err := idx.Record(evt); err != nil {
		t.Fatalf("Record(%+v): %v", evt, err)
	}
	// Wait for the in-memory bucket to reflect the write before tests
	// query it. Record is synchronous so this is a fast no-op, but the
	// helper isolates tests from any future async re-implementation.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = idx.WaitForCondition(ctx, func() bool {
		return idx.Score(evt.EntryPath) != 0
	})
}
