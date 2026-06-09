package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// ── Pure set-computation helper ─────────────────────────────────────────────

func TestComputeWikiRefSet(t *testing.T) {
	cases := []struct {
		name      string
		current   []string
		requested []string
		action    string
		want      []string
	}{
		{
			name:      "link adds to existing set",
			current:   []string{"team/a.md"},
			requested: []string{"team/b.md"},
			action:    "link",
			want:      []string{"team/a.md", "team/b.md"},
		},
		{
			name:      "link dedups already-linked path",
			current:   []string{"team/a.md", "team/b.md"},
			requested: []string{"team/b.md", "team/c.md", "team/b.md"},
			action:    "link",
			want:      []string{"team/a.md", "team/b.md", "team/c.md"},
		},
		{
			name:      "link normalizes whitespace and slashes before dedup",
			current:   []string{"team/a.md"},
			requested: []string{"  team/a.md  ", "team/d.md"},
			action:    "link",
			want:      []string{"team/a.md", "team/d.md"},
		},
		{
			name:      "replace sets exactly the requested set",
			current:   []string{"team/a.md", "team/b.md"},
			requested: []string{"team/c.md", "team/c.md", "team/d.md"},
			action:    "replace",
			want:      []string{"team/c.md", "team/d.md"},
		},
		{
			name:      "unlink removes the requested path",
			current:   []string{"team/a.md", "team/b.md", "team/c.md"},
			requested: []string{"team/b.md"},
			action:    "unlink",
			want:      []string{"team/a.md", "team/c.md"},
		},
		{
			name:      "unlink of an unlinked path is a no-op",
			current:   []string{"team/a.md"},
			requested: []string{"team/z.md"},
			action:    "unlink",
			want:      []string{"team/a.md"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeWikiRefSet(tc.current, tc.requested, tc.action)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("computeWikiRefSet(%v, %v, %q) = %v; want %v",
					tc.current, tc.requested, tc.action, got, tc.want)
			}
		})
	}
}

func TestWikiArticlePathStructuralError(t *testing.T) {
	bad := []string{
		"",
		"/etc/passwd",
		"team/../secrets.md",
		"notes/x.md", // not under team/
		"team/x.txt", // not .md
	}
	for _, p := range bad {
		if err := wikiArticlePathStructuralError(p); err == nil {
			t.Errorf("expected structural error for %q, got nil", p)
		}
	}
	if err := wikiArticlePathStructuralError("team/playbooks/onboarding.md"); err != nil {
		t.Errorf("expected nil for a well-formed path, got %v", err)
	}
}

// ── Registration gate ───────────────────────────────────────────────────────

// link_task_wiki rides with the wiki-curation roles (CEO + Librarian) and must
// NOT be exposed to ordinary specialist agents.
func TestLinkTaskWikiRegisteredForCuratorsOnly(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	cases := []struct {
		slug     string
		mustHave bool
	}{
		{"ceo", true},
		{"librarian", true},
		{"pm", false},
		{"engineer", false},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			names := listRegisteredToolsWithSlug(t, tc.slug, "general", false)
			has := slices.Contains(names, "link_task_wiki")
			if tc.mustHave && !has {
				t.Errorf("slug=%s: expected link_task_wiki registered; got %v", tc.slug, names)
			}
			if !tc.mustHave && has {
				t.Errorf("slug=%s: did not expect link_task_wiki registered; got %v", tc.slug, names)
			}
		})
	}
}

// ── Integration against a fake broker ───────────────────────────────────────

// fakeWikiLinkBroker stands up an httptest server that:
//   - serves GET /wiki/read with 200 for known-good paths and 400/404 otherwise,
//   - serves GET /tasks/{id} returning a fixed current wiki_refs set,
//   - serves POST /tasks echoing back the wiki_refs it was sent and recording
//     the last POST body.
type fakeWikiLinkBroker struct {
	srv         *httptest.Server
	currentRefs []string
	validPaths  map[string]bool
	lastPost    map[string]any
}

