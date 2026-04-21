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
)

func TestEntityToolsRegisteredOnlyInMarkdownBackend(t *testing.T) {
	toolNames := []string{"entity_fact_record", "entity_brief_synthesize"}
	cases := []struct {
		backend  string
		mustHave bool
	}{
		{"markdown", true},
		{"nex", false},
		{"gbrain", false},
		{"none", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.backend, func(t *testing.T) {
			t.Setenv("WUPHF_MEMORY_BACKEND", tc.backend)
			names := listRegisteredTools(t, "general", false)
			for _, tool := range toolNames {
				has := slices.Contains(names, tool)
				if tc.mustHave && !has {
					t.Errorf("backend=%s missing tool %q; got %v", tc.backend, tool, names)
				}
				if !tc.mustHave && has {
					t.Errorf("backend=%s unexpectedly has tool %q", tc.backend, tool)
				}
			}
		})
	}
}

func TestEntityToolsRegisteredInOneOnOne(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	names := listRegisteredTools(t, "dm-ceo", true)
	for _, want := range []string{"entity_fact_record", "entity_brief_synthesize"} {
		if !slices.Contains(names, want) {
			t.Errorf("1:1 mode missing tool %q", want)
		}
	}
}

func TestHandleEntityFactRecord_HappyPath(t *testing.T) {
	var seenBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seenBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"fact_id":           "f-123",
			"fact_count":        3,
			"threshold_crossed": false,
		})
	}))
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleEntityFactRecord(context.Background(), nil, TeamEntityFactRecordArgs{
		EntityKind: "people",
		EntitySlug: "nazz",
		Fact:       "Ex-HubSpot",
		SourcePath: "agents/pm/notebook/x.md",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	if seenBody["entity_slug"] != "nazz" || seenBody["fact"] != "Ex-HubSpot" {
		t.Fatalf("body mismatch: %+v", seenBody)
	}
	if seenBody["recorded_by"] != "pm" {
		t.Errorf("recorded_by=%v", seenBody["recorded_by"])
	}
	if !strings.Contains(toolErrorText(res), "f-123") {
		// The text result is the JSON envelope.
		out := toolErrorText(res)
		if !strings.Contains(out, "f-123") {
			t.Errorf("missing fact_id in output: %s", out)
		}
	}
}

func TestHandleEntityFactRecord_ValidationErrors(t *testing.T) {
	// No broker URL needed — errors fire before any HTTP call.
	cases := []struct {
		name string
		args TeamEntityFactRecordArgs
	}{
		{"missing kind", TeamEntityFactRecordArgs{EntitySlug: "x", Fact: "y"}},
		{"missing slug", TeamEntityFactRecordArgs{EntityKind: "people", Fact: "y"}},
		{"missing fact", TeamEntityFactRecordArgs{EntityKind: "people", EntitySlug: "x"}},
		{"bad source path", TeamEntityFactRecordArgs{EntityKind: "people", EntitySlug: "x", Fact: "y", SourcePath: "../etc/passwd"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WUPHF_AGENT_SLUG", "pm")
			res, _, _ := handleEntityFactRecord(context.Background(), nil, tc.args)
			if !isToolError(res) {
				t.Fatalf("expected tool error; got: %s", toolErrorText(res))
			}
		})
	}
}

func TestHandleEntityBriefSynthesize_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"synthesis_id": 42,
			"queued_at":    "2026-04-20T12:00:00Z",
		})
	}))
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleEntityBriefSynthesize(context.Background(), nil, TeamEntityBriefSynthesizeArgs{
		EntityKind: "companies",
		EntitySlug: "acme",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	if !strings.Contains(toolErrorText(res), "42") {
		t.Errorf("missing synthesis_id: %s", toolErrorText(res))
	}
}

func TestEntityGraphQueryRegistered(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	names := listRegisteredTools(t, "general", false)
	if !slices.Contains(names, "entity_graph_query") {
		t.Fatalf("entity_graph_query missing; got %v", names)
	}
}

func TestHandleEntityGraphQuery_HappyPath(t *testing.T) {
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind":      "people",
			"slug":      "sarah",
			"direction": "out",
			"edges": []map[string]any{
				{
					"from_kind":          "people",
					"from_slug":          "sarah",
					"to_kind":            "companies",
					"to_slug":            "acme",
					"first_seen_fact_id": "abc",
					"last_seen_ts":       "2026-04-21T00:00:00Z",
					"occurrence_count":   1,
				},
			},
		})
	}))
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleEntityGraphQuery(context.Background(), nil, TeamEntityGraphQueryArgs{
		EntityKind: "people",
		EntitySlug: "sarah",
		Direction:  "out",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	if !strings.Contains(seenQuery, "kind=people") || !strings.Contains(seenQuery, "slug=sarah") || !strings.Contains(seenQuery, "direction=out") {
		t.Errorf("unexpected query string: %q", seenQuery)
	}
	out := toolErrorText(res)
	if !strings.Contains(out, "acme") {
		t.Errorf("missing to_slug in output: %s", out)
	}
}

func TestHandleEntityGraphQuery_Validation(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	cases := []TeamEntityGraphQueryArgs{
		{EntitySlug: "x"},      // missing kind
		{EntityKind: "people"}, // missing slug
		{EntityKind: "people", EntitySlug: "x", Direction: "?"}, // bad direction
	}
	for i, args := range cases {
		res, _, _ := handleEntityGraphQuery(context.Background(), nil, args)
		if !isToolError(res) {
			t.Fatalf("case %d expected tool error; got %s", i, toolErrorText(res))
		}
	}
}

func TestHandleEntityBriefSynthesize_Validation(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	cases := []TeamEntityBriefSynthesizeArgs{
		{EntitySlug: "x"},
		{EntityKind: "people"},
	}
	for i, args := range cases {
		res, _, _ := handleEntityBriefSynthesize(context.Background(), nil, args)
		if !isToolError(res) {
			t.Fatalf("case %d expected tool error; got %s", i, toolErrorText(res))
		}
	}
}
