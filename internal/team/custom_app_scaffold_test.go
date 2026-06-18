package team

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/templates"
)

// TestScaffoldCreatesBuildingDraft locks the instant-preview contract: Scaffold
// materializes a real editable project (so the dev server can boot it in
// seconds) and records a "building" draft BEFORE the agent writes any code.
func TestScaffoldCreatesBuildingDraft(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	now := time.Unix(1_700_000_000, 0).UTC()
	id := customAppID("lead-scorer", "Lead Scorer", "general")

	app, err := store.Scaffold(id, "Lead Scorer", "", "app-builder", now)
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if app.ID != id {
		t.Fatalf("id = %q, want %q", app.ID, id)
	}
	if app.Status != customAppStatusBuilding {
		t.Fatalf("status = %q, want %q", app.Status, customAppStatusBuilding)
	}
	if app.Version != 0 {
		t.Fatalf("version = %d, want 0 (no published build yet)", app.Version)
	}

	// The sealed entry is a valid placeholder so Get/sealed view never 404s.
	gotApp, gotHTML, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotApp.Status != customAppStatusBuilding {
		t.Fatalf("persisted status = %q", gotApp.Status)
	}
	if !strings.Contains(gotHTML, "being built") {
		t.Fatalf("placeholder html missing: %q", gotHTML)
	}

	// The editable project landed so a dev server can boot it: project root
	// files at src/, app code under src/src/.
	src, err := store.Source(id)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	for _, want := range []string{"package.json", "vite.config.ts", "index.html", "src/App.tsx"} {
		if _, ok := src[want]; !ok {
			t.Fatalf("scaffold missing %q; have %v", want, keysOf(src))
		}
	}
}

// TestScaffoldIsIdempotent: a retried/deduped create must not clobber in-flight
// work — a second Scaffold for the same id returns the existing manifest.
func TestScaffoldIsIdempotent(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	now := time.Unix(1_700_000_000, 0).UTC()
	id := customAppID("lead-scorer", "Lead Scorer", "general")

	if _, err := store.Scaffold(id, "Lead Scorer", "", "app-builder", now); err != nil {
		t.Fatalf("Scaffold 1: %v", err)
	}
	// Simulate progress: drop a marker into the source tree.
	marker := filepath.Join(store.appDir(id), customAppSourceDir, "src", "App.tsx")
	if err := os.WriteFile(marker, []byte("// edited"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	again, err := store.Scaffold(id, "Renamed", "🚀", "someone", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Scaffold 2: %v", err)
	}
	if again.Name != "Lead Scorer" {
		t.Fatalf("idempotent scaffold renamed app: %q", again.Name)
	}
	body, err := os.ReadFile(marker)
	if err != nil || string(body) != "// edited" {
		t.Fatalf("idempotent scaffold clobbered source: %q err=%v", body, err)
	}
}

// TestPublishFlipsDraftToReadyAndPreservesNodeModules: register_app onto a
// pre-scaffolded draft must flip it to ready, bump the version, and KEEP
// node_modules so a running dev server survives the publish and hot-reloads the
// new source.
func TestPublishFlipsDraftToReadyAndPreservesNodeModules(t *testing.T) {
	store := newCustomAppStore(t.TempDir())
	now := time.Unix(1_700_000_000, 0).UTC()
	id := customAppID("lead-scorer", "Lead Scorer", "general")

	if _, err := store.Scaffold(id, "Lead Scorer", "", "app-builder", now); err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	// Simulate `bun install` having populated node_modules in the dev server.
	nm := filepath.Join(store.appDir(id), customAppSourceDir, "node_modules", "left-pad", "index.js")
	if err := os.MkdirAll(filepath.Dir(nm), 0o700); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(nm, []byte("module.exports={}"), 0o600); err != nil {
		t.Fatalf("write node_modules: %v", err)
	}

	published, err := store.Save(CustomAppWriteRequest{
		ID:    id,
		Name:  "Lead Scorer",
		HTML:  validAppHTML,
		Actor: "app-builder",
		Files: map[string]string{
			"package.json":   "{}",
			"src/App.tsx":    "export default function App(){return null}",
			"vite.config.ts": "export default {}",
			"index.html":     "<!doctype html>",
		},
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Save publish: %v", err)
	}
	if published.Status != customAppStatusReady {
		t.Fatalf("status = %q, want ready", published.Status)
	}
	if published.Version != 1 {
		t.Fatalf("version = %d, want 1", published.Version)
	}
	// node_modules survived the publish (dev server can keep running).
	if _, err := os.Stat(nm); err != nil {
		t.Fatalf("publish deleted node_modules: %v", err)
	}
	// ...but source was replaced (Source skips node_modules).
	src, err := store.Source(id)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if _, ok := src["src/App.tsx"]; !ok {
		t.Fatalf("published source missing src/App.tsx; have %v", keysOf(src))
	}
	for k := range src {
		if strings.HasPrefix(k, "node_modules/") {
			t.Fatalf("Source leaked node_modules entry %q", k)
		}
	}
}

func TestParseNewAppBuildTitle(t *testing.T) {
	cases := map[string]struct {
		name string
		ok   bool
	}{
		"Build app: Lead Scorer":   {"Lead Scorer", true},
		"create app:   CSV Export": {"CSV Export", true},
		"Improve app: Lead Scorer": {"", false},
		"Update app: X":            {"", false},
		"Write the plan":           {"", false},
		"Build app:":               {"", false},
	}
	for title, want := range cases {
		name, ok := parseNewAppBuildTitle(title)
		if ok != want.ok || name != want.name {
			t.Fatalf("parseNewAppBuildTitle(%q) = (%q,%v), want (%q,%v)", title, name, ok, want.name, want.ok)
		}
	}
}

// TestMutateTaskPrescaffoldsNewAppBuild is the end-to-end wiring: creating an
// app-builder "Build app: X" task pre-scaffolds the app (building draft on disk)
// and appends the workspace brief (with the app id) to the task details.
func TestMutateTaskPrescaffoldsNewAppBuild(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"ceo", appBuilderSlug}},
	}

	created, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Build app: Lead Scorer",
		Details:   "Score inbound leads by ICP fit.",
		Owner:     appBuilderSlug,
		CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("MutateTask create: %v", err)
	}
	if !strings.Contains(created.Task.Details, appWorkspaceBriefMarker) {
		t.Fatalf("task details missing workspace brief: %q", created.Task.Details)
	}
	if !strings.Contains(created.Task.Details, "register_app(app_id=app_") {
		t.Fatalf("task details missing app_id publish instruction: %q", created.Task.Details)
	}

	apps, err := b.appStore().List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var draft *CustomApp
	for i := range apps {
		if apps[i].Name == "Lead Scorer" {
			draft = &apps[i]
			break
		}
	}
	if draft == nil {
		t.Fatalf("expected a pre-scaffolded app, got %+v", apps)
	}
	if draft.Status != customAppStatusBuilding {
		t.Fatalf("pre-scaffolded app status = %q, want building", draft.Status)
	}
	if !strings.Contains(created.Task.Details, draft.ID) {
		t.Fatalf("brief id %q not the scaffolded app id %q", created.Task.Details, draft.ID)
	}
}

