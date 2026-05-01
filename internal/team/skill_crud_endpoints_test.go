package team

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// crudTestServer wires up a broker behind an in-process HTTP server with the
// CRUD subpath router registered. Returns the server + a helper that issues
// authenticated JSON requests.
func crudTestServer(t *testing.T) (*httptest.Server, *Broker, func(method, path string, body any) (*http.Response, []byte)) {
	t.Helper()
	b := newTestBroker(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/skills", b.requireAuth(b.handleSkills))
	mux.HandleFunc("/skills/", b.requireAuth(b.handleSkillsSubpath))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	do := func(method, p string, body any) (*http.Response, []byte) {
		t.Helper()
		var buf io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			buf = bytes.NewReader(raw)
		}
		req, _ := http.NewRequest(method, srv.URL+p, buf)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, p, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp, out
	}
	return srv, b, do
}

// seedProposedSkill is a helper that drops a pre-baked proposed skill into
// b.skills so the approve/reject tests don't have to round-trip through the
// scanner.
func seedProposedSkill(t *testing.T, b *Broker, name string) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.skills = append(b.skills, teamSkill{
		ID:          "skill-" + name,
		Name:        name,
		Title:       name,
		Description: "Seeded for tests.",
		Content:     "Step 1: do something.",
		CreatedBy:   "archivist",
		Channel:     "general",
		Status:      "proposed",
		CreatedAt:   "2026-04-28T00:00:00Z",
		UpdatedAt:   "2026-04-28T00:00:00Z",
	})
}

// TestSkillApproveEndpoint_FlipsToActive covers the happy path: a proposed
// skill flips to active and the approval counter increments by one.
func TestSkillApproveEndpoint_FlipsToActive(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "send-digest")

	resp, body := do(http.MethodPost, "/skills/send-digest/approve", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	var out struct {
		Skill teamSkill `json:"skill"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Skill.Status != "active" {
		t.Errorf("status: got %q, want active", out.Skill.Status)
	}

	b.mu.Lock()
	count := b.skillCompileMetrics.ProposalsApprovedTotal
	b.mu.Unlock()
	if count != 1 {
		t.Errorf("ProposalsApprovedTotal: got %d, want 1", count)
	}
}

// TestSkillApproveEndpoint_ConflictWhenNotProposed checks that approving an
// already-active skill returns 409.
func TestSkillApproveEndpoint_ConflictWhenNotProposed(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "already-active")
	b.mu.Lock()
	for i := range b.skills {
		if b.skills[i].Name == "already-active" {
			b.skills[i].Status = "active"
		}
	}
	b.mu.Unlock()

	resp, _ := do(http.MethodPost, "/skills/already-active/approve", map[string]any{})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", resp.StatusCode)
	}
}

// TestSkillRejectEndpoint_RemovesAndReturnsToken covers the optimistic remove
// + undo token issuance.
func TestSkillRejectEndpoint_RemovesAndReturnsToken(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "doomed-skill")

	resp, body := do(http.MethodPost, "/skills/doomed-skill/reject", map[string]any{
		"reason": "too risky for v1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	var out struct {
		OK        bool   `json:"ok"`
		UndoToken string `json:"undo_token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true")
	}
	if out.UndoToken == "" {
		t.Error("expected non-empty undo_token")
	}
	if out.ExpiresIn <= 0 {
		t.Errorf("expires_in: got %d, want > 0", out.ExpiresIn)
	}

	// Verify the skill was removed from b.skills.
	b.mu.Lock()
	for _, s := range b.skills {
		if skillSlug(s.Name) == "doomed-skill" {
			t.Errorf("skill should be removed from b.skills, but found %q", s.Name)
		}
	}
	b.mu.Unlock()
}

// TestSkillRejectAndUndo_RoundTrip covers the full reject → undo flow within
// the 5-second window.
func TestSkillRejectAndUndo_RoundTrip(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "undo-me")

	// Step 1: reject.
	resp, body := do(http.MethodPost, "/skills/undo-me/reject", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject status %d, body %s", resp.StatusCode, body)
	}
	var rejectResp struct {
		UndoToken string `json:"undo_token"`
	}
	if err := json.Unmarshal(body, &rejectResp); err != nil {
		t.Fatalf("unmarshal reject: %v", err)
	}

	// Step 2: undo.
	resp, body = do(http.MethodPost, "/skills/reject/undo", map[string]any{
		"undo_token": rejectResp.UndoToken,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status %d, body %s", resp.StatusCode, body)
	}

	var undoResp struct {
		Skill teamSkill `json:"skill"`
	}
	if err := json.Unmarshal(body, &undoResp); err != nil {
		t.Fatalf("unmarshal undo: %v", err)
	}
	if undoResp.Skill.Name != "undo-me" {
		t.Errorf("revived name: got %q, want undo-me", undoResp.Skill.Name)
	}
	if undoResp.Skill.Status != "proposed" {
		t.Errorf("revived status: got %q, want proposed", undoResp.Skill.Status)
	}

	// Verify the skill is back in b.skills.
	b.mu.Lock()
	found := b.findSkillByNameLocked("undo-me")
	b.mu.Unlock()
	if found == nil {
		t.Error("skill not restored to b.skills after undo")
	}
}

