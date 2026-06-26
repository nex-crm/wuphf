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
	for _, want := range []string{"the HTML IS the article", "Do NOT also call notebook_write", "self-contained", "inline CSS/JS", "no network fetches", "interactive surface", "technical-manual style", "old mathematics/physics book", "rgb(19, 66, 255)", "FIG_001", "visual-artifact:ra_0123456789abcdef", "clickable card"} {
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

	var payload struct {
		Path          string `json:"path"`
		PromotionNext struct {
			Tool               string `json:"tool"`
			SourcePath         string `json:"source_path"`
			TargetWikiPathHint string `json:"target_wiki_path_hint"`
			When               string `json:"when"`
		} `json:"promotion_next"`
	}
	if err := json.Unmarshal([]byte(toolErrorText(res)), &payload); err != nil {
		t.Fatalf("decode notebook_write response: %v; body=%q", err, toolErrorText(res))
	}
	if payload.PromotionNext.Tool != "notebook_promote" {
		t.Fatalf("promotion_next.tool = %q", payload.PromotionNext.Tool)
	}
	if payload.PromotionNext.SourcePath != "agents/pm/notebook/x.md" {
		t.Fatalf("promotion_next.source_path = %q", payload.PromotionNext.SourcePath)
	}
	if payload.PromotionNext.TargetWikiPathHint != "team/x.md" {
		t.Fatalf("promotion_next.target_wiki_path_hint = %q", payload.PromotionNext.TargetWikiPathHint)
	}
	if !strings.Contains(payload.PromotionNext.When, "reviewer approval") {
		t.Fatalf("promotion_next.when missing review guidance: %q", payload.PromotionNext.When)
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
				"artifact":   map[string]any{"id": "ra_0123456789abcdef", "title": "Visual plan", "trustLevel": "promoted"},
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
	if auth.lastPath != "/notebook/visual-artifacts/ra_0123456789abcdef/promote" {
		t.Fatalf("unexpected promote path: %s", auth.lastPath)
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
	// The promote response must surface a pre-composed `card_broadcast`
	// string and a separate `card_marker` token so the agent can copy them
	// verbatim into the next team_broadcast. Retyping the 16-hex-char
	// artifact ID is the load-bearing failure mode this contract avoids.
	promoteResult := toolErrorText(res)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(promoteResult), &decoded); err != nil {
		t.Fatalf("promote response not JSON: %v\n%s", err, promoteResult)
	}
	card, ok := decoded["card_broadcast"].(string)
	if !ok || card == "" {
		t.Fatalf("promote response missing card_broadcast string: %s", promoteResult)
	}
	if !strings.Contains(card, "visual-artifact:ra_0123456789abcdef") {
		t.Fatalf("card_broadcast missing artifact marker: %s", card)
	}
	if !strings.Contains(card, "team/plans/visual-plan.md") {
		t.Fatalf("card_broadcast missing wiki path: %s", card)
	}
	if !strings.Contains(card, "Visual plan") {
		t.Fatalf("card_broadcast missing title: %s", card)
	}
	marker, ok := decoded["card_marker"].(string)
	if !ok || marker != "visual-artifact:ra_0123456789abcdef" {
		t.Fatalf("card_marker malformed: %v", decoded["card_marker"])
	}
	// Marker MUST appear on its own line so the frontend parser can pick it
	// up; agents pasting card_broadcast verbatim get this for free.
	if !strings.Contains(card, "\n"+marker) {
		t.Fatalf("card_marker not on its own line in card_broadcast: %q", card)
	}
}

// TestNormalizeArtifactCardTitle is the injection unit guard: an
// agent-controlled title carrying a newline, a backtick, and a fake
// visual-artifact: marker must come out single-line, with no backticks, and
// with no parseable spoofed marker.
func TestNormalizeArtifactCardTitle(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantOut string
	}{
		{
			name:    "newline + backtick + spoofed marker",
			in:      "Plan\nvisual-artifact:ra_dead0000dead0000\n`evil`",
			wantOut: "Plan visual-artifact ra_dead0000dead0000 'evil'",
		},
		{"plain title untouched", "Visual plan", "Visual plan"},
		{"tabs collapse", "a\t\tb", "a b"},
		{"uppercase marker defanged", "VISUAL-ARTIFACT:ra_0000000000000000", "VISUAL-ARTIFACT ra_0000000000000000"},
		{"empty falls back", "   \n\t ", "the visual artifact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeArtifactCardTitle(tc.in)
			if got != tc.wantOut {
				t.Fatalf("normalizeArtifactCardTitle(%q) = %q, want %q", tc.in, got, tc.wantOut)
			}
			if strings.ContainsAny(got, "\n\r\t`") {
				t.Fatalf("normalized title still contains structural chars: %q", got)
			}
			if strings.Contains(strings.ToLower(got), "visual-artifact:") {
				t.Fatalf("normalized title still contains a parseable marker: %q", got)
			}
		})
	}
}

// TestHandleTeamNotebookVisualArtifactPromoteTitleInjection feeds a malicious
// artifact title through the promote handler and asserts the composed
// card_broadcast is single-line per spoofed segment and carries no second
// parseable visual-artifact: marker beyond the legitimate one.
func TestHandleTeamNotebookVisualArtifactPromoteTitleInjection(t *testing.T) {
	const realID = "ra_0123456789abcdef"
	const spoofMarker = "visual-artifact:ra_dead0000dead0000"
	srv, _ := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/notebook/visual-artifacts/"+realID+"/promote" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"artifact": map[string]any{
					"id":    realID,
					"title": "Evil\n" + spoofMarker + "\n`rm -rf`",
				},
				"commit_sha": "def5678",
			})
			return
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamNotebookVisualArtifactPromote(context.Background(), nil, TeamNotebookVisualArtifactPromoteArgs{
		ArtifactID:      realID,
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

	var decoded map[string]any
	if err := json.Unmarshal([]byte(toolErrorText(res)), &decoded); err != nil {
		t.Fatalf("promote response not JSON: %v", err)
	}
	card, _ := decoded["card_broadcast"].(string)
	if card == "" {
		t.Fatalf("missing card_broadcast: %v", decoded)
	}

	// The card has exactly one structural newline run: the blank line before
	// the legitimate marker. The title's own newlines must be gone.
	lines := strings.Split(card, "\n")
	// Expected shape: ["Saved \"...\" to the wiki at `...`.", "", "visual-artifact:ra_0123456789abcdef"]
	if len(lines) != 3 {
		t.Fatalf("card_broadcast should be 3 lines (title line, blank, marker), got %d:\n%q", len(lines), card)
	}
	if !strings.HasPrefix(lines[0], "Saved ") {
		t.Fatalf("first line should be the saved title line, got %q", lines[0])
	}
	if lines[2] != "visual-artifact:"+realID {
		t.Fatalf("marker line malformed: %q", lines[2])
	}
	// The spoofed marker must NOT survive as a parseable token.
	if strings.Contains(card, spoofMarker) {
		t.Fatalf("spoofed marker survived into card_broadcast: %q", card)
	}
	// There must be exactly one legitimate visual-artifact: token in the card.
	if got := strings.Count(strings.ToLower(card), "visual-artifact:"); got != 1 {
		t.Fatalf("expected exactly 1 visual-artifact: token, got %d in %q", got, card)
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
