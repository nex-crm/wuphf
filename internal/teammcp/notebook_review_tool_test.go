package teammcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// reviewToolStubBroker mounts the two endpoints the team_notebook_review
// tool consumes. The handler may be supplied per-test to override behaviour
// (503, errors, etc).
type reviewToolStubBroker struct {
	mu              sync.Mutex
	getCalls        int
	postCalls       int
	postBodies      []string
	getN            []string
	candidatesGET   func(w http.ResponseWriter, r *http.Request)
	candidatesPOST  func(w http.ResponseWriter, r *http.Request)
	notebookReadGET func(w http.ResponseWriter, r *http.Request)
}

func newReviewToolStubBroker(t *testing.T, b *reviewToolStubBroker) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notebook/review-candidates":
			switch r.Method {
			case http.MethodGet:
				b.mu.Lock()
				b.getCalls++
				b.getN = append(b.getN, r.URL.Query().Get("n"))
				b.mu.Unlock()
				if b.candidatesGET != nil {
					b.candidatesGET(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"candidates": []map[string]any{},
					"threshold":  3.0,
					"window":     7,
				})
			case http.MethodPost:
				body, _ := io.ReadAll(r.Body)
				b.mu.Lock()
				b.postCalls++
				b.postBodies = append(b.postBodies, string(body))
				b.mu.Unlock()
				if b.candidatesPOST != nil {
					b.candidatesPOST(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"recorded": []string{},
					"skipped":  []map[string]string{},
				})
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		case "/notebook/read":
			if b.notebookReadGET != nil {
				b.notebookReadGET(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# default snippet\nbody body body"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setBrokerEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("WUPHF_TEAM_BROKER_URL", url)
	t.Setenv("WUPHF_BROKER_TOKEN", "test-token")
	t.Setenv("WUPHF_BROKER_TOKEN_FILE", "/dev/null")
}

func TestNotebookReviewTool_ReturnsRankedCandidates(t *testing.T) {
	stub := &reviewToolStubBroker{
		candidatesGET: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{
						"entry_path": "agents/eng/notebook/auth.md",
						"owner_slug": "eng",
						"score":      2.0,
						"top_signal": 1, // DemandSignalChannelContextAsk
					},
					{
						"entry_path": "agents/pm/notebook/retro.md",
						"owner_slug": "pm",
						"score":      3.0,
						"top_signal": 0, // DemandSignalCrossAgentSearch
					},
				},
				"threshold": 3.0,
				"window":    7,
			})
		},
		notebookReadGET: func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Query().Get("path")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("snippet for " + path))
		},
	}
	srv := newReviewToolStubBroker(t, stub)
	setBrokerEnv(t, srv.URL)

	res, _, err := handleTeamNotebookReview(context.Background(), nil, TeamNotebookReviewArgs{Limit: 5})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	payload := toolErrorText(res)
	var resp notebookReviewResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("unmarshal: %v; payload=%s", err, payload)
	}
	if len(resp.Candidates) != 2 {
		t.Fatalf("candidates len = %d; payload=%s", len(resp.Candidates), payload)
	}
	// Sorted by score desc — pm(3.0) before eng(2.0).
	if resp.Candidates[0].Path != "agents/pm/notebook/retro.md" {
		t.Fatalf("candidates[0].Path = %q, want pm/retro.md", resp.Candidates[0].Path)
	}
	if resp.Candidates[0].TopSignal != "cross_agent_search" {
		t.Fatalf("candidates[0].TopSignal = %q, want cross_agent_search", resp.Candidates[0].TopSignal)
	}
	if !strings.Contains(resp.Candidates[0].Snippet, "snippet for agents/pm/notebook/retro.md") {
		t.Fatalf("snippet missing for pm: %q", resp.Candidates[0].Snippet)
	}
	if resp.Candidates[0].PromoteURL != "/reviews?path=agents%2Fpm%2Fnotebook%2Fretro.md" {
		t.Fatalf("promote_url = %q", resp.Candidates[0].PromoteURL)
	}
	if resp.Threshold != 3.0 || resp.Window != 7 {
		t.Fatalf("threshold/window = %v/%v", resp.Threshold, resp.Window)
	}
	if stub.getCalls != 1 {
		t.Fatalf("getCalls = %d, want 1", stub.getCalls)
	}
	if stub.postCalls != 0 {
		t.Fatalf("postCalls = %d, want 0 (no flag)", stub.postCalls)
	}
	if got := stub.getN[0]; got != "5" {
		t.Fatalf("GET n = %q, want 5", got)
	}
}

