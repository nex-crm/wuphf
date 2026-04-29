package team

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	statePath := leakedBrokerStatePath(t)

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

// seedActiveSkill is a helper for the disable/enable tests — drops an active
// skill into b.skills with the required fields populated.
func seedActiveSkill(t *testing.T, b *Broker, name string) {
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
		Status:      "active",
		CreatedAt:   "2026-04-28T00:00:00Z",
		UpdatedAt:   "2026-04-28T00:00:00Z",
	})
}

// TestHandleSkillDisable covers the happy path and the conflict case.
// PR 7: disabled is a fourth status alongside proposed/active/archived. The
// handler refuses any source status other than active so the UI can model
// disable as a one-step transition off active.
func TestHandleSkillDisable(t *testing.T) {
	t.Parallel()

	t.Run("active flips to disabled", func(t *testing.T) {
		t.Parallel()
		_, b, do := crudTestServer(t)
		seedActiveSkill(t, b, "deploy-frontend")

		resp, body := do(http.MethodPost, "/skills/deploy-frontend/disable", map[string]any{
			"reason": "paused while we revise the runbook",
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, body %s", resp.StatusCode, body)
		}
		var out struct {
			Skill teamSkill `json:"skill"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.Skill.Status != "disabled" {
			t.Errorf("status: got %q, want disabled", out.Skill.Status)
		}

		// Catalog filter must skip the now-disabled skill.
		b.mu.Lock()
		b.members = []officeMember{{Slug: "ceo", BuiltIn: true}, {Slug: "deploy-bot"}}
		// Re-scope the seeded skill so the predicate match is unambiguous.
		for i := range b.skills {
			if b.skills[i].Name == "deploy-frontend" {
				b.skills[i].OwnerAgents = []string{"deploy-bot"}
			}
		}
		visible := b.listSkillsForAgentLocked("deploy-bot", listSkillsOpts{activeOnly: true})
		b.mu.Unlock()
		for _, sk := range visible {
			if sk.Name == "deploy-frontend" {
				t.Error("disabled skill must not appear in activeOnly catalog")
			}
		}
	})

	t.Run("non-active returns 409", func(t *testing.T) {
		t.Parallel()
		_, b, do := crudTestServer(t)
		seedProposedSkill(t, b, "weekly-retro")

		resp, body := do(http.MethodPost, "/skills/weekly-retro/disable", map[string]any{})
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status %d, body %s, want 409", resp.StatusCode, body)
		}
	})

	t.Run("missing skill returns 404", func(t *testing.T) {
		t.Parallel()
		_, _, do := crudTestServer(t)
		resp, _ := do(http.MethodPost, "/skills/does-not-exist/disable", map[string]any{})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status %d, want 404", resp.StatusCode)
		}
	})
}

// TestHandleSkillEnable covers the disabled -> active transition and the
// conflict for any other source status.
func TestHandleSkillEnable(t *testing.T) {
	t.Parallel()

	t.Run("disabled flips to active", func(t *testing.T) {
		t.Parallel()
		_, b, do := crudTestServer(t)
		seedActiveSkill(t, b, "deploy-frontend")

		// First disable it.
		if resp, body := do(http.MethodPost, "/skills/deploy-frontend/disable", map[string]any{}); resp.StatusCode != http.StatusOK {
			t.Fatalf("disable: status %d, body %s", resp.StatusCode, body)
		}

		resp, body := do(http.MethodPost, "/skills/deploy-frontend/enable", map[string]any{})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("enable: status %d, body %s", resp.StatusCode, body)
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

		// State persists through the broker so a restart sees active.
		b.mu.Lock()
		var found *teamSkill
		for i := range b.skills {
			if b.skills[i].Name == "deploy-frontend" {
				found = &b.skills[i]
				break
			}
		}
		status := ""
		if found != nil {
			status = found.Status
		}
		b.mu.Unlock()
		if status != "active" {
			t.Errorf("in-memory status: got %q, want active", status)
		}
	})

	t.Run("non-disabled returns 409", func(t *testing.T) {
		t.Parallel()
		_, b, do := crudTestServer(t)
		seedActiveSkill(t, b, "live-skill")
		_ = b

		resp, body := do(http.MethodPost, "/skills/live-skill/enable", map[string]any{})
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status %d, body %s, want 409", resp.StatusCode, body)
		}
	})
}

// TestHandleSkillRestore covers the archived -> proposed transition. Since
// findSkillByNameLocked excludes archived, the handler scans b.skills directly;
// this test pins down that contract.
func TestHandleSkillRestore(t *testing.T) {
	t.Parallel()

	t.Run("archived flips to proposed", func(t *testing.T) {
		t.Parallel()
		_, b, do := crudTestServer(t)
		seedActiveSkill(t, b, "weekly-retro")

		if resp, body := do(http.MethodPost, "/skills/weekly-retro/archive", map[string]any{}); resp.StatusCode != http.StatusOK {
			t.Fatalf("archive: status %d, body %s", resp.StatusCode, body)
		}

		resp, body := do(http.MethodPost, "/skills/weekly-retro/restore", map[string]any{})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("restore: status %d, body %s", resp.StatusCode, body)
		}
		var out struct {
			Skill teamSkill `json:"skill"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.Skill.Status != "proposed" {
			t.Errorf("status: got %q, want proposed (re-confirm gate)", out.Skill.Status)
		}
	})

	t.Run("non-archived returns 409", func(t *testing.T) {
		t.Parallel()
		_, b, do := crudTestServer(t)
		seedActiveSkill(t, b, "still-live")

		resp, body := do(http.MethodPost, "/skills/still-live/restore", map[string]any{})
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status %d, body %s, want 409", resp.StatusCode, body)
		}
	})

	t.Run("missing skill returns 404", func(t *testing.T) {
		t.Parallel()
		_, _, do := crudTestServer(t)
		resp, _ := do(http.MethodPost, "/skills/missing/restore", map[string]any{})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status %d, want 404", resp.StatusCode)
		}
	})
}

// TestHandleSkillDisable_DrainsInterviewRequests verifies that disabling a
// skill drains any matching pending skill_proposal interview entries —
// symmetric with approve/reject (D8).
func TestHandleSkillDisable_DrainsInterviewRequests(t *testing.T) {
	t.Parallel()
	_, b, do := crudTestServer(t)
	seedActiveSkill(t, b, "deploy-frontend")

	// Hand-seed a pending skill_proposal interview entry that points at the
	// active skill via ReplyTo. Disable should drain it.
	b.mu.Lock()
	b.requests = append(b.requests, humanInterview{
		ID:        "req-1",
		Kind:      "skill_proposal",
		ReplyTo:   "deploy-frontend",
		Status:    "pending",
		CreatedAt: "2026-04-28T00:00:00Z",
		UpdatedAt: "2026-04-28T00:00:00Z",
	})
	b.mu.Unlock()

	resp, body := do(http.MethodPost, "/skills/deploy-frontend/disable", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if req.ID == "req-1" && req.Status != "answered" {
			t.Errorf("skill_proposal request status: got %q, want answered", req.Status)
		}
	}
}
