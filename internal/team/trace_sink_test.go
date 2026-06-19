package team

import (
	"strings"
	"testing"
)

// TestTraceFromToolUseMasksPII is the security regression: argument bodies that
// carry PII (an email body, a draft message) are redacted, while structural
// config (query, max_results, channel) is kept so the extractor can fill Params.
func TestTraceFromToolUseMasksPII(t *testing.T) {
	input := `{"platform":"gmail","action_id":"GMAIL_SEND_EMAIL","data":{` +
		`"to":"ceo@acme.com","subject":"Digest","body":"SECRET private contents here",` +
		`"query":"is:unread newer_than:1d","max_results":25}}`
	tr, ok := traceFromToolUse("OFFICE-1", "t1", "ceo", "mcp__wuphf-office__team_action_execute", input, 0)
	if !ok {
		t.Fatal("proxy tool_use with action_id must produce a trace")
	}
	if tr.Platform != "gmail" || tr.ActionID != "GMAIL_SEND_EMAIL" {
		t.Fatalf("platform/action_id: %+v", tr)
	}
	data, _ := tr.Args["data"].(map[string]any)
	if data == nil {
		t.Fatalf("args.data missing: %+v", tr.Args)
	}
	if data["body"] != "[redacted]" {
		t.Errorf("body must be redacted, got %v", data["body"])
	}
	if data["query"] != "is:unread newer_than:1d" {
		t.Errorf("structural query must be kept, got %v", data["query"])
	}
	// Belt-and-suspenders: the secret string must not appear anywhere in the
	// serialized trace args.
	if strings.Contains(strings.Join(flattenStrings(tr.Args), " "), "SECRET private") {
		t.Errorf("redacted body leaked into args: %+v", tr.Args)
	}
}

// TestTraceFromToolUseSkipsNonProxy ensures plain harness tools and non-proxy
// MCP tools are not traced (only integration work is).
func TestTraceFromToolUseSkipsNonProxy(t *testing.T) {
	for _, name := range []string{"Bash", "mcp__wuphf-office__team_action_search", "Read"} {
		if _, ok := traceFromToolUse("OFFICE-1", "t1", "ceo", name, `{"x":1}`, 0); ok {
			t.Errorf("non-proxy tool %q must not be traced", name)
		}
	}
}

// TestSummarizeResultBoundsAndPreservesShape verifies a JSON response is reduced
// to a bounded, key-preserving summary (so the extractor can infer
// result_path/expose) and a giant blob is capped.
func TestSummarizeResultBoundsAndPreservesShape(t *testing.T) {
	resp := `{"data":{"messages":[{"sender":"a@b.com","subject":"Hi","snippet":"` +
		strings.Repeat("x", 5000) + `"}]}}`
	got := summarizeResult(resp)
	if len(got) > traceResultCap+8 {
		t.Fatalf("summary not capped: %d chars", len(got))
	}
	for _, key := range []string{"data", "messages", "sender", "subject"} {
		if !strings.Contains(got, key) {
			t.Errorf("summary lost structural key %q: %s", key, got)
		}
	}
}

// TestActionTraceRoundTrip drives persist -> read-by-task through the real sink.
func TestActionTraceRoundTrip(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	path := TraceSinkPath()
	persistActionTrace(ActionTrace{TaskID: "OFFICE-7", Seq: 0, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"})
	persistActionTrace(ActionTrace{TaskID: "OFFICE-7", Seq: 1, Platform: "slack", ActionID: "SLACK_CHAT_POST_MESSAGE"})
	persistActionTrace(ActionTrace{TaskID: "OFFICE-OTHER", Seq: 0, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"})

	got, err := ActionTracesForTask(path, "OFFICE-7")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 traces for OFFICE-7, got %d: %+v", len(got), got)
	}
	if got[0].ActionID != "GMAIL_FETCH_EMAILS" || got[1].ActionID != "SLACK_CHAT_POST_MESSAGE" {
		t.Fatalf("order/content wrong: %+v", got)
	}
}

// flattenStrings collects every string value reachable in a nested args map.
func flattenStrings(v any) []string {
	var out []string
	switch t := v.(type) {
	case string:
		out = append(out, t)
	case map[string]any:
		for _, vv := range t {
			out = append(out, flattenStrings(vv)...)
		}
	case []any:
		for _, vv := range t {
			out = append(out, flattenStrings(vv)...)
		}
	}
	return out
}

// TestActionTracesForTaskMergesChannelAndID is the regression for the split-key
// bug: a task's traces tagged with its channel slug ("task-office-9") and its
// task id ("OFFICE-9") must merge under one canonical identity, or the
// extractor sees only half the trace and the gate wrongly fails.
func TestActionTracesForTaskMergesChannelAndID(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	path := TraceSinkPath()
	persistActionTrace(ActionTrace{TaskID: "OFFICE-9", Seq: 0, Platform: "gmail", ActionID: "GMAIL_FETCH_EMAILS"})
	persistActionTrace(ActionTrace{TaskID: "task-office-9", Seq: 1, Platform: "slack", ActionID: "SLACK_SEND_MESSAGE"})

	for _, key := range []string{"OFFICE-9", "task-office-9"} {
		got, err := ActionTracesForTask(path, key)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if len(got) != 2 {
			t.Fatalf("querying %q must merge both taggings (want 2), got %d", key, len(got))
		}
	}
}

// TestWikiReadFromToolUse pins the wiki-provenance signal: team_wiki_read's
// article_path is captured; other tools (including non-wiki MCP) are skipped.
func TestWikiReadFromToolUse(t *testing.T) {
	if got, ok := wikiReadFromToolUse("mcp__wuphf-office__team_wiki_read", `{"article_path":"playbooks/triage.md"}`); !ok || got != "playbooks/triage.md" {
		t.Fatalf("expected playbooks/triage.md, got %q ok=%v", got, ok)
	}
	if got, ok := wikiReadFromToolUse("team_wiki_read", `{"article_path":"team/x.md"}`); !ok || got != "team/x.md" {
		t.Fatalf("bare name must work, got %q ok=%v", got, ok)
	}
	for _, name := range []string{"team_wiki_search", "Bash", "mcp__wuphf-office__team_action_execute"} {
		if _, ok := wikiReadFromToolUse(name, `{"article_path":"x"}`); ok {
			t.Errorf("non-read tool %q must be skipped", name)
		}
	}
}

// TestWikiContextForTask drives persist -> read-by-task (canonical, deduped).
func TestWikiContextForTask(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	path := WikiContextSinkPath()
	persistWikiRead("OFFICE-5", "playbooks/triage.md")
	persistWikiRead("task-office-5", "team/escalation.md") // channel-slug tagging
	persistWikiRead("OFFICE-5", "playbooks/triage.md")     // dup
	persistWikiRead("OFFICE-OTHER", "noise.md")

	got := WikiContextForTask(path, "OFFICE-5")
	if len(got) != 2 || got[0] != "playbooks/triage.md" || got[1] != "team/escalation.md" {
		t.Fatalf("want [triage, escalation] merged+deduped, got %v", got)
	}
}
