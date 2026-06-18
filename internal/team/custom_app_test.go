package team

import (
	"strings"
	"testing"
	"time"
)

const validAppHTML = `<!doctype html><html><head><style>body{font-family:Georgia}</style></head><body><div id="root"></div><form><input/></form><script>var x=1;</script></body></html>`

func TestCustomAppStoreCreateGetListUpdateDelete(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	now := time.Unix(1_700_000_000, 0).UTC()

	created, err := store.Save(CustomAppWriteRequest{
		Name:        "Standup Digest",
		Icon:        "📊",
		Summary:     "Daily standup",
		Description: "Lists each agent's open tasks grouped by status.",
		HTML:        validAppHTML,
		Actor:       "app-builder",
	}, now)
	if err != nil {
		t.Fatalf("Save create: %v", err)
	}
	if !strings.HasPrefix(created.ID, "app_") {
		t.Fatalf("unexpected id %q", created.ID)
	}
	if created.Version != 1 {
		t.Fatalf("version = %d, want 1", created.Version)
	}
	if created.Slug != "standup-digest" {
		t.Fatalf("slug = %q, want standup-digest", created.Slug)
	}
	if created.Entry != customAppEntry {
		t.Fatalf("entry = %q, want %q", created.Entry, customAppEntry)
	}

	gotApp, gotHTML, err := store.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotHTML != validAppHTML {
		t.Fatalf("Get html mismatch")
	}
	if gotApp.Name != "Standup Digest" {
		t.Fatalf("Get name = %q", gotApp.Name)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("List = %+v, want one app %s", list, created.ID)
	}

	// Update in place keeps the id and bumps the version.
	updated, err := store.Save(CustomAppWriteRequest{
		ID:          created.ID,
		Name:        "Standup Digest",
		Description: "Now also shows blocked tasks.",
		HTML:        validAppHTML,
		Actor:       "app-builder",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Save update: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("update changed id: %q -> %q", created.ID, updated.ID)
	}
	if updated.Version != 2 {
		t.Fatalf("update version = %d, want 2", updated.Version)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Fatalf("update changed createdAt")
	}

	if err := store.Delete(created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := store.Get(created.ID); err == nil {
		t.Fatalf("Get after delete: expected error")
	}
	if got, _ := store.List(); len(got) != 0 {
		t.Fatalf("List after delete = %+v, want empty", got)
	}
}

func TestCustomAppStoreUpdateMissingIsCallerError(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	_, err := store.Save(CustomAppWriteRequest{
		ID:   "app_0123456789abcdef",
		Name: "Ghost",
		HTML: validAppHTML,
	}, time.Now())
	if err == nil || !isCustomAppCallerError(err) {
		t.Fatalf("update of missing app: got %v, want caller error", err)
	}
}

func TestValidateCustomAppHTML(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		wantErr bool
	}{
		{"valid singlefile-ish", validAppHTML, false},
		{"form allowed", `<form><button>go</button></form>`, false},
		{"inline svg img data uri", `<img src="data:image/png;base64,AAAA">`, false},
		{"empty", "", true},
		{"external script", `<script src="https://evil.example/x.js"></script>`, true},
		{"external stylesheet link", `<link rel="stylesheet" href="data:,">`, true},
		{"base tag", `<base href="https://evil.example/">`, true},
		{"iframe", `<iframe src="data:,"></iframe>`, true},
		{"object", `<object data="data:,"></object>`, true},
		{"inline event handler", `<div onclick="steal()">x</div>`, true},
		{"css import", `<style>@import url('x.css');</style>`, true},
		{"external image url", `<img src="https://evil.example/x.png">`, true},
		{"meta refresh", `<meta http-equiv="refresh" content="0;url=https://x">`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCustomAppHTML(tc.html)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && err != nil && !isCustomAppCallerError(err) {
				t.Fatalf("error is not a caller error: %v", err)
			}
		})
	}
}

