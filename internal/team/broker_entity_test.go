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
	"sync"
	"testing"
	"time"
)

// newEntityTestServer wires a full httptest server with the four /entity/*
// routes plus a running wiki worker and a stubbed synthesizer (so no
// real LLM shellout happens in tests).
func newEntityTestServer(t *testing.T, llmStub func(ctx context.Context, sys, user string) (string, error)) (*httptest.Server, *Broker, *entityPublisherStub, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := NewBroker()
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	pub := &entityPublisherStub{}
	factLog := NewFactLog(worker)
	if llmStub == nil {
		llmStub = func(context.Context, string, string) (string, error) {
			return "# Stubbed brief\n\nSynthesized.\n", nil
		}
	}
	synth := NewEntitySynthesizer(worker, factLog, pub, SynthesizerConfig{
		Threshold: 2,
		Timeout:   5 * time.Second,
		LLMCall:   llmStub,
	})
	synth.Start(context.Background())
	b.SetEntitySynthesizer(factLog, synth)

	mux := http.NewServeMux()
	mux.HandleFunc("/entity/fact", b.requireAuth(b.handleEntityFact))
	mux.HandleFunc("/entity/brief/synthesize", b.requireAuth(b.handleEntityBriefSynthesize))
	mux.HandleFunc("/entity/facts", b.requireAuth(b.handleEntityFactsList))
	mux.HandleFunc("/entity/briefs", b.requireAuth(b.handleEntityBriefsList))
	srv := httptest.NewServer(mux)
	return srv, b, pub, func() {
		srv.Close()
		synth.Stop()
		cancel()
		worker.Stop()
	}
}

func TestBrokerEntity_FactHappyPathPublishesEvent(t *testing.T) {
	srv, b, pub, teardown := newEntityTestServer(t, nil)
	defer teardown()

	// Subscribe BEFORE posting so we don't miss the event.
	events, unsub := b.SubscribeEntityFactEvents(16)
	defer unsub()

	payload, _ := json.Marshal(map[string]any{
		"entity_kind": "people",
		"entity_slug": "nazz",
		"fact":        "Ex-HubSpot PM.",
		"recorded_by": "pm",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	var resp struct {
		FactID           string `json:"fact_id"`
		FactCount        int    `json:"fact_count"`
		ThresholdCrossed bool   `json:"threshold_crossed"`
	}
	_ = json.NewDecoder(res.Body).Decode(&resp)
	if resp.FactID == "" {
		t.Fatalf("no fact_id")
	}
	if resp.FactCount != 1 {
		t.Fatalf("fact_count=%d", resp.FactCount)
	}
	if resp.ThresholdCrossed {
		t.Fatalf("threshold should not be crossed at count=1 (threshold=2)")
	}

	select {
	case evt := <-events:
		if evt.FactID != resp.FactID {
			t.Fatalf("event fact_id mismatch: %s vs %s", evt.FactID, resp.FactID)
		}
		if evt.ThresholdCrossed {
			t.Fatalf("event shouldn't flag threshold crossed yet")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected entity:fact_recorded SSE event within 2s")
	}
	_ = pub
}

func TestBrokerEntity_FactThresholdTriggersSynthesis(t *testing.T) {
	var synthCalls sync.WaitGroup
	synthCalls.Add(1)
	once := sync.Once{}
	srv, b, pub, teardown := newEntityTestServer(t, func(ctx context.Context, sys, user string) (string, error) {
		once.Do(func() { synthCalls.Done() })
		return "# Acme\n\nSynth OK.\n", nil
	})
	defer teardown()

	// Fire two facts — threshold is 2, so the second append triggers synthesis.
	for i := 0; i < 2; i++ {
		payload, _ := json.Marshal(map[string]any{
			"entity_kind": "companies",
			"entity_slug": "acme",
			"fact":        "fact " + string(rune('A'+i)),
			"recorded_by": "pm",
		})
		req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post fact: %v", err)
		}
		res.Body.Close()
	}

	// Wait for synthesis to run.
	done := make(chan struct{})
	go func() { synthCalls.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("synthesis not triggered within 3s")
	}
	// Give the worker a tick to commit.
	time.Sleep(300 * time.Millisecond)
	if pub.briefCount() == 0 {
		t.Fatal("expected a brief synthesis after threshold crossed")
	}
}

func TestBrokerEntity_FactValidationErrors(t *testing.T) {
	srv, b, _, teardown := newEntityTestServer(t, nil)
	defer teardown()

	cases := []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"bad kind", map[string]any{"entity_kind": "widgets", "entity_slug": "x", "fact": "y", "recorded_by": "pm"}, http.StatusBadRequest},
		{"bad slug", map[string]any{"entity_kind": "people", "entity_slug": "X!", "fact": "y", "recorded_by": "pm"}, http.StatusBadRequest},
		{"empty fact", map[string]any{"entity_kind": "people", "entity_slug": "x", "fact": "  ", "recorded_by": "pm"}, http.StatusBadRequest},
		{"missing recorded_by", map[string]any{"entity_kind": "people", "entity_slug": "x", "fact": "y"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.body)
			req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("http: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				body, _ := io.ReadAll(res.Body)
				t.Fatalf("want %d got %d body=%s", tc.status, res.StatusCode, body)
			}
		})
	}
}

func TestBrokerEntity_FactRequiresAuth(t *testing.T) {
	srv, _, _, teardown := newEntityTestServer(t, nil)
	defer teardown()
	payload, _ := json.Marshal(map[string]any{"entity_kind": "people", "entity_slug": "x", "fact": "y", "recorded_by": "pm"})
	res, err := http.Post(srv.URL+"/entity/fact", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401; got %d", res.StatusCode)
	}
}