func TestNotebookReviewTool_EmptyCandidates_FriendlyMessage(t *testing.T) {
	stub := &reviewToolStubBroker{}
	srv := newReviewToolStubBroker(t, stub)
	setBrokerEnv(t, srv.URL)

	res, _, err := handleTeamNotebookReview(context.Background(), nil, TeamNotebookReviewArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	var resp notebookReviewResponse
	if err := json.Unmarshal([]byte(toolErrorText(res)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Candidates) != 0 {
		t.Fatalf("candidates = %v, want empty", resp.Candidates)
	}
	if !strings.Contains(resp.Message, "No promotion candidates yet") {
		t.Fatalf("message = %q, want friendly empty message", resp.Message)
	}
}

func TestNotebookReviewTool_503_DegradesGracefully(t *testing.T) {
	stub := &reviewToolStubBroker{
		candidatesGET: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "demand index not active"})
		},
	}
	srv := newReviewToolStubBroker(t, stub)
	setBrokerEnv(t, srv.URL)

	res, _, err := handleTeamNotebookReview(context.Background(), nil, TeamNotebookReviewArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("expected success, got tool error: %s", toolErrorText(res))
	}
	var resp notebookReviewResponse
	if err := json.Unmarshal([]byte(toolErrorText(res)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp.Message, "not yet active") {
		t.Fatalf("message = %q, want degradation message", resp.Message)
	}
}

