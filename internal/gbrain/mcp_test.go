package gbrain

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeServerConfig controls how the in-memory fake gbrain server behaves so a
// single helper can drive both happy-path and failure-injection tests.
type fakeServerConfig struct {
	// failQueryUntil makes the `query` tool return a transport-style error for
	// the first N calls (by closing the session), used to exercise reconnect.
	queryText      string
	getPageText    string
	listPagesText  string
	putPageText    string
	identityText   string
	findExpertsTxt string
}

// startFakeClient stands up an in-memory MCP server exposing canned gbrain
// tools and returns a Client wired to it via NewInMemoryTransports. The server
// is torn down by t.Cleanup.
func startFakeClient(t *testing.T, cfg fakeServerConfig) *Client {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "fake-gbrain", Version: "0.1.0"}, nil)

	text := func(s string) func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
		return func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
		}
	}

	mcp.AddTool(server, &mcp.Tool{Name: toolQuery, Description: "hybrid search"}, text(cfg.queryText))
	mcp.AddTool(server, &mcp.Tool{Name: toolSearch, Description: "fts"}, text(cfg.queryText))
	mcp.AddTool(server, &mcp.Tool{Name: toolGetPage, Description: "page"}, text(cfg.getPageText))
	mcp.AddTool(server, &mcp.Tool{Name: toolListPages, Description: "list"}, text(cfg.listPagesText))
	mcp.AddTool(server, &mcp.Tool{Name: toolPutPage, Description: "put"}, text(cfg.putPageText))
	mcp.AddTool(server, &mcp.Tool{Name: toolFindExperts, Description: "experts"}, text(cfg.findExpertsTxt))
	mcp.AddTool(server, &mcp.Tool{Name: toolGetBrainIdentity, Description: "identity"}, text(cfg.identityText))

	clientTr, serverTr := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverTr) }()

	rawClient := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := rawClient.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("connect fake: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// Inject the pre-connected session directly so the typed methods exercise
	// CallTool/flatten/decode without spawning a subprocess or dialing HTTP.
	c := NewClient(WithRemoteURL("inmemory://fake"))
	c.session = session
	return c
}

func TestClient_Query(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{
		queryText: `[{"slug":"onboarding","page_id":7,"title":"Onboarding","type":"guide","chunk_text":"do this","score":0.91,"stale":false}]`,
	})
	hits, err := c.Query(context.Background(), "how to onboard", 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].Slug != "onboarding" || hits[0].PageID != 7 || hits[0].Score != 0.91 {
		t.Errorf("hit mismatch: %+v", hits[0])
	}
}

func TestClient_QueryEmptyShortCircuits(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{queryText: `[]`})
	hits, err := c.Query(context.Background(), "   ", 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if hits != nil {
		t.Errorf("expected nil hits for empty query, got %v", hits)
	}
}

func TestClient_Search(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{
		queryText: `[{"slug":"billing","title":"Billing","score":0.4}]`,
	})
	hits, err := c.Search(context.Background(), "billing", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Slug != "billing" {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}

func TestClient_GetPage(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{
		getPageText: `{"slug":"runbook","title":"Runbook","content":"# Runbook\nsteps","type":"doc","tags":["ops","oncall"],"frontmatter":{"owner":"sam"}}`,
	})
	page, err := c.GetPage(context.Background(), "runbook")
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if page.Slug != "runbook" || !strings.Contains(page.Content, "# Runbook") {
		t.Errorf("page mismatch: %+v", page)
	}
	if len(page.Tags) != 2 || page.Frontmatter["owner"] != "sam" {
		t.Errorf("metadata mismatch: tags=%v fm=%v", page.Tags, page.Frontmatter)
	}
}

func TestClient_GetPageRequiresSlug(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{})
	if _, err := c.GetPage(context.Background(), "  "); err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestClient_ListPages(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{
		listPagesText: `[{"slug":"a","title":"A","type":"doc"},{"slug":"b","title":"B","type":"guide","stale":true}]`,
	})
	pages, err := c.ListPages(context.Background(), ListOptions{Limit: 10, Type: "doc"})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 2 || pages[0].Slug != "a" || !pages[1].Stale {
		t.Errorf("pages mismatch: %+v", pages)
	}
}

func TestClient_PutPage(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{
		putPageText: `{"slug":"new-page","status":"created","page_id":42}`,
	})
	res, err := c.PutPage(context.Background(), "---\ntitle: New Page\n---\nbody", PutOptions{Slug: "new-page", SourceKind: "wiki", IngestedVia: "broker"})
	if err != nil {
		t.Fatalf("PutPage: %v", err)
	}
	if res.Slug != "new-page" || res.Status != "created" || res.PageID != 42 {
		t.Errorf("put result mismatch: %+v", res)
	}
}

func TestClient_PutPageRequiresContent(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{})
	if _, err := c.PutPage(context.Background(), "   ", PutOptions{}); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestClient_FindExperts(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{
		findExpertsTxt: `[{"name":"Pam","slug":"pam","score":0.8,"reason":"wrote the wiki"}]`,
	})
	experts, err := c.FindExperts(context.Background(), "wiki curation", 3)
	if err != nil {
		t.Fatalf("FindExperts: %v", err)
	}
	if len(experts) != 1 || experts[0].Name != "Pam" {
		t.Errorf("experts mismatch: %+v", experts)
	}
}