func TestBrokerEntity_FactMethodCheck(t *testing.T) {
	srv, b, _, teardown := newEntityTestServer(t, nil)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/entity/fact", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405; got %d", res.StatusCode)
	}
}

func TestBrokerEntity_SynthesizeExplicitQueue(t *testing.T) {
	srv, b, pub, teardown := newEntityTestServer(t, nil)
	defer teardown()

	// Append one fact first so the synth has input.
	payload, _ := json.Marshal(map[string]any{
		"entity_kind": "customers",
		"entity_slug": "northstar",
		"fact":        "Expanded contract.",
		"recorded_by": "pm",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	res.Body.Close()

	// Explicit synth request.
	synthPayload, _ := json.Marshal(map[string]any{
		"entity_kind": "customers",
		"entity_slug": "northstar",
		"actor_slug":  "human",
	})
	req, _ = authReq(http.MethodPost, srv.URL+"/entity/brief/synthesize", bytes.NewReader(synthPayload), b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	var resp struct {
		SynthesisID uint64 `json:"synthesis_id"`
		QueuedAt    string `json:"queued_at"`
	}
	_ = json.NewDecoder(res.Body).Decode(&resp)
	if resp.QueuedAt == "" {
		t.Fatalf("missing queued_at")
	}

	// Wait for synthesis to commit.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if pub.briefCount() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("synthesis did not complete within 3s")
}

func TestBrokerEntity_FactsList(t *testing.T) {
	srv, b, _, teardown := newEntityTestServer(t, nil)
	defer teardown()

	for _, f := range []string{"A", "B", "C"} {
		payload, _ := json.Marshal(map[string]any{
			"entity_kind": "people", "entity_slug": "pm", "fact": f, "recorded_by": "pm",
		})
		req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("http: %v", err)
		}
		res.Body.Close()
	}
	req, _ := authReq(http.MethodGet, srv.URL+"/entity/facts?kind=people&slug=pm", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var resp struct {
		Facts []Fact `json:"facts"`
	}
	_ = json.NewDecoder(res.Body).Decode(&resp)
	if len(resp.Facts) != 3 {
		t.Fatalf("want 3 facts, got %d", len(resp.Facts))
	}
}

func TestBrokerEntity_FactsListValidation(t *testing.T) {
	srv, b, _, teardown := newEntityTestServer(t, nil)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/entity/facts?kind=invalid&slug=x", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400; got %d", res.StatusCode)
	}
}

func TestBrokerEntity_BriefsList(t *testing.T) {
	srv, b, pub, teardown := newEntityTestServer(t, nil)
	defer teardown()

	// Seed a brief via fact + explicit synth.
	payload, _ := json.Marshal(map[string]any{
		"entity_kind": "people", "entity_slug": "ceo", "fact": "Founder.", "recorded_by": "pm",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	res.Body.Close()

	synthP, _ := json.Marshal(map[string]any{"entity_kind": "people", "entity_slug": "ceo", "actor_slug": "human"})
	req, _ = authReq(http.MethodPost, srv.URL+"/entity/brief/synthesize", bytes.NewReader(synthP), b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	res.Body.Close()

	// Wait for brief.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && pub.briefCount() == 0 {
		time.Sleep(20 * time.Millisecond)
	}

	req, _ = authReq(http.MethodGet, srv.URL+"/entity/briefs", nil, b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var resp struct {
		Briefs []BriefSummary `json:"briefs"`
	}
	_ = json.NewDecoder(res.Body).Decode(&resp)
	if len(resp.Briefs) == 0 {
		t.Fatalf("expected at least 1 brief")
	}
	found := false
	for _, br := range resp.Briefs {
		if br.Kind == EntityKindPeople && br.Slug == "ceo" {
			found = true
			if br.FactCount < 1 {
				t.Errorf("fact_count=%d", br.FactCount)
			}
			if br.LastSynthesizedTS == "" {
				t.Errorf("missing last_synthesized_ts")
			}
		}
	}
	if !found {
		t.Fatalf("briefs list did not contain people/ceo: %+v", resp.Briefs)
	}
}

func TestBrokerEntity_WorkerDownReturns503(t *testing.T) {
	b := NewBroker()
	mux := http.NewServeMux()
	mux.HandleFunc("/entity/fact", b.requireAuth(b.handleEntityFact))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	payload, _ := json.Marshal(map[string]any{"entity_kind": "people", "entity_slug": "x", "fact": "y", "recorded_by": "pm"})
	req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503; got %d", res.StatusCode)
	}
}

// Ensures X-WUPHF-Agent is the recorded_by fallback when the body omits it.
func TestBrokerEntity_FactFallbackToAgentHeader(t *testing.T) {
	srv, b, _, teardown := newEntityTestServer(t, nil)
	defer teardown()
	payload, _ := json.Marshal(map[string]any{
		"entity_kind": "people", "entity_slug": "x", "fact": "y",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
	req.Header.Set("X-WUPHF-Agent", "sales")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d body=%s", res.StatusCode, body)
	}
	// Confirm the fact was recorded by the header slug.
	req, _ = authReq(http.MethodGet, srv.URL+"/entity/facts?kind=people&slug=x", nil, b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	var r struct {
		Facts []Fact `json:"facts"`
	}
	_ = json.NewDecoder(res.Body).Decode(&r)
	if len(r.Facts) != 1 || r.Facts[0].RecordedBy != "sales" {
		t.Fatalf("expected recorded_by=sales; got %+v", r.Facts)
	}
	_ = strings.TrimSpace
}