// TestMutateTaskImproveDoesNotPrescaffold: an "Improve app" task targets an
// existing app and must NOT mint a new draft.
func TestMutateTaskImproveDoesNotPrescaffold(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	b.channels = []teamChannel{
		{Slug: "general", Name: "general", Members: []string{"ceo", appBuilderSlug}},
	}
	created, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     "Improve app: Lead Scorer",
		Owner:     appBuilderSlug,
		CreatedBy: "ceo",
	})
	if err != nil {
		t.Fatalf("MutateTask create: %v", err)
	}
	if strings.Contains(created.Task.Details, appWorkspaceBriefMarker) {
		t.Fatalf("improve task should not get a scaffold brief: %q", created.Task.Details)
	}
	apps, _ := b.appStore().List()
	if len(apps) != 0 {
		t.Fatalf("improve task pre-scaffolded an app: %+v", apps)
	}
}

// TestScaffoldEmbedExcludesBuildArtifacts guards a real footgun: the App
// scaffold is embedded with `//go:embed all:app-scaffold`, which sweeps EVERY
// file on disk under templates/app-scaffold at build time — it does not honor
// .gitignore. A release binary is built from a clean checkout (goreleaser does
// not run `bun install` for the scaffold), so node_modules/dist are absent and
// only the ~dozen source files embed. But if node_modules (now ~127 MB with the
// refine + Mantine stack) or a dist/ build output were ever committed or present
// at build time, the binary would balloon and every scaffolded app would
// materialize a stale node_modules. This test materializes the scaffold from the
// embedded FS and fails if any build-artifact path leaked in, catching a bad
// commit or an embed-directive regression in CI (where the tree is clean).
func TestScaffoldEmbedExcludesBuildArtifacts(t *testing.T) {
	// Walk the EMBEDDED scaffold FS directly — this is what
	// writeScaffoldSourceLocked materializes onto disk, before Source() filters
	// node_modules back out. A leak here means the binary embedded the artifacts
	// and every scaffolded app pays to write them.
	var leaked []string
	err := fs.WalkDir(
		templates.AppScaffold,
		templates.AppScaffoldRoot,
		func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			slashed := filepath.ToSlash(p)
			if strings.Contains(slashed, "/node_modules/") ||
				strings.Contains(slashed, "/dist/") {
				leaked = append(leaked, slashed)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk embedded scaffold: %v", err)
	}
	if len(leaked) > 0 {
		t.Fatalf("scaffold embed includes %d build-artifact file(s); the binary "+
			"would balloon and every scaffolded app would materialize them. Run "+
			"`rm -rf templates/app-scaffold/node_modules templates/app-scaffold/dist` "+
			"before building, and never commit them. First few: %v",
			len(leaked), leaked[:min(5, len(leaked))])
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
