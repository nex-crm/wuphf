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

// newEntityGraphTestServer mirrors newEntityTestServer but also wires an
// EntityGraph so /entity/graph and the fact-recorded hook are exercised
// end-to-end.
func newEntityGraphTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
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
	graph := NewEntityGraph(worker)
	synth := NewEntitySynthesizer(worker, factLog, pub, SynthesizerConfig{
		Threshold: 99,
		Timeout:   5 * time.Second,
		Graph:     graph,
		LLMCall: func(context.Context, string, string) (string, error) {
			return "# stub\n", nil
		},
	})
	synth.Start(context.Background())
	b.SetEntitySynthesizer(factLog, synth)
	b.SetEntityGraph(graph)

	mux := http.NewServeMux()
	mux.HandleFunc("/entity/fact", b.requireAuth(b.handleEntityFact))
	mux.HandleFunc("/entity/graph", b.requireAuth(b.handleEntityGraph))
	srv := httptest.NewServer(mux)
	return srv, b, func() {
		srv.Close()
		synth.Stop()
		cancel()
		worker.Stop()
	}
}

func TestEntityGraphEndpoint_FactRecordPopulatesGraph(t *testing.T) {
	srv, b, teardown := newEntityGraphTestServer(t)
	defer teardown()

	payload, _ := json.Marshal(map[string]any{
		"entity_kind": "people",
		"entity_slug": "sarah",
		"fact":        "Works at [[companies/acme]] as PM.",
		"recorded_by": "pm",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/entity/fact", bytes.NewReader(payload), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post fact: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("fact status=%d body=%s", res.StatusCode, body)
	}

	// Out-edges from sarah should include acme.
	req, _ = authReq(http.MethodGet, srv.URL+"/entity/graph?kind=people&slug=sarah&direction=out", nil, b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get graph: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("graph status=%d body=%s", res.StatusCode, body)
	}
	var gr struct {
		Edges []struct {
			FromKind string `json:"from_kind"`
			FromSlug string `json:"from_slug"`
			ToKind   string `json:"to_kind"`
			ToSlug   string `json:"to_slug"`
		} `json:"edges"`
		Direction string `json:"direction"`
	}
	if err := json.NewDecoder(res.Body).Decode(&gr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if gr.Direction != "out" {
		t.Errorf("direction=%q", gr.Direction)
	}
	if len(gr.Edges) != 1 {
		t.Fatalf("want 1 edge; got %d: %+v", len(gr.Edges), gr.Edges)
	}
	e := gr.Edges[0]
	if e.FromKind != "people" || e.FromSlug != "sarah" || e.ToKind != "companies" || e.ToSlug != "acme" {
		t.Errorf("edge shape wrong: %+v", e)
	}
}

func TestEntityGraphEndpoint_ValidatesDirection(t *testing.T) {
	srv, b, teardown := newEntityGraphTestServer(t)
	defer teardown()

	req, _ := authReq(http.MethodGet, srv.URL+"/entity/graph?kind=people&slug=x&direction=sideways", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400; got %d", res.StatusCode)
	}
}

func TestEntityGraphEndpoint_MissingGraphReturns503(t *testing.T) {
	b := NewBroker()
	mux := http.NewServeMux()
	mux.HandleFunc("/entity/graph", b.requireAuth(b.handleEntityGraph))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := authReq(http.MethodGet, srv.URL+"/entity/graph?kind=people&slug=x", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503; got %d", res.StatusCode)
	}
}
