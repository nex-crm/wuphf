package teammcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestNotebookToolsRegisteredOnlyInMarkdownBackend locks in the gate: the 5
// notebook_* tools appear iff WUPHF_MEMORY_BACKEND=markdown. Other backends
// (nex, gbrain, none) must not expose them — matches wiki parity.
func TestNotebookToolsRegisteredOnlyInMarkdownBackend(t *testing.T) {
	notebookTools := []string{
		"notebook_write",
		"notebook_read",
		"notebook_list",
		"notebook_search",
		"notebook_promote",
		"notebook_visual_artifact_create",
		"notebook_visual_artifact_list",
		"notebook_visual_artifact_read",
		"notebook_visual_artifact_promote",
	}
	cases := []struct {
		name     string
		backend  string
		mustHave bool
	}{
		{"markdown registers notebook tools", "markdown", true},
		{"nex excludes notebook tools", "nex", false},
		{"gbrain excludes notebook tools", "gbrain", false},
		{"none excludes notebook tools", "none", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WUPHF_MEMORY_BACKEND", tc.backend)
			names := listRegisteredTools(t, "general", false)
			for _, name := range notebookTools {
				if tc.mustHave && !slices.Contains(names, name) {
					t.Errorf("backend=%s expected %q registered; got %v", tc.backend, name, names)
				}
				if !tc.mustHave && slices.Contains(names, name) {
					t.Errorf("backend=%s expected %q NOT registered; got %v", tc.backend, name, names)
				}
			}
		})
	}
}

// TestNotebookToolsRegisteredInOneOnOne confirms the DM/1:1 path also
// registers the notebook tools when markdown is the backend.
func TestNotebookToolsRegisteredInOneOnOne(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	names := listRegisteredTools(t, "dm-ceo", true)
	for _, want := range []string{"notebook_write", "notebook_read", "notebook_list", "notebook_search", "notebook_promote", "notebook_visual_artifact_create", "notebook_visual_artifact_promote"} {
		if !slices.Contains(names, want) {
			t.Errorf("expected %q registered in 1:1; got %v", want, names)
		}
	}
}

func TestNotebookVisualArtifactToolsTeachHTMLGuidance(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	tools := listRegisteredToolMap(t, "general", false)
	tool := tools["notebook_visual_artifact_create"]
	if tool == nil {
		t.Fatalf("notebook_visual_artifact_create was not registered; tools=%v", tools)
	}
	for _, want := range []string{"after notebook_write", "self-contained", "inline CSS/JS", "no network fetches", "interactive tuning surfaces", "technical-manual artifact style", "old mathematics/physics book", "rgb(19, 66, 255)", "FIG_001", "visual-artifact:ra_0123456789abcdef"} {
		if !strings.Contains(tool.Description, want) {
			t.Fatalf("description missing %q:\n%s", want, tool.Description)
		}
	}
}

// stubBroker is an httptest server that records the last POST body and
// returns canned responses for each notebook endpoint.
func stubBroker(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *testingAuth) {
	t.Helper()
	auth := &testingAuth{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.lastAuth = r.Header.Get("Authorization")
		auth.lastPath = r.URL.Path
		auth.lastRaw = r.URL.RawQuery
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			auth.lastBody = string(body)
		}
		handler(w, r)
	}))
	return srv, auth
}

type testingAuth struct {
	lastAuth string
	lastPath string
	lastRaw  string
	lastBody string
}

func withBrokerURL(t *testing.T, url string) {
	t.Helper()
	t.Setenv("WUPHF_TEAM_BROKER_URL", url)
	t.Setenv("WUPHF_BROKER_TOKEN", "test-token")
	t.Setenv("WUPHF_BROKER_TOKEN_FILE", "/dev/null")
}

