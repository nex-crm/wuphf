package team

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/workflow"
)

// TestIsIntegrationRead: a deterministic step with Platform+ActionID is an
// integration read (runs the generic executor); llm/external or bare
// deterministic steps are not.
func TestIsIntegrationRead(t *testing.T) {
	read := workflow.Action{ID: "fetch", Kind: workflow.ActionDeterministic, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"}
	if !read.IsIntegrationRead() {
		t.Fatal("deterministic + platform + action_id should be an integration read")
	}
	for _, a := range []workflow.Action{
		{ID: "x", Kind: workflow.ActionDeterministic},                                          // no target
		{ID: "x", Kind: workflow.ActionLLM, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"}, // llm, not a read
		{ID: "x", Kind: workflow.ActionExternal, Platform: "slack", ActionID: "SLACK_SEND"},    // a send
	} {
		if a.IsIntegrationRead() {
			t.Fatalf("%+v should not be an integration read", a)
		}
	}
}

// TestProjectResultExtractsAndProjects: projection pulls the array at ResultPath
// and keeps only the exposed fields (so raw provider bodies never flow on).
func TestProjectResultExtractsAndProjects(t *testing.T) {
	raw := json.RawMessage(`{"data":{"messages":[
	  {"sender":"Ada <ada@x.com>","subject":"hi","preview":{"body":"hello"},"labelIds":["UNREAD"],"payload":{"huge":"SECRET-BLOB"}},
	  {"sender":"Bob <bob@x.com>","subject":"yo","preview":{"body":"there"},"labelIds":["IMPORTANT"],"payload":{"huge":"SECRET-BLOB"}}
	]}}`)
	out, count := projectResult(raw, "data.messages", []string{"sender", "subject", "preview.body"})
	if count != 2 {
		t.Fatalf("want 2 projected, got %d", count)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("projected output must be valid JSON: %v", err)
	}
	if rows[0]["sender"] != "Ada <ada@x.com>" || rows[0]["subject"] != "hi" || rows[0]["body"] != "hello" {
		t.Fatalf("projection wrong: %+v", rows[0])
	}
	// The unexposed huge payload (a stand-in for secrets/attachments) is dropped.
	if _, leaked := rows[0]["payload"]; leaked {
		t.Fatalf("unexposed field leaked into projection: %+v", rows[0])
	}
}

// TestProjectResultPassthroughWhenNoArray: if the path doesn't resolve to an
// array, the whole response passes through (the reducer still bounds it).
func TestProjectResultPassthrough(t *testing.T) {
	raw := json.RawMessage(`{"data":{"status":"ok"}}`)
	out, count := projectResult(raw, "data.messages", []string{"sender"})
	if count != 0 || string(out) != string(raw) {
		t.Fatalf("non-array path should pass through unchanged: count=%d out=%s", count, out)
	}
}

func TestWalkPathAndLeafKey(t *testing.T) {
	m := map[string]any{"preview": map[string]any{"body": "hi"}}
	if walkPath(m, "preview.body") != "hi" {
		t.Fatal("walkPath should follow nested maps")
	}
	if walkPath(m, "preview.missing") != nil {
		t.Fatal("walkPath should return nil for a missing segment")
	}
	if leafKey("preview.body") != "body" || leafKey("sender") != "sender" {
		t.Fatal("leafKey should return the last segment")
	}
}

// TestExecIntegrationReadRefusedWhenNotAllowed: the executor refuses a read for a
// target not on the allow-list BEFORE any provider call (D6). With a deny-all
// allow predicate it must fail closed without touching Composio.
func TestExecIntegrationReadRefusedWhenNotAllowed(t *testing.T) {
	b := &Broker{}
	denyAll := func(string, string) bool { return false }
	a := workflow.Action{ID: "fetch", Kind: workflow.ActionDeterministic, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"}
	out := b.execIntegrationAction(nil, a, denyAll)
	if out.OK {
		t.Fatalf("a read not on the allow-list must be refused, got OK: %+v", out)
	}
	if got := out.Err; got == "" {
		t.Fatal("refusal should carry an error explaining the allow-list")
	}
}

// TestOutputKeyForNaming: presentation keying stays sensible without any
// provider-specific execution branch.
func TestOutputKeyForNaming(t *testing.T) {
	cases := map[string]string{
		"summarize_threads":  "summary",
		"compose_digest":     "digest",
		"post_slack_general": "post_slack_general",
	}
	for id, want := range cases {
		if got := outputKeyFor(workflow.Action{ID: id}); got != want {
			t.Errorf("outputKeyFor(%q)=%q want %q", id, got, want)
		}
	}
}

// TestRenderContextSkipsInternalKeys: prompt rendering excludes bookkeeping keys
// (counts/reductions/errors) so they don't pollute the model context.
func TestRenderContextSkipsInternalKeys(t *testing.T) {
	ctx := renderContext(map[string]any{
		"gmail_fetch_emails":           json.RawMessage(`[{"subject":"hi"}]`),
		"gmail_fetch_emails_count":     2,
		"gmail_fetch_emails_reduction": workflow.Reduction{Truncated: true},
		"llm_used":                     true,
	})
	if !strings.Contains(ctx, "gmail_fetch_emails:") {
		t.Fatalf("should render the content key, got: %s", ctx)
	}
	for _, internal := range []string{"_count", "_reduction", "llm_used"} {
		if strings.Contains(ctx, internal) {
			t.Fatalf("internal key %q leaked into prompt context: %s", internal, ctx)
		}
	}
}

// TestRenderActionParamsSubstitutesUpstreamOutput verifies the send body is
// built from the action's params with {{step}} tokens replaced by the real
// upstream output (the llm step's text) — not the raw context map, and not a
// placeholder.
func TestRenderActionParamsSubstitutesUpstreamOutput(t *testing.T) {
	params := map[string]any{"data": map[string]any{
		"channel":       "#general",
		"markdown_text": "{{summarize_urgent_emails}}",
	}}
	data := map[string]any{
		"summarize_urgent_emails":  "⚠️ 2 urgent: Acme renewal, YC intro.",
		"gmail_fetch_emails":       []any{map[string]any{"subject": "Acme renewal"}},
		"gmail_fetch_emails_count": 2, // internal-ish; must not leak into a token
	}
	body := renderActionParams(params, data)
	inner, _ := body["data"].(map[string]any)
	if inner == nil {
		t.Fatalf("rendered body missing data envelope: %+v", body)
	}
	if inner["channel"] != "#general" {
		t.Errorf("channel must pass through, got %v", inner["channel"])
	}
	if inner["markdown_text"] != "⚠️ 2 urgent: Acme renewal, YC intro." {
		t.Fatalf("markdown_text must be the real summary, got %q", inner["markdown_text"])
	}
}

// TestRenderActionParamsEmptyFallsBackToContext keeps legacy contracts working:
// with no authored params, the whole context is the body (old behavior).
func TestRenderActionParamsEmptyFallsBackToContext(t *testing.T) {
	data := map[string]any{"x": 1}
	if got := renderActionParams(nil, data); got["x"] != 1 {
		t.Fatalf("empty params must fall back to context, got %+v", got)
	}
}