// TestCustomAppVersionRetentionAndRollback is the IRON RULE regression for the
// modify loop: an update must NOT destroy the prior version, and rollback must
// restore its exact bytes as a new forward version. Before this, Save
// overwrote index.html in place and the version counter was a false affordance.
func TestCustomAppVersionRetentionAndRollback(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	now := time.Unix(1_700_000_000, 0).UTC()
	htmlA := validAppHTML
	htmlB := `<!doctype html><html><head></head><body><div id="root">B</div><script>var b=2;</script></body></html>`

	a, err := store.Save(CustomAppWriteRequest{Name: "Tool", HTML: htmlA, Actor: "app-builder"}, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := a.ID

	b2, err := store.Save(CustomAppWriteRequest{ID: id, Name: "Tool", HTML: htmlB, Actor: "app-builder"}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if b2.Version != 2 {
		t.Fatalf("update version = %d, want 2", b2.Version)
	}

	_, cur, err := store.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cur != htmlB {
		t.Fatalf("current bytes are not htmlB after update")
	}

	vs, err := store.ListVersions(id)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(vs) != 2 || vs[0].Version != 2 || vs[1].Version != 1 {
		t.Fatalf("versions = %v, want [v2 v1]", vs)
	}
	// The newest snapshot is the current build; the prior one is not.
	if !vs[0].Current || vs[1].Current {
		t.Fatalf("current flags = [%v %v], want [true false]", vs[0].Current, vs[1].Current)
	}
	// Capture metadata (who/when) rides along with each retained build so the
	// timeline can label it.
	if vs[1].UpdatedBy != "app-builder" || vs[1].UpdatedAt == "" {
		t.Fatalf("v1 meta = %+v, want updatedBy app-builder + a timestamp", vs[1])
	}

	// Rollback to v1 restores htmlA's exact bytes as a NEW version (append-only).
	r, err := store.Rollback(id, 1, "human", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if r.Version != 3 {
		t.Fatalf("rollback version = %d, want 3", r.Version)
	}
	_, cur2, _ := store.Get(id)
	if cur2 != htmlA {
		t.Fatalf("rollback did not restore htmlA bytes")
	}
	if vs2, _ := store.ListVersions(id); len(vs2) != 3 {
		t.Fatalf("versions after rollback = %v, want 3 entries", vs2)
	}
}

// TestCustomAppGetVersionNonDestructive verifies the timeline's preview read:
// fetching a past version returns its exact bytes + metadata and does NOT change
// the current build.
func TestCustomAppGetVersionNonDestructive(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	now := time.Unix(1_700_000_000, 0).UTC()
	htmlA := validAppHTML
	htmlB := `<!doctype html><html><head></head><body><div id="root">B</div><script>var b=2;</script></body></html>`

	a, err := store.Save(CustomAppWriteRequest{Name: "Tool", HTML: htmlA, Actor: "app-builder"}, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := a.ID
	if _, err := store.Save(CustomAppWriteRequest{ID: id, Name: "Tool", HTML: htmlB, Actor: "pam"}, now.Add(time.Minute)); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Reading v1 returns htmlA's bytes, its capture metadata, and is NOT current.
	ver, body, err := store.GetVersion(id, 1)
	if err != nil {
		t.Fatalf("get version 1: %v", err)
	}
	if body != htmlA {
		t.Fatalf("v1 bytes are not htmlA")
	}
	if ver.Current {
		t.Fatalf("v1 should not be current")
	}
	if ver.UpdatedBy != "app-builder" {
		t.Fatalf("v1 updatedBy = %q, want app-builder", ver.UpdatedBy)
	}

	// v2 reads back as the current build.
	if v2, _, err := store.GetVersion(id, 2); err != nil || !v2.Current {
		t.Fatalf("v2 = %+v err=%v, want current", v2, err)
	}

	// The current build is untouched by the preview reads.
	if _, cur, _ := store.Get(id); cur != htmlB {
		t.Fatalf("preview read mutated the current build")
	}

	// Unknown version is a caller (400-class) error, not a 500.
	if _, _, err := store.GetVersion(id, 99); !isCustomAppCallerError(err) {
		t.Fatalf("get unknown version err = %v, want caller error", err)
	}
}

func TestCustomAppSourcePersistence(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	store.buildBundle = stubBuildBundle
	now := time.Unix(1_700_000_000, 0).UTC()
	a, err := store.Save(CustomAppWriteRequest{
		Name: "Tool", HTML: validAppHTML, Actor: "app-builder",
		Files: map[string]string{"src/App.tsx": "export const App = () => null", "package.json": "{}"},
	}, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	src, err := store.Source(a.ID)
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if src["src/App.tsx"] == "" || src["package.json"] != "{}" {
		t.Fatalf("source not persisted: %v", src)
	}
	// The host always writes the protected contract files into the persisted
	// source, even when the agent omitted them, so a later edit + build has them.
	for _, p := range []string{"src/wuphf-bridge.ts", "src/wuphf-inspector.ts", "vite.config.ts"} {
		if src[p] == "" {
			t.Fatalf("protected file %q not persisted: have %v", p, keysOf(src))
		}
	}
	// Update replaces the app's own source set wholesale (deletes propagate); the
	// host-owned protected files persist across the replace.
	if _, err := store.Save(CustomAppWriteRequest{
		ID: a.ID, Name: "Tool", HTML: validAppHTML, Actor: "app-builder",
		Files: map[string]string{"src/App.tsx": "v2"},
	}, now.Add(time.Minute)); err != nil {
		t.Fatalf("update: %v", err)
	}
	src2, _ := store.Source(a.ID)
	if src2["src/App.tsx"] != "v2" {
		t.Fatalf("source not replaced: %v", src2)
	}
	// package.json was dropped from the new file set, so it is gone (delete
	// propagated); only the new app file + the 3 protected files remain.
	if _, ok := src2["package.json"]; ok {
		t.Fatalf("dropped file package.json not deleted: %v", keysOf(src2))
	}
	if len(src2) != 4 {
		t.Fatalf("source set = %v, want src/App.tsx + 3 protected files", keysOf(src2))
	}
}

func TestCustomAppSourcePathRejection(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	bad := []string{
		"../escape.txt", "/abs.txt", "node_modules/x", "dist/index.html", "a/../../b",
		".vite/deps/foo.js", // build cache: the host owns it
		// build-tool config that would tamper with the SERVER-SIDE build env:
		".npmrc", ".bunfig.toml", "nested/.npmrc", ".env", ".env.local", ".env.production",
	}
	for _, p := range bad {
		_, err := store.Save(CustomAppWriteRequest{
			Name: "T", HTML: validAppHTML, Files: map[string]string{p: "x"},
		}, time.Now())
		if err == nil || !isCustomAppCallerError(err) {
			t.Fatalf("path %q: expected caller error, got %v", p, err)
		}
	}
}

func TestCustomAppIDValidation(t *testing.T) {
	good := []string{"app_0123456789abcdef", "app_ffffffffffffffff"}
	bad := []string{"", "app_short", "ra_0123456789abcdef", "app_0123456789ABCDEF", "app_0123456789abcdeg"}
	for _, id := range good {
		if err := validateCustomAppID(id); err != nil {
			t.Fatalf("valid id %q rejected: %v", id, err)
		}
	}
	for _, id := range bad {
		if err := validateCustomAppID(id); err == nil {
			t.Fatalf("bad id %q accepted", id)
		}
	}
}

func TestAppProposalApprovedAndBrief(t *testing.T) {
	if !appProposalApproved("approve") || !appProposalApproved("approve_with_note") {
		t.Fatalf("approve choices should be approved")
	}
	for _, c := range []string{"reject", "reject_with_steer", "needs_more_info", ""} {
		if appProposalApproved(c) {
			t.Fatalf("%q should NOT be approved", c)
		}
	}
	title, details := appBuilderTaskBrief(appProposalSpec{
		Name: "Lead Scorer", Description: "Score inbound leads.", AppID: "",
	}, "use our ICP weights")
	if title != "Build app: Lead Scorer" {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(details, "Score inbound leads.") || !strings.Contains(details, "use our ICP weights") || !strings.Contains(details, "register_app") {
		t.Fatalf("brief missing parts: %q", details)
	}
	upTitle, upDetails := appBuilderTaskBrief(appProposalSpec{
		Name: "Lead Scorer", Description: "Add CSV export.", AppID: "app_0123456789abcdef",
	}, "")
	if upTitle != "Improve app: Lead Scorer" {
		t.Fatalf("update title = %q", upTitle)
	}
	if !strings.Contains(upDetails, "app_0123456789abcdef") || !strings.Contains(upDetails, "get_app") {
		t.Fatalf("update brief missing parts: %q", upDetails)
	}
}