func newFakeWikiLinkBroker(t *testing.T, currentRefs []string, validPaths []string) *fakeWikiLinkBroker {
	t.Helper()
	f := &fakeWikiLinkBroker{
		currentRefs: currentRefs,
		validPaths:  map[string]bool{},
	}
	for _, p := range validPaths {
		f.validPaths[p] = true
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/wiki/read":
			path := r.URL.Query().Get("path")
			if !strings.HasPrefix(path, "team/") || !strings.HasSuffix(strings.ToLower(path), ".md") {
				http.Error(w, `{"error":"invalid article path"}`, http.StatusBadRequest)
				return
			}
			if !f.validPaths[path] {
				http.Error(w, `{"error":"article not found"}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# article\nbody\n"))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"taskId": strings.TrimPrefix(r.URL.Path, "/tasks/"),
				"task": map[string]any{
					"id":        strings.TrimPrefix(r.URL.Path, "/tasks/"),
					"wiki_refs": f.currentRefs,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/tasks":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.lastPost = body
			refs := []string{}
			if raw, ok := body["wiki_refs"].([]any); ok {
				for _, v := range raw {
					if s, ok := v.(string); ok {
						refs = append(refs, s)
					}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"id":        body["id"],
					"wiki_refs": refs,
				},
			})
		default:
			http.Error(w, `{"error":"unexpected path"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// postedRefs returns the wiki_refs the tool sent on the last POST /tasks call.
func (f *fakeWikiLinkBroker) postedRefs(t *testing.T) []string {
	t.Helper()
	raw, ok := f.lastPost["wiki_refs"].([]any)
	if !ok {
		t.Fatalf("last POST body has no wiki_refs slice: %#v", f.lastPost)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func TestHandleTeamWikiLink_LinkAddsAndDedups(t *testing.T) {
	broker := newFakeWikiLinkBroker(t,
		[]string{"team/a.md"},
		[]string{"team/a.md", "team/b.md"},
	)
	withBrokerURL(t, broker.srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	// link b.md plus a duplicate a.md → result keeps a, adds b, no dupes.
	res, _, err := handleTeamWikiLink(context.Background(), nil, TeamWikiLinkArgs{
		TaskID: "ISS-1",
		Action: "link",
		Paths:  []string{"team/b.md", "team/a.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("expected success, got: %s", toolErrorText(res))
	}
	got := broker.postedRefs(t)
	want := []string{"team/a.md", "team/b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("posted wiki_refs = %v; want %v", got, want)
	}
	if broker.lastPost["action"] != "comment" {
		t.Fatalf("expected mutation action=comment, got %v", broker.lastPost["action"])
	}
	if broker.lastPost["id"] != "ISS-1" {
		t.Fatalf("expected id=ISS-1, got %v", broker.lastPost["id"])
	}
}

func TestHandleTeamWikiLink_ReplaceSetsExactSet(t *testing.T) {
	broker := newFakeWikiLinkBroker(t,
		[]string{"team/a.md", "team/b.md"},
		[]string{"team/c.md", "team/d.md"},
	)
	withBrokerURL(t, broker.srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	res, _, err := handleTeamWikiLink(context.Background(), nil, TeamWikiLinkArgs{
		TaskID: "ISS-2",
		Action: "replace",
		Paths:  []string{"team/c.md", "team/d.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("expected success, got: %s", toolErrorText(res))
	}
	got := broker.postedRefs(t)
	want := []string{"team/c.md", "team/d.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replace posted wiki_refs = %v; want %v", got, want)
	}
}

func TestHandleTeamWikiLink_UnlinkRemoves(t *testing.T) {
	broker := newFakeWikiLinkBroker(t,
		[]string{"team/a.md", "team/b.md", "team/c.md"},
		nil, // unlink never validates, so no valid paths needed
	)
	withBrokerURL(t, broker.srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	res, _, err := handleTeamWikiLink(context.Background(), nil, TeamWikiLinkArgs{
		TaskID: "ISS-3",
		Action: "unlink",
		Paths:  []string{"team/b.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("expected success, got: %s", toolErrorText(res))
	}
	got := broker.postedRefs(t)
	want := []string{"team/a.md", "team/c.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unlink posted wiki_refs = %v; want %v", got, want)
	}
}

func TestHandleTeamWikiLink_InvalidPathRejected(t *testing.T) {
	// Path is well-formed structurally but does not resolve to a real article:
	// the broker /wiki/read returns 404, so the tool must reject it and NOT
	// post any mutation.
	broker := newFakeWikiLinkBroker(t,
		[]string{"team/a.md"},
		[]string{"team/a.md"}, // team/missing.md is intentionally NOT valid
	)
	withBrokerURL(t, broker.srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	res, _, err := handleTeamWikiLink(context.Background(), nil, TeamWikiLinkArgs{
		TaskID: "ISS-4",
		Action: "link",
		Paths:  []string{"team/missing.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !isToolError(res) {
		t.Fatalf("expected a tool error for a non-existent article, got success")
	}
	if !strings.Contains(toolErrorText(res), "team/missing.md") {
		t.Fatalf("error should name the rejected path; got %q", toolErrorText(res))
	}
	if broker.lastPost != nil {
		t.Fatalf("no mutation should be posted when a path is rejected; got %#v", broker.lastPost)
	}
}

func TestHandleTeamWikiLink_StructurallyBadPathRejectedBeforeNetwork(t *testing.T) {
	broker := newFakeWikiLinkBroker(t, nil, nil)
	withBrokerURL(t, broker.srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	res, _, err := handleTeamWikiLink(context.Background(), nil, TeamWikiLinkArgs{
		TaskID: "ISS-5",
		Action: "link",
		Paths:  []string{"team/../escape.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !isToolError(res) {
		t.Fatalf("expected a tool error for a traversal path, got success")
	}
	if broker.lastPost != nil {
		t.Fatalf("no mutation should be posted for a malformed path; got %#v", broker.lastPost)
	}
}

func TestHandleTeamWikiLink_EmptyResultRejected(t *testing.T) {
	// Unlinking the only ref would empty the set, which the MutateTask apply
	// path cannot store. The tool must refuse honestly rather than report an
	// empty set the broker did not apply.
	broker := newFakeWikiLinkBroker(t,
		[]string{"team/a.md"},
		nil,
	)
	withBrokerURL(t, broker.srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	res, _, err := handleTeamWikiLink(context.Background(), nil, TeamWikiLinkArgs{
		TaskID: "ISS-6",
		Action: "unlink",
		Paths:  []string{"team/a.md"},
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !isToolError(res) {
		t.Fatalf("expected a tool error when the set would be emptied, got success")
	}
	if broker.lastPost != nil {
		t.Fatalf("no mutation should be posted when the result would be empty; got %#v", broker.lastPost)
	}
}

func TestHandleTeamWikiLink_ValidationErrors(t *testing.T) {
	broker := newFakeWikiLinkBroker(t, nil, nil)
	withBrokerURL(t, broker.srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	cases := []struct {
		name string
		args TeamWikiLinkArgs
	}{
		{"missing task_id", TeamWikiLinkArgs{Action: "link", Paths: []string{"team/a.md"}}},
		{"bad action", TeamWikiLinkArgs{TaskID: "ISS-7", Action: "frobnicate", Paths: []string{"team/a.md"}}},
		{"empty paths", TeamWikiLinkArgs{TaskID: "ISS-7", Action: "link", Paths: []string{"  "}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, err := handleTeamWikiLink(context.Background(), nil, tc.args)
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if !isToolError(res) {
				t.Fatalf("expected a tool error, got success")
			}
		})
	}
}
