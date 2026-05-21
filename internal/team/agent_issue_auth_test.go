package team

import (
	"strings"
	"testing"
)

// TestReportAgentIssueRoutesAuthErrorThroughSystemCard verifies that
// "Not logged in" failures from the agent loop are routed through the
// system-authored SystemErrorCard path instead of being posted as an
// agent_issue bubble. Issue #933 — runtime/auth failures must look
// different from in-character CEO output.
func TestReportAgentIssueRoutesAuthErrorThroughSystemCard(t *testing.T) {
	b := newTestBroker(t)

	msg, _, posted, err := b.ReportAgentIssue("ceo", "general", "", "Claude CLI requires login. Run `claude login` or use /init to choose a different provider.")
	if err != nil {
		t.Fatalf("ReportAgentIssue: %v", err)
	}
	if !posted {
		t.Fatal("expected the auth-error path to post a system card")
	}
	if msg.From != "system" {
		t.Errorf("expected From=system (banner card), got %q", msg.From)
	}
	if msg.Kind != "system_auth_error" {
		t.Errorf("expected Kind=system_auth_error, got %q", msg.Kind)
	}
	if !strings.Contains(string(msg.Payload), `"provider":"claude-code"`) {
		t.Errorf("expected payload to include provider=claude-code, got %s", string(msg.Payload))
	}
	if !strings.Contains(string(msg.Payload), `"sign_in_command":"claude auth login"`) {
		t.Errorf("expected payload to include sign_in_command, got %s", string(msg.Payload))
	}
	// The legacy agent_issue records list must be empty — the auth path
	// short-circuits before the issue table is touched.
	if len(b.AgentIssues()) != 0 {
		t.Errorf("auth error must not populate AgentIssues, got %+v", b.AgentIssues())
	}
}

// TestReportAgentIssueAuthErrorIdentifiesCodex covers the codex variant
// of the runtime-specific sign-in CTA.
func TestReportAgentIssueAuthErrorIdentifiesCodex(t *testing.T) {
	b := newTestBroker(t)

	msg, _, posted, err := b.ReportAgentIssue("eng", "general", "", "Codex CLI requires login. Run `codex login` or use /provider to choose a different provider.")
	if err != nil {
		t.Fatalf("ReportAgentIssue: %v", err)
	}
	if !posted {
		t.Fatal("expected the auth-error path to post a system card")
	}
	if msg.Kind != "system_auth_error" {
		t.Errorf("expected Kind=system_auth_error, got %q", msg.Kind)
	}
	if !strings.Contains(string(msg.Payload), `"provider":"codex"`) {
		t.Errorf("expected payload to include provider=codex, got %s", string(msg.Payload))
	}
	if !strings.Contains(string(msg.Payload), `"sign_in_command":"codex login"`) {
		t.Errorf("expected payload to include codex sign_in_command, got %s", string(msg.Payload))
	}
}

// TestReportAgentIssueAuthErrorDedupesWithinChannel ensures that
// repeated auth errors from the same provider don't stack identical
// banners. The first one stays visible; subsequent emits are suppressed.
func TestReportAgentIssueAuthErrorDedupesWithinChannel(t *testing.T) {
	b := newTestBroker(t)

	_, _, posted1, err := b.ReportAgentIssue("ceo", "general", "", "claude requires login. run `claude login`")
	if err != nil || !posted1 {
		t.Fatalf("first auth error: posted=%v err=%v", posted1, err)
	}
	_, _, posted2, err := b.ReportAgentIssue("ceo", "general", "", "claude requires login. run `claude login`")
	if err != nil {
		t.Fatalf("second auth error: %v", err)
	}
	if posted2 {
		t.Error("second identical auth banner should be deduped")
	}

	// Exactly one system_auth_error in the channel.
	count := 0
	for _, m := range b.ChannelMessages("general") {
		if m.Kind == "system_auth_error" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one system_auth_error banner, got %d", count)
	}
}

// TestReportAgentIssueLeavesNonAuthErrorsAsAgentIssue verifies the fork
// is precise: non-auth visible issues still take the legacy agent_issue
// path so self-heal approval flows aren't broken.
func TestReportAgentIssueLeavesNonAuthErrorsAsAgentIssue(t *testing.T) {
	b := newTestBroker(t)

	msg, _, posted, err := b.ReportAgentIssue("ceo", "general", "", "browser access is not available")
	if err != nil || !posted {
		t.Fatalf("non-auth issue: posted=%v err=%v", posted, err)
	}
	if msg.From != "ceo" {
		t.Errorf("expected non-auth issue to stay agent-authored, got From=%q", msg.From)
	}
	if msg.Kind != "agent_issue" {
		t.Errorf("expected Kind=agent_issue for non-auth path, got %q", msg.Kind)
	}
	if len(b.AgentIssues()) != 1 {
		t.Errorf("expected one entry in AgentIssues, got %d", len(b.AgentIssues()))
	}
}
