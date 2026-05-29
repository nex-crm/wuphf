package provider

import (
	"path/filepath"
	"testing"
)

// TestResetClaudeSessionFor_ClearsOnlyTargetSlug locks in the per-slug
// reset hand-off used by the per-agent runtime picker. Switching one agent
// from claude-code to codex must not clobber another agent's resume id —
// they're independent dispatches that just happen to share the same store.
func TestResetClaudeSessionFor_ClearsOnlyTargetSlug(t *testing.T) {
	// Isolate the store to a temp file so this test doesn't read or write
	// the developer's real ~/.wuphf/providers/claude-sessions.json.
	prev := claudeSessionStoreFactory
	tmp := filepath.Join(t.TempDir(), "claude-sessions.json")
	claudeSessionStoreFactory = func() *claudeSessionStore {
		return newClaudeSessionStoreAt(tmp)
	}
	t.Cleanup(func() {
		claudeSessionStoreMu.Lock()
		claudeSessionStoreInstance = nil
		claudeSessionStoreMu.Unlock()
		claudeSessionStoreFactory = prev
	})
	// Re-init the singleton against the temp factory.
	claudeSessionStoreMu.Lock()
	claudeSessionStoreInstance = nil
	claudeSessionStoreMu.Unlock()
	store := getClaudeSessionStore()

	store.save("outreach", "sess-outreach", "/cwd")
	store.save("eng", "sess-eng", "/cwd")

	ResetClaudeSessionFor("outreach")

	if got := store.resumeSessionID("outreach", "/cwd"); got != "" {
		t.Errorf("outreach resume id should be cleared, got %q", got)
	}
	if got := store.resumeSessionID("eng", "/cwd"); got != "sess-eng" {
		t.Errorf("eng resume id should be untouched, got %q", got)
	}
}

// TestResetClaudeSessionFor_IdempotentOnUnknownSlug locks in the contract
// the broker relies on: provider switches publish member_updated events
// for any field change (name, role, runtime). The launcher calls
// ResetClaudeSessionFor unconditionally; on a no-runtime-change event
// (e.g. rename) the slug may not even have a stored session.
func TestResetClaudeSessionFor_IdempotentOnUnknownSlug(t *testing.T) {
	prev := claudeSessionStoreFactory
	tmp := filepath.Join(t.TempDir(), "claude-sessions.json")
	claudeSessionStoreFactory = func() *claudeSessionStore {
		return newClaudeSessionStoreAt(tmp)
	}
	t.Cleanup(func() {
		claudeSessionStoreMu.Lock()
		claudeSessionStoreInstance = nil
		claudeSessionStoreMu.Unlock()
		claudeSessionStoreFactory = prev
	})
	claudeSessionStoreMu.Lock()
	claudeSessionStoreInstance = nil
	claudeSessionStoreMu.Unlock()

	// No save first — clearing an unknown slug must not panic or error.
	ResetClaudeSessionFor("never-seen")
	// Empty slug must short-circuit instead of corrupting the store.
	ResetClaudeSessionFor("")
}