func TestHandleTeamNotebookWrite(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path":          "agents/pm/notebook/x.md",
			"commit_sha":    "abc1234",
			"bytes_written": 17,
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamNotebookWrite(context.Background(), nil, TeamNotebookWriteArgs{
		ArticlePath: "agents/pm/notebook/x.md",
		TaskID:      "task-1",
		Mode:        "create",
		Content:     "# x\nbody\n",
		CommitMsg:   "draft x",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("expected success, got tool error: %s", toolErrorText(res))
	}
	if !strings.Contains(auth.lastPath, "/notebook/write") {
		t.Fatalf("broker hit wrong path: %s", auth.lastPath)
	}
	if !strings.Contains(auth.lastBody, "\"slug\":\"pm\"") {
		t.Fatalf("expected slug=pm in body, got %s", auth.lastBody)
	}
	if !strings.Contains(auth.lastBody, "\"task_id\":\"task-1\"") {
		t.Fatalf("expected task_id in body, got %s", auth.lastBody)
	}
	if !strings.Contains(auth.lastAuth, "Bearer test-token") {
		t.Fatalf("expected auth header, got %q", auth.lastAuth)
	}
}

func TestHandleTeamNotebookWriteSlugMismatchLocal(t *testing.T) {
	// No broker hit — client-side rejection.
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	res, _, err := handleTeamNotebookWrite(context.Background(), nil, TeamNotebookWriteArgs{
		ArticlePath: "agents/ceo/notebook/x.md",
		Mode:        "create",
		Content:     "# x\n",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !isToolError(res) {
		t.Fatal("expected tool error for slug-mismatch")
	}
	if !strings.Contains(toolErrorText(res), "notebook_path_not_author_owned") {
		t.Fatalf("expected explicit error code, got %s", toolErrorText(res))
	}
}

func TestHandleTeamNotebookWriteValidations(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	cases := []struct {
		name string
		args TeamNotebookWriteArgs
	}{
		{"empty path", TeamNotebookWriteArgs{Mode: "create", Content: "x"}},
		{"bogus mode", TeamNotebookWriteArgs{ArticlePath: "agents/pm/notebook/x.md", Mode: "banana", Content: "x"}},
		{"empty content", TeamNotebookWriteArgs{ArticlePath: "agents/pm/notebook/x.md", Mode: "create", Content: "  "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := handleTeamNotebookWrite(context.Background(), nil, tc.args)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if !isToolError(res) {
				t.Fatalf("expected tool error for %s", tc.name)
			}
		})
	}
}

func TestHandleTeamNotebookWriteMissingSlug(t *testing.T) {
	// Explicitly clear the env slug so resolveSlug fails.
	t.Setenv("WUPHF_AGENT_SLUG", "")
	t.Setenv("NEX_AGENT_SLUG", "")
	res, _, _ := handleTeamNotebookWrite(context.Background(), nil, TeamNotebookWriteArgs{
		ArticlePath: "agents/pm/notebook/x.md",
		Mode:        "create",
		Content:     "x",
	})
	if !isToolError(res) {
		t.Fatal("expected tool error for missing slug")
	}
}

func TestHandleTeamNotebookRead(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("# Retro\n\nbody\n"))
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamNotebookRead(context.Background(), nil, TeamNotebookReadArgs{
		ArticlePath: "agents/ceo/notebook/x.md",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("unexpected tool error: %s", toolErrorText(res))
	}
	if !strings.Contains(auth.lastPath, "/notebook/read") {
		t.Fatalf("wrong path: %s", auth.lastPath)
	}
	// Cross-agent read: request should carry path but slug optional / empty.
	if !strings.Contains(auth.lastRaw, "path=agents") {
		t.Fatalf("expected path query param, got %q", auth.lastRaw)
	}
}

func TestHandleTeamNotebookReadMissingPath(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	res, _, _ := handleTeamNotebookRead(context.Background(), nil, TeamNotebookReadArgs{})
	if !isToolError(res) {
		t.Fatal("expected tool error for missing path")
	}
}

func TestHandleTeamNotebookListDefaultsToCaller(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{}})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	_, _, err := handleTeamNotebookList(context.Background(), nil, TeamNotebookListArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(auth.lastRaw, "slug=pm") {
		t.Fatalf("expected slug=pm default, got %q", auth.lastRaw)
	}
}

func TestHandleTeamNotebookListCrossAgent(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{"path": "agents/ceo/notebook/x.md", "title": "x"},
			},
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	_, _, err := handleTeamNotebookList(context.Background(), nil, TeamNotebookListArgs{TargetSlug: "ceo"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(auth.lastRaw, "slug=ceo") {
		t.Fatalf("expected slug=ceo target, got %q", auth.lastRaw)
	}
}

func TestHandleTeamNotebookListNoSlugAnywhere(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "")
	t.Setenv("NEX_AGENT_SLUG", "")
	res, _, _ := handleTeamNotebookList(context.Background(), nil, TeamNotebookListArgs{})
	if !isToolError(res) {
		t.Fatal("expected tool error when no slug available")
	}
}

func TestHandleTeamNotebookSearch(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": []map[string]any{
				{"path": "agents/pm/notebook/x.md", "line": 3, "snippet": "Retro"},
			},
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	_, _, err := handleTeamNotebookSearch(context.Background(), nil, TeamNotebookSearchArgs{
		TargetSlug: "pm",
		Pattern:    "Retro",
		TaskID:     "task-1",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(auth.lastRaw, "slug=pm") || !strings.Contains(auth.lastRaw, "q=Retro") {
		t.Fatalf("expected slug+q query params, got %q", auth.lastRaw)
	}
	if !strings.Contains(auth.lastRaw, "task_id=task-1") || !strings.Contains(auth.lastRaw, "actor=pm") {
		t.Fatalf("expected task workflow query params, got %q", auth.lastRaw)
	}
}

func TestHandleTeamNotebookSearchRequiresArgs(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	cases := []struct {
		name string
		args TeamNotebookSearchArgs
	}{
		{"missing target", TeamNotebookSearchArgs{Pattern: "x"}},
		{"missing pattern", TeamNotebookSearchArgs{TargetSlug: "pm"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, _ := handleTeamNotebookSearch(context.Background(), nil, tc.args)
			if !isToolError(res) {
				t.Fatalf("expected tool error for %s", tc.name)
			}
		})
	}
}

// TestHandleTeamNotebookReadSpecialCharsInPattern confirms URL-encoding on
// the search path so special characters do not corrupt the broker URL.
func TestHandleTeamNotebookSearchEncoding(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": []map[string]any{}})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	_, _, _ = handleTeamNotebookSearch(context.Background(), nil, TeamNotebookSearchArgs{
		TargetSlug: "pm",
		Pattern:    "$(whoami)&=?",
	})
	if strings.Contains(auth.lastRaw, "$(whoami)") {
		t.Fatalf("pattern not URL-encoded: %q", auth.lastRaw)
	}
}

func TestHandleTeamNotebookPromote_Happy(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"promotion_id":  "rvw-123-0001",
			"reviewer_slug": "ceo",
			"state":         "pending",
			"human_only":    false,
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamNotebookPromote(context.Background(), nil, TeamNotebookPromoteArgs{
		SourcePath:     "agents/pm/notebook/retro.md",
		TaskID:         "task-1",
		TargetWikiPath: "team/playbooks/retro.md",
		Rationale:      "canonical",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	if !strings.Contains(auth.lastPath, "/notebook/promote") {
		t.Fatalf("broker hit wrong path: %s", auth.lastPath)
	}
	if !strings.Contains(auth.lastBody, "\"my_slug\":\"pm\"") {
		t.Fatalf("expected my_slug in body: %s", auth.lastBody)
	}
	if !strings.Contains(auth.lastBody, "\"task_id\":\"task-1\"") {
		t.Fatalf("expected task_id in body: %s", auth.lastBody)
	}
	if !strings.Contains(auth.lastBody, "\"target_wiki_path\":\"team/playbooks/retro.md\"") {
		t.Fatalf("expected target path in body: %s", auth.lastBody)
	}
}

func TestHandleTeamNotebookPromote_Validations(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	cases := []struct {
		name string
		args TeamNotebookPromoteArgs
	}{
		{"missing source", TeamNotebookPromoteArgs{TargetWikiPath: "team/x.md", Rationale: "r"}},
		{"source not under caller", TeamNotebookPromoteArgs{
			SourcePath: "agents/other/notebook/x.md", TargetWikiPath: "team/x.md", Rationale: "r",
		}},
		{"source no .md", TeamNotebookPromoteArgs{
			SourcePath: "agents/pm/notebook/x", TargetWikiPath: "team/x.md", Rationale: "r",
		}},
		{"target not team/", TeamNotebookPromoteArgs{
			SourcePath: "agents/pm/notebook/x.md", TargetWikiPath: "wrong/x.md", Rationale: "r",
		}},
		{"target no .md", TeamNotebookPromoteArgs{
			SourcePath: "agents/pm/notebook/x.md", TargetWikiPath: "team/x", Rationale: "r",
		}},
		{"missing rationale", TeamNotebookPromoteArgs{
			SourcePath: "agents/pm/notebook/x.md", TargetWikiPath: "team/x.md",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, _ := handleTeamNotebookPromote(context.Background(), nil, tc.args)
			if !isToolError(res) {
				t.Fatalf("expected tool error for %s", tc.name)
			}
		})
	}
}

func TestHandleTeamNotebookVisualArtifactCreate(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"artifact": map[string]any{
				"id":                 "ra_0123456789abcdef",
				"title":              "Visual plan",
				"sourceMarkdownPath": "agents/pm/notebook/plan.md",
			},
			"commit_sha":    "abc1234",
			"bytes_written": 512,
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamNotebookVisualArtifactCreate(context.Background(), nil, TeamNotebookVisualArtifactCreateArgs{
		SourcePath: "agents/pm/notebook/plan.md",
		TaskID:     "task-1",
		Title:      "Visual plan",
		Summary:    "Compare the options.",
		HTML:       "<!doctype html><html><body><h1>Plan</h1></body></html>",
		CommitMsg:  "artifact: create visual plan",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	if auth.lastPath != "/notebook/visual-artifacts" {
		t.Fatalf("wrong path: %s", auth.lastPath)
	}
	for _, want := range []string{
		`"slug":"pm"`,
		`"source_markdown_path":"agents/pm/notebook/plan.md"`,
		`"related_task_id":"task-1"`,
		`"title":"Visual plan"`,
	} {
		if !strings.Contains(auth.lastBody, want) {
			t.Fatalf("request body missing %s: %s", want, auth.lastBody)
		}
	}
}

func TestHandleTeamNotebookVisualArtifactListReadPromote(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/notebook/visual-artifacts":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifacts": []map[string]any{{"id": "ra_0123456789abcdef"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/notebook/visual-artifacts/ra_0123456789abcdef":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifact": map[string]any{"id": "ra_0123456789abcdef"},
				"html":     "<!doctype html><html><body>ok</body></html>",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/notebook/visual-artifacts/ra_0123456789abcdef/promote":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifact":   map[string]any{"id": "ra_0123456789abcdef", "trustLevel": "promoted"},
				"commit_sha": "def5678",
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamNotebookVisualArtifactList(context.Background(), nil, TeamNotebookVisualArtifactListArgs{
		SourcePath: "agents/pm/notebook/plan.md",
	})
	if err != nil {
		t.Fatalf("list handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("list tool error: %s", toolErrorText(res))
	}
	if auth.lastPath != "/notebook/visual-artifacts" || !strings.Contains(auth.lastRaw, "source_path=agents") {
		t.Fatalf("unexpected list request path=%s raw=%s", auth.lastPath, auth.lastRaw)
	}

	res, _, err = handleTeamNotebookVisualArtifactRead(context.Background(), nil, TeamNotebookVisualArtifactReadArgs{
		ArtifactID: "ra_0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("read handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("read tool error: %s", toolErrorText(res))
	}

	res, _, err = handleTeamNotebookVisualArtifactPromote(context.Background(), nil, TeamNotebookVisualArtifactPromoteArgs{
		ArtifactID:      "ra_0123456789abcdef",
		TargetWikiPath:  "team/plans/visual-plan.md",
		MarkdownSummary: "# Visual plan\n\nSummary.\n",
		Mode:            "create",
		CommitMsg:       "artifact: promote visual plan",
	})
	if err != nil {
		t.Fatalf("promote handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("promote tool error: %s", toolErrorText(res))
	}
	for _, want := range []string{
		`"actor_slug":"pm"`,
		`"target_wiki_path":"team/plans/visual-plan.md"`,
		`"markdown_summary":"# Visual plan\n\nSummary."`,
		`"mode":"create"`,
	} {
		if !strings.Contains(auth.lastBody, want) {
			t.Fatalf("promote request body missing %s: %s", want, auth.lastBody)
		}
	}
}

func TestHandleTeamNotebookVisualArtifactValidations(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	createCases := []TeamNotebookVisualArtifactCreateArgs{
		{Title: "x", HTML: "<html></html>"},
		{SourcePath: "agents/other/notebook/x.md", Title: "x", HTML: "<html></html>"},
		{SourcePath: "agents/pm/notebook/x.md", HTML: "<html></html>"},
		{SourcePath: "agents/pm/notebook/x.md", Title: "x"},
	}
	for i, args := range createCases {
		res, _, _ := handleTeamNotebookVisualArtifactCreate(context.Background(), nil, args)
		if !isToolError(res) {
			t.Fatalf("create case %d expected tool error", i)
		}
	}
	promoteCases := []TeamNotebookVisualArtifactPromoteArgs{
		{TargetWikiPath: "team/x.md", MarkdownSummary: "x"},
		{ArtifactID: "bad", TargetWikiPath: "team/x.md", MarkdownSummary: "x"},
		{ArtifactID: "ra_0123456789abcdef", TargetWikiPath: "wrong/x.md", MarkdownSummary: "x"},
		{ArtifactID: "ra_0123456789abcdef", TargetWikiPath: "team/x", MarkdownSummary: "x"},
		{ArtifactID: "ra_0123456789abcdef", TargetWikiPath: "team/x.md"},
		{ArtifactID: "ra_0123456789abcdef", TargetWikiPath: "team/x.md", MarkdownSummary: "x", Mode: "bad"},
	}
	for i, args := range promoteCases {
		res, _, _ := handleTeamNotebookVisualArtifactPromote(context.Background(), nil, args)
		if !isToolError(res) {
			t.Fatalf("promote case %d expected tool error", i)
		}
	}
}

func TestHandleTeamNotebookVisualArtifactListRequiresScope(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "")
	t.Setenv("NEX_AGENT_SLUG", "")

	res, _, err := handleTeamNotebookVisualArtifactList(context.Background(), nil, TeamNotebookVisualArtifactListArgs{})
	if err != nil {
		t.Fatalf("list handler returned transport error: %v", err)
	}
	if !isToolError(res) || !strings.Contains(toolErrorText(res), "target_slug is required") {
		t.Fatalf("expected target_slug tool error, got %#v text=%q", res, toolErrorText(res))
	}
}

// Helpers

func isToolError(res *mcp.CallToolResult) bool {
	if res == nil {
		return false
	}
	return res.IsError
}

func toolErrorText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
