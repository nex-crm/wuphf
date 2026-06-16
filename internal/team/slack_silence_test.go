package team

import "testing"

func TestSlackOutboundIsSilent(t *testing.T) {
	silent := []string{
		"",
		"   ",
		"NO_REPLY",
		"no_reply",
		"  NO_REPLY  ",
		"NO REPLY",
		"no   reply",
		"[SILENT]",
		"SILENT",
		"silent",
		"(silent)",
		"*(silent)*",
		"_silent_",
		"`silent`",
		"~silence~",
		"(no reply)",
		"no response",
		".",
		"...",
		"…",
		"🔇",
	}
	for _, s := range silent {
		if !slackOutboundIsSilent(s) {
			t.Errorf("expected silent: %q", s)
		}
	}

	speak := []string{
		"OFFICE-12 is done: the Linear vs Jira brief is ready.",
		"The deployment ran silently with no errors.",           // 'silent' inside prose
		"No reply has come back from hermes yet, still waiting", // 'no reply' inside prose
		"Approved. Proceeding with the send.",
		"Here is the result.",
		"@hermes you own OFFICE-3: draft the brief.",
	}
	for _, s := range speak {
		if slackOutboundIsSilent(s) {
			t.Errorf("expected to send (not silent): %q", s)
		}
	}
}

// A message LONGER than 64 chars is never treated as silence, even if it starts
// with a marker — that guards against suppressing a real message.
func TestSlackOutboundIsSilent_LongMessageNeverSilent(t *testing.T) {
	long := "NO_REPLY but actually here is a full and complete deliverable that exceeds the threshold"
	if slackOutboundIsSilent(long) {
		t.Fatalf("a >64-char message must never be suppressed: %q", long)
	}
}