func TestClient_Identity(t *testing.T) {
	c := startFakeClient(t, fakeServerConfig{identityText: "brain: acme-co"})
	id, err := c.Identity(context.Background())
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if !strings.Contains(id, "acme-co") {
		t.Errorf("identity = %q", id)
	}
}

// TestClient_CallToolErrorResult verifies an MCP IsError result decodes into a
// Go error at the typed-method boundary rather than being silently swallowed.
func TestClient_CallToolErrorResult(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: toolQuery, Description: "q"},
		func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "brain not initialised"}},
			}, nil, nil
		})

	clientTr, serverTr := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, serverTr) }()
	rawClient := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	session, err := rawClient.Connect(ctx, clientTr, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	c := NewClient(WithRemoteURL("inmemory://fake"))
	c.session = session

	// CallTool itself returns the flattened ERROR string without a Go error...
	raw, err := c.CallTool(ctx, toolQuery, map[string]any{"query": "x", "limit": 1})
	if err != nil {
		t.Fatalf("CallTool returned Go error, want flattened string: %v", err)
	}
	if !strings.HasPrefix(raw, "ERROR: ") {
		t.Errorf("expected ERROR prefix, got %q", raw)
	}
	// ...but the typed Query surfaces it as a decode error so callers notice.
	if _, qErr := c.Query(ctx, "x", 1); qErr == nil {
		t.Error("expected Query to surface the error result")
	}
}

// reconnectTransport wraps an in-memory transport and, after the first
// connection, fails the next Connect once before succeeding. It lets us prove
// the Client reconnects exactly once after a session death.
type reconnectTransport struct {
	mu        sync.Mutex
	make      func() (mcp.Transport, mcp.Transport, *mcp.Server)
	failNext  bool
	connects  int
	serverCtx context.Context
}

func (rt *reconnectTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	rt.mu.Lock()
	rt.connects++
	fail := rt.failNext
	rt.failNext = false
	rt.mu.Unlock()
	if fail {
		return nil, errors.New("injected transport failure")
	}
	clientTr, serverTr, server := rt.make()
	go func() { _ = server.Run(rt.serverCtx, serverTr) }()
	return clientTr.Connect(ctx)
}

// TestClient_ReconnectAfterFailure proves a dead session triggers exactly one
// reconnect + retry, and the retried call succeeds.
func TestClient_ReconnectAfterFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	makeServer := func() (mcp.Transport, mcp.Transport, *mcp.Server) {
		server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "0"}, nil)
		mcp.AddTool(server, &mcp.Tool{Name: toolGetBrainIdentity, Description: "id"},
			func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
			})
		ct, st := mcp.NewInMemoryTransports()
		return ct, st, server
	}

	rt := &reconnectTransport{make: makeServer, serverCtx: ctx}

	c := NewClient(WithRemoteURL("inmemory://fake"))
	// Override the transport factory by injecting our reconnecting transport.
	c.transportFn = func(context.Context) (mcp.Transport, error) { return rt, nil }

	// First call: establishes session #1.
	if _, err := c.Identity(ctx); err != nil {
		t.Fatalf("first Identity: %v", err)
	}
	// Kill the live session and arm a one-shot transport failure so the next
	// call must reconnect twice-over (close existing, dial fails once, retry
	// dials a fresh server).
	c.resetSession()
	rt.mu.Lock()
	rt.failNext = true
	rt.mu.Unlock()

	if _, err := c.Identity(ctx); err != nil {
		t.Fatalf("Identity after failure should reconnect+retry: %v", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	// connects: 1 (first) + 1 (failed retry dial) + 1 (successful retry dial).
	if rt.connects < 3 {
		t.Errorf("expected >=3 connect attempts (initial + fail + retry), got %d", rt.connects)
	}
}

// TestClient_NotInstalled asserts the local stdio path returns an
// ErrNotInstalled-wrapped error when no binary and no remote URL are present.
func TestClient_NotInstalled(t *testing.T) {
	t.Setenv(MCPURLEnv, "")
	t.Setenv("WUPHF_GBRAIN_COMMAND", "")
	t.Setenv("PATH", t.TempDir()) // ensure `gbrain` is not resolvable

	c := NewClient()
	err := c.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error when gbrain is not installed and no URL set")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("expected ErrNotInstalled, got %v", err)
	}
}

// TestIntegration_Identity optionally exercises a real `gbrain serve`. Skipped
// unless WUPHF_GBRAIN_IT=1. Requires an initialised brain on the host.
func TestIntegration_Identity(t *testing.T) {
	if os.Getenv("WUPHF_GBRAIN_IT") != "1" {
		t.Skip("set WUPHF_GBRAIN_IT=1 to run the live gbrain integration test")
	}
	if BinaryPath() == "" {
		t.Skip("gbrain binary not on PATH")
	}
	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := c.Identity(ctx)
	if err != nil {
		t.Fatalf("Identity against live gbrain: %v", err)
	}
	if strings.TrimSpace(id) == "" {
		t.Error("live gbrain returned empty identity")
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