func TestNotebookReviewTool_FlagPostsCEOSignal(t *testing.T) {
	stub := &reviewToolStubBroker{
		candidatesPOST: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"recorded": []string{"agents/pm/notebook/retro.md"},
				"skipped":  []map[string]string{},
			})
		},
	}
	srv := newReviewToolStubBroker(t, stub)
	setBrokerEnv(t, srv.URL)

	res, _, err := handleTeamNotebookReview(context.Background(), nil, TeamNotebookReviewArgs{
		Flag: []string{"agents/pm/notebook/retro.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	if stub.postCalls != 1 {
		t.Fatalf("postCalls = %d, want 1", stub.postCalls)
	}
	if !strings.Contains(stub.postBodies[0], "\"entry_paths\":[\"agents/pm/notebook/retro.md\"]") {
		t.Fatalf("post body did not include entry_paths: %s", stub.postBodies[0])
	}
	var resp notebookReviewResponse
	if err := json.Unmarshal([]byte(toolErrorText(res)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !slices.Contains(resp.Flagged, "agents/pm/notebook/retro.md") {
		t.Fatalf("flagged = %v, want pm/retro.md", resp.Flagged)
	}
}

func TestNotebookReviewTool_Flag503_DegradesGracefully(t *testing.T) {
	stub := &reviewToolStubBroker{
		candidatesPOST: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "demand index not active"})
		},
	}
	srv := newReviewToolStubBroker(t, stub)
	setBrokerEnv(t, srv.URL)

	res, _, err := handleTeamNotebookReview(context.Background(), nil, TeamNotebookReviewArgs{
		Flag: []string{"agents/pm/notebook/retro.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	if stub.getCalls != 0 {
		t.Fatalf("getCalls = %d after 503 flag, want 0 (skipped)", stub.getCalls)
	}
	var resp notebookReviewResponse
	if err := json.Unmarshal([]byte(toolErrorText(res)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp.Message, "not yet active") {
		t.Fatalf("message = %q, want degradation", resp.Message)
	}
}

func TestNotebookReviewTool_DedupesAndTrimsFlag(t *testing.T) {
	stub := &reviewToolStubBroker{
		candidatesPOST: func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"recorded": []string{"agents/pm/notebook/retro.md"},
				"skipped":  []map[string]string{},
			})
		},
	}
	srv := newReviewToolStubBroker(t, stub)
	setBrokerEnv(t, srv.URL)

	_, _, err := handleTeamNotebookReview(context.Background(), nil, TeamNotebookReviewArgs{
		Flag: []string{"agents/pm/notebook/retro.md", "  agents/pm/notebook/retro.md  ", "", "  "},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if stub.postCalls != 1 {
		t.Fatalf("postCalls = %d, want 1 after dedupe", stub.postCalls)
	}
	body := stub.postBodies[0]
	// Only one unique path should be present.
	if strings.Count(body, "agents/pm/notebook/retro.md") != 1 {
		t.Fatalf("expected exactly one occurrence of dedupe path in body: %s", body)
	}
}

func TestNotebookReviewTool_RegisteredOnlyForCEO(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	cases := []struct {
		name     string
		channel  string
		oneOnOne bool
		slug     string
		want     bool
	}{
		{"office non-lead", "general", false, "workflow-architect", false},
		{"dm non-lead", "dm-eng", true, "workflow-architect", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names := listRegisteredToolsWithSlug(t, tc.slug, tc.channel, tc.oneOnOne)
			has := slices.Contains(names, "team_notebook_review")
			if has != tc.want {
				t.Errorf("present = %v; want %v; tools=%v", has, tc.want, names)
			}
		})
	}
}

func TestNotebookReviewTool_RegisteredForCEOInOfficeAndDM(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	cases := []struct {
		name     string
		channel  string
		oneOnOne bool
		slug     string
	}{
		{"office ceo", "general", false, "ceo"},
		{"dm ceo", "dm-ceo", true, "ceo"},
		{"office empty-slug lead", "general", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names := listRegisteredToolsWithSlug(t, tc.slug, tc.channel, tc.oneOnOne)
			if !slices.Contains(names, "team_notebook_review") {
				t.Errorf("expected team_notebook_review for ceo; got %v", names)
			}
		})
	}
}

func TestTruncateOnWordBoundary(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"empty", "", 200, ""},
		{"short", "hello world", 200, "hello world"},
		{"trims at word boundary", "the quick brown fox jumps", 14, "the quick…"},
		{"hard cut without spaces", "abcdefghij", 5, "abcde…"},
		{"collapses internal whitespace", "line one\n\nline two", 200, "line one line two"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := truncateOnWordBoundary(c.in, c.max)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSortByScoreDesc(t *testing.T) {
	rows := []notebookReviewResult{
		{Path: "a", Score: 1.0},
		{Path: "c", Score: 3.0},
		{Path: "b", Score: 3.0},
		{Path: "d", Score: 2.0},
	}
	sortByScoreDesc(rows)
	want := []string{"b", "c", "d", "a"} // tie-break: path asc within score desc
	for i, w := range want {
		if rows[i].Path != w {
			t.Errorf("rows[%d].Path = %q, want %q (full=%v)", i, rows[i].Path, w, rows)
		}
	}
}

// listRegisteredToolsWithSlug is the variable-slug analog of
// listRegisteredTools (which hardcodes "workflow-architect"). PR 4's CEO
// gate cannot be exercised through the existing helper, so we build a
// parallel one here. The pattern mirrors server_backend_switch_test.go.
func listRegisteredToolsWithSlug(t *testing.T, slug, channel string, oneOnOne bool) []string {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "wuphf-team-test", Version: "0.1.0"}, nil)
	configureServerTools(server, slug, channel, oneOnOne)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Wait()

	client := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = clientSession.Close() }()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	return names
}