// TestSkillRejectUndo_RejectsExpiredToken checks that an unknown / expired
// token is rejected with 404.
func TestSkillRejectUndo_RejectsExpiredToken(t *testing.T) {
	t.Parallel()
	_, _, do := crudTestServer(t)

	resp, _ := do(http.MethodPost, "/skills/reject/undo", map[string]any{
		"undo_token": "unknown-token",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", resp.StatusCode)
	}
}

// TestValidateSkillFilePath covers the path-traversal + allow-list checks.
func TestValidateSkillFilePath(t *testing.T) {
	t.Parallel()

	good := []string{
		"references/api.md",
		"templates/email.html",
		"scripts/run.sh",
		"assets/image.png",
		"references/sub/nested.md",
	}
	for _, p := range good {
		if err := validateSkillFilePath(p); err != nil {
			t.Errorf("good path %q rejected: %v", p, err)
		}
	}

	bad := []struct {
		path string
		want string
	}{
		{"", "required"},
		{"/etc/passwd", "relative"},
		{"../etc/passwd", "traverse"},
		{"references/../../../etc/passwd", "traverse"},
		{"random/file.md", "must be under"},
		{"references/" + strings.Repeat("a", 200), "exceeds"},
	}
	for _, tc := range bad {
		err := validateSkillFilePath(tc.path)
		if err == nil {
			t.Errorf("bad path %q accepted", tc.path)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("path %q: error %q should contain %q", tc.path, err.Error(), tc.want)
		}
	}
}

// TestSkillArchiveEndpoint flips an existing skill to archived.
func TestSkillArchiveEndpoint(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "archive-me")

	resp, body := do(http.MethodPost, "/skills/archive-me/archive", map[string]any{
		"reason": "no longer relevant",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.skills {
		if skillSlug(s.Name) == "archive-me" {
			if s.Status != "archived" {
				t.Errorf("status: got %q, want archived", s.Status)
			}
			return
		}
	}
	t.Error("skill not found after archive (it should still exist with status=archived)")
}

// TestSkillPatchEndpoint_FindReplace covers the body-only patch path.
func TestSkillPatchEndpoint_FindReplace(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "patch-me")
	b.mu.Lock()
	for i := range b.skills {
		if b.skills[i].Name == "patch-me" {
			b.skills[i].Content = "Step 1: typo here.\nStep 2: continue."
		}
	}
	b.mu.Unlock()

	resp, body := do(http.MethodPost, "/skills/patch-me/patch", map[string]any{
		"old_string": "typo here",
		"new_string": "fixed text",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.skills {
		if s.Name == "patch-me" {
			if !strings.Contains(s.Content, "fixed text") {
				t.Errorf("content not patched: %q", s.Content)
			}
			if strings.Contains(s.Content, "typo here") {
				t.Error("old text still present after patch")
			}
			return
		}
	}
	t.Error("skill not found after patch")
}

// TestSkillArchiveStatusSurvivesRestart is a regression test for the state
// replay quirk: archiving a skill must survive a broker restart. Previously,
// if saveLocked was not called after the archive wiki-enqueue, broker-state.json
// still held status=proposed/active, and a fresh broker loading from that file
// would revert the skill to the stale status.
func TestSkillArchiveStatusSurvivesRestart(t *testing.T) {
	// Use a named temp dir so the state path is in scope for both brokers.
	statePath := filepath.Join(t.TempDir(), "broker-state.json")

	b1 := NewBrokerAt(statePath)
	mux := http.NewServeMux()
	mux.HandleFunc("/skills", b1.requireAuth(b1.handleSkills))
	mux.HandleFunc("/skills/", b1.requireAuth(b1.handleSkillsSubpath))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	do := func(method, path string, body any) (*http.Response, []byte) {
		t.Helper()
		var buf io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			buf = bytes.NewReader(raw)
		}
		req, _ := http.NewRequest(method, srv.URL+path, buf)
		req.Header.Set("Authorization", "Bearer "+b1.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp, out
	}

	// Seed a skill as active (not proposed) so the archive endpoint accepts it.
	b1.mu.Lock()
	b1.skills = append(b1.skills, teamSkill{
		ID:          "skill-state-replay",
		Name:        "state-replay",
		Title:       "State Replay Test",
		Description: "Regression test skill.",
		Content:     "Step 1: verify.",
		CreatedBy:   "archivist",
		Channel:     "general",
		Status:      "active",
		CreatedAt:   "2026-04-28T00:00:00Z",
		UpdatedAt:   "2026-04-28T00:00:00Z",
	})
	b1.mu.Unlock()

	// Archive via the HTTP handler. The fix ensures saveLocked is called
	// inside the handler so the status is flushed to broker-state.json.
	resp, body := do(http.MethodPost, "/skills/state-replay/archive", map[string]any{
		"reason": "regression test",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive: status %d, body %s", resp.StatusCode, body)
	}

	// Simulate a broker restart by constructing a fresh broker on the same
	// state path and loading the state from disk. The reloaded broker must
	// see status=archived, not the stale "active" value from before the
	// archive call.
	b2 := reloadedBroker(t, b1)
	b2.mu.Lock()
	var found *teamSkill
	for i := range b2.skills {
		if skillSlug(b2.skills[i].Name) == "state-replay" {
			found = &b2.skills[i]
			break
		}
	}
	b2.mu.Unlock()

	if found == nil {
		t.Fatal("state-replay skill missing from reloaded broker")
	}
	if found.Status != "archived" {
		t.Errorf("state replay: status after restart = %q, want %q", found.Status, "archived")
	}
}

// TestSkillPatchEndpoint_ConflictOnMultipleMatches verifies the 409 path when
// the old_string matches more than once and replace_all is false.
func TestSkillPatchEndpoint_ConflictOnMultipleMatches(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "ambiguous")
	b.mu.Lock()
	for i := range b.skills {
		if b.skills[i].Name == "ambiguous" {
			b.skills[i].Content = "foo bar foo baz foo"
		}
	}
	b.mu.Unlock()

	resp, _ := do(http.MethodPost, "/skills/ambiguous/patch", map[string]any{
		"old_string": "foo",
		"new_string": "FOO",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status: got %d, want 409", resp.StatusCode)
	}
}

// setSkillStatus is a tiny helper that flips the in-memory status of a seeded
// skill so the lifecycle tests can drive transitions without round-tripping
// through other handlers.
func setSkillStatus(t *testing.T, b *Broker, name, status string) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.skills {
		if skillSlug(b.skills[i].Name) == skillSlug(name) {
			b.skills[i].Status = status
			return
		}
	}
	t.Fatalf("setSkillStatus: skill %q not found", name)
}

// TestSkillDisableEnableRoundTrip covers the active → disabled → active
// lifecycle. /skills/{name}/disable and /enable were 404'd before this fix
// because only /archive was wired in handleSkillsCRUDSubpath.
func TestSkillDisableEnableRoundTrip(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "round-trip")
	setSkillStatus(t, b, "round-trip", "active")

	// Disable.
	resp, body := do(http.MethodPost, "/skills/round-trip/disable", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable: status %d, body %s", resp.StatusCode, body)
	}
	var disabled struct {
		Skill teamSkill `json:"skill"`
	}
	if err := json.Unmarshal(body, &disabled); err != nil {
		t.Fatalf("disable unmarshal: %v", err)
	}
	if disabled.Skill.Status != "disabled" {
		t.Errorf("disable status: got %q, want disabled", disabled.Skill.Status)
	}

	// Confirm GET /skills?include_disabled=true surfaces the disabled skill.
	resp, body = do(http.MethodGet, "/skills?include_disabled=true", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status %d, body %s", resp.StatusCode, body)
	}
	var list struct {
		Skills []teamSkill `json:"skills"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("list unmarshal: %v", err)
	}
	found := false
	for _, s := range list.Skills {
		if skillSlug(s.Name) == "round-trip" {
			found = true
			if s.Status != "disabled" {
				t.Errorf("listed status: got %q, want disabled", s.Status)
			}
		}
	}
	if !found {
		t.Error("disabled skill missing from /skills response")
	}

	// Enable.
	resp, body = do(http.MethodPost, "/skills/round-trip/enable", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable: status %d, body %s", resp.StatusCode, body)
	}
	var enabled struct {
		Skill teamSkill `json:"skill"`
	}
	if err := json.Unmarshal(body, &enabled); err != nil {
		t.Fatalf("enable unmarshal: %v", err)
	}
	if enabled.Skill.Status != "active" {
		t.Errorf("enable status: got %q, want active", enabled.Skill.Status)
	}
}

// TestSkillRestoreFromArchive covers the archive → restore round trip and
// asserts the skill is visible again in the default /skills listing.
func TestSkillRestoreFromArchive(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedProposedSkill(t, b, "restore-me")
	setSkillStatus(t, b, "restore-me", "active")

	// Archive.
	resp, body := do(http.MethodPost, "/skills/restore-me/archive", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive: status %d, body %s", resp.StatusCode, body)
	}

	// Default list must hide it.
	resp, body = do(http.MethodGet, "/skills", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list pre-restore: status %d, body %s", resp.StatusCode, body)
	}
	var preList struct {
		Skills []teamSkill `json:"skills"`
	}
	if err := json.Unmarshal(body, &preList); err != nil {
		t.Fatalf("pre-list unmarshal: %v", err)
	}
	for _, s := range preList.Skills {
		if skillSlug(s.Name) == "restore-me" {
			t.Errorf("archived skill should not appear in default list: status=%q", s.Status)
		}
	}

	// Restore.
	resp, body = do(http.MethodPost, "/skills/restore-me/restore", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore: status %d, body %s", resp.StatusCode, body)
	}
	var restored struct {
		Skill teamSkill `json:"skill"`
	}
	if err := json.Unmarshal(body, &restored); err != nil {
		t.Fatalf("restore unmarshal: %v", err)
	}
	if restored.Skill.Status != "active" {
		t.Errorf("restore status: got %q, want active", restored.Skill.Status)
	}

	// Default list must show it again.
	resp, body = do(http.MethodGet, "/skills", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list post-restore: status %d, body %s", resp.StatusCode, body)
	}
	var postList struct {
		Skills []teamSkill `json:"skills"`
	}
	if err := json.Unmarshal(body, &postList); err != nil {
		t.Fatalf("post-list unmarshal: %v", err)
	}
	found := false
	for _, s := range postList.Skills {
		if skillSlug(s.Name) == "restore-me" {
			found = true
			if s.Status != "active" {
				t.Errorf("restored listing status: got %q, want active", s.Status)
			}
		}
	}
	if !found {
		t.Error("restored skill missing from default /skills listing")
	}
}

// TestSkillDisableInvalidTransitions enforces the status invariants:
// - disable requires active or proposed
// - enable requires disabled
// - restore requires archived
func TestSkillDisableInvalidTransitions(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)

	// Disabling an archived skill → 409.
	seedProposedSkill(t, b, "already-archived")
	setSkillStatus(t, b, "already-archived", "archived")
	resp, _ := do(http.MethodPost, "/skills/already-archived/disable", map[string]any{})
	// Note: archived skills are also hidden by findSkillByNameLocked, so the
	// caller sees 404 before the status check. Either response (404 or 409)
	// signals the invalid transition; assert non-2xx.
	if resp.StatusCode == http.StatusOK {
		t.Errorf("disabling archived skill should fail; got 200")
	}

	// Enabling an active skill → 409.
	seedProposedSkill(t, b, "already-active")
	setSkillStatus(t, b, "already-active", "active")
	resp, body := do(http.MethodPost, "/skills/already-active/enable", map[string]any{})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("enable on active: status %d, want 409 — body=%s", resp.StatusCode, body)
	}

	// Restoring a non-archived skill → 409. The lookup uses the archive-aware
	// helper so the skill is reachable and the status check fires.
	seedProposedSkill(t, b, "still-proposed")
	resp, body = do(http.MethodPost, "/skills/still-proposed/restore", map[string]any{})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("restore on proposed: status %d, want 409 — body=%s", resp.StatusCode, body)
	}
}

// TestSkillEndpointsMethodAndPath covers method/path edge cases on the new
// verbs: GET → 405, missing skill → 404.
func TestSkillEndpointsMethodAndPath(t *testing.T) {
	t.Parallel()
	_, _, do := crudTestServer(t)

	// GET on disable → 405.
	resp, _ := do(http.MethodGet, "/skills/anything/disable", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /disable: status %d, want 405", resp.StatusCode)
	}

	// POST on a non-existent skill → 404.
	resp, _ = do(http.MethodPost, "/skills/nonexistent/disable", map[string]any{})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /disable nonexistent: status %d, want 404", resp.StatusCode)
	}

	// Same expectations for enable + restore.
	resp, _ = do(http.MethodGet, "/skills/anything/enable", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /enable: status %d, want 405", resp.StatusCode)
	}
	resp, _ = do(http.MethodPost, "/skills/nonexistent/enable", map[string]any{})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /enable nonexistent: status %d, want 404", resp.StatusCode)
	}
	resp, _ = do(http.MethodGet, "/skills/anything/restore", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /restore: status %d, want 405", resp.StatusCode)
	}
	resp, _ = do(http.MethodPost, "/skills/nonexistent/restore", map[string]any{})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("POST /restore nonexistent: status %d, want 404", resp.StatusCode)
	}
}
