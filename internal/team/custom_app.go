package team

// custom_app.go owns the storage + validation for agent-generated internal
// tools ("Apps"). An App is a small, self-contained single-file web app — the
// built output of a real Vite/React/TS project (inlined via
// vite-plugin-singlefile by the App Builder agent) — that lives under
// <runtime-home>/.wuphf/apps/<id>/ and renders inside a sandboxed iframe.
//
// Why a dedicated store instead of the wiki git worker:
//   - Apps are a distinct concern from the curated wiki; coupling them to the
//     wiki write queue would entangle two unrelated serializers.
//   - v1 versioning is a monotonic counter on the manifest, not git history.
//
// Security model: the rendered iframe is the real boundary (sandbox=
// "allow-scripts" with no allow-same-origin, CSP connect-src 'none'). The
// write-time validator below is defense-in-depth: it mirrors the proven
// rich-artifact sandbox policy (no external script/style/base, no nested
// browsing contexts, no inline event handlers, no off-origin URLs) but ALLOWS
// <form> because a form is inert under the app sandbox and real tools need it.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/templates"
)

const (
	customAppEntry = "index.html"
	// Singlefile React bundles run larger than rich artifacts (the whole app +
	// inlined CSS lives in one document), so the ceiling is higher.
	customAppMaxHTMLBytes = 4 * 1024 * 1024
	customAppDefaultIcon  = "🧩"
	customAppManifestFile = "app.json"
	customAppMaxNameBytes = 120
	// Version snapshots + source live next to the manifest so an edit can roll
	// back to a known-good build, and the App Builder can edit real source
	// instead of regenerating from prose.
	customAppVersionsDir        = "versions"
	customAppVersionMetaFile    = "meta.json"
	customAppSourceDir          = "src"
	customAppMaxSourceFiles     = 300
	customAppMaxSourceFileBytes = 512 * 1024
	// customAppStatusBuilding marks a pre-scaffolded app that has no published
	// build yet — the source exists (so the live dev preview can boot) but the
	// App Builder has not run register_app. A missing/empty status means
	// "ready" (back-compat with manifests written before this field existed).
	customAppStatusBuilding = "building"
	customAppStatusReady    = "ready"
)

// customAppPreservedSrcDirs are top-level entries under src/ that a publish must
// NOT delete: build/install artifacts that are expensive to regenerate and that
// a running dev server depends on. Keeping node_modules across a register_app
// lets the live Vite server hot-reload the freshly published source instead of
// crashing on a vanished dependency tree. They are also skipped when reading
// source back (get_app) so the agent never sees node_modules.
var customAppPreservedSrcDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	".vite":        true,
}

// CustomApp is the durable manifest for an agent-generated internal tool. The
// built HTML bundle lives next to it on disk (Entry) so listings stay cheap.
type CustomApp struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	Entry       string `json:"entry"`
	Version     int    `json:"version"`
	// Status is "building" for a pre-scaffolded app awaiting its first publish,
	// or "ready"/"" for a published app. Lets the sidebar hide drafts while the
	// build task's live preview still resolves them.
	Status string `json:"status,omitempty"`
	// EditChannel is the slug of the app's persistent edit thread — the channel
	// of the App Builder task that created/improves it (`task-<id>`). Binding the
	// app to a stable channel lets the FE mount a per-app "chat to edit" panel:
	// a human note posted there re-engages the App Builder owner (via the
	// existing task_followup wake) to read get_app + republish with register_app.
	// Empty for apps minted before this field existed or registered html-only
	// (no owning task) — those simply have no edit thread until the next build.
	EditChannel string `json:"editChannel,omitempty"`
	CreatedBy   string `json:"createdBy"`
	UpdatedBy   string `json:"updatedBy,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	ContentHash string `json:"contentHash"`
}

// CustomAppWriteRequest is the create/update payload. An empty ID creates a new
// app; a populated ID updates the existing one in place (bumping Version).
type CustomAppWriteRequest struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	HTML        string `json:"html"`
	Actor       string `json:"actor,omitempty"`
	// Files is the app's source project (relative path -> content), persisted
	// under src/ so a later edit modifies real files instead of regenerating
	// from prose. Optional; nil leaves any existing source untouched. Build
	// artifacts (node_modules/, dist/) are rejected.
	Files map[string]string `json:"files,omitempty"`
}

var errCustomAppCaller = errors.New("app: caller error")

type customAppCallerError struct{ err error }

func (e customAppCallerError) Error() string   { return e.err.Error() }
func (e customAppCallerError) Unwrap() []error { return []error{errCustomAppCaller, e.err} }

func newCustomAppCallerError(format string, args ...any) error {
	return customAppCallerError{err: fmt.Errorf(format, args...)}
}

func isCustomAppCallerError(err error) bool { return errors.Is(err, errCustomAppCaller) }

// CustomAppsRootDir returns <runtime-home>/.wuphf/apps, honouring
// config.RuntimeHomeDir so dev runs stay isolated from prod (same discipline as
// WikiRootDir).
func CustomAppsRootDir() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return filepath.Join(".wuphf", "apps")
	}
	return filepath.Join(home, ".wuphf", "apps")
}

// customAppStore is the standalone persistence layer for Apps. All operations
// serialize on mu; reads and writes both lock so a listing never observes a
// half-written manifest.
type customAppStore struct {
	root string
	mu   sync.Mutex
	// buildBundle compiles the persisted source dir into the sealed single-file
	// bundle bytes. Defaults to the real bun-driven buildAppBundle; tests inject a
	// hermetic stub so they need neither bun nor the network. Always set —
	// newCustomAppStore wires the default.
	buildBundle func(srcDir string) ([]byte, error)
	// publishMu serializes concurrent publishes OF THE SAME app id. The server-side
	// build runs WITHOUT the store-wide mu (so reads/listings aren't starved for
	// the multi-second build), so this per-app gate is what stops two builds from
	// racing in the same src/ dir. Keyed by app id; entries are created on demand.
	publishMu sync.Map // map[string]*sync.Mutex
}

func newCustomAppStore(root string) *customAppStore {
	return &customAppStore{root: root, buildBundle: buildAppBundle}
}

// publishLock returns the per-app publish mutex, creating it on first use.
func (s *customAppStore) publishLock(id string) *sync.Mutex {
	m, _ := s.publishMu.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func validateCustomAppID(id string) error {
	id = strings.TrimSpace(id)
	if len(id) != len("app_")+16 || !strings.HasPrefix(id, "app_") {
		return newCustomAppCallerError("app: invalid id %q", id)
	}
	for _, ch := range strings.TrimPrefix(id, "app_") {
		if !((ch >= 'a' && ch <= 'f') || (ch >= '0' && ch <= '9')) {
			return newCustomAppCallerError("app: invalid id %q", id)
		}
	}
	return nil
}

func customAppID(slug, name, createdAt string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{slug, name, createdAt}, "\x00")))
	return "app_" + hex.EncodeToString(sum[:])[:16]
}

func customAppContentHash(htmlBody string) string {
	sum := sha256.Sum256([]byte(htmlBody))
	return hex.EncodeToString(sum[:])
}

func (s *customAppStore) appDir(id string) string {
	return filepath.Join(s.root, id)
}

// List returns all apps, most-recently-updated first.
func (s *customAppStore) List() ([]CustomApp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return []CustomApp{}, nil
		}
		return nil, fmt.Errorf("app: read registry: %w", err)
	}
	out := make([]CustomApp, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Only iterate well-formed app ids so a stray/foreign directory name can
		// never be joined onto the apps root (defense in depth — os.ReadDir never
		// returns ".."/absolute names, but the id shape is the contract).
		if err := validateCustomAppID(entry.Name()); err != nil {
			continue
		}
		app, err := s.readManifestLocked(entry.Name())
		if err != nil {
			continue // skip unreadable/foreign dirs rather than fail the whole list
		}
		out = append(out, app)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// Get returns the manifest plus the built HTML bundle for an app.
func (s *customAppStore) Get(id string) (CustomApp, string, error) {
	if err := validateCustomAppID(id); err != nil {
		return CustomApp{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	app, err := s.readManifestLocked(id)
	if err != nil {
		return CustomApp{}, "", err
	}
	body, err := os.ReadFile(filepath.Join(s.appDir(id), customAppEntry))
	if err != nil {
		return CustomApp{}, "", fmt.Errorf("app: read entry: %w", err)
	}
	return app, string(body), nil
}

func (s *customAppStore) readManifestLocked(id string) (CustomApp, error) {
	raw, err := os.ReadFile(filepath.Join(s.appDir(id), customAppManifestFile))
	if err != nil {
		return CustomApp{}, fmt.Errorf("app: read manifest: %w", err)
	}
	var app CustomApp
	if err := json.Unmarshal(raw, &app); err != nil {
		return CustomApp{}, fmt.Errorf("app: decode manifest: %w", err)
	}
	if app.ID != id {
		return CustomApp{}, fmt.Errorf("app: manifest id mismatch")
	}
	return app, nil
}

// Save creates a new app (empty req.ID) or updates an existing one.
//
// When the request carries source Files (the normal App Builder path), the HOST
// owns the bundle: it overwrites the protected host-contract files with their
// canonical embedded bytes, writes the source, builds it server-side
// (`bun install` + `bun run build`), and stores the BROKER-built
// dist/index.html — the agent-submitted html is ignored, so a generated app can
// never ship a tampered bridge or an unverified bundle. A build failure does NOT
// publish; it returns a caller error carrying the build output tail.
//
// When there are no source Files (an html-only registration, e.g. a built-in or
// simple app), it falls back to the submitted html as before.
func (s *customAppStore) Save(req CustomAppWriteRequest, now time.Time) (CustomApp, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return CustomApp{}, newCustomAppCallerError("app: name is required")
	}
	if len(name) > customAppMaxNameBytes {
		return CustomApp{}, newCustomAppCallerError("app: name exceeds %d bytes", customAppMaxNameBytes)
	}
	if strings.ContainsRune(name, '\x00') {
		return CustomApp{}, newCustomAppCallerError("app: name must not contain NUL bytes")
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = "app-builder"
	}
	icon := strings.TrimSpace(req.Icon)
	if icon == "" {
		icon = customAppDefaultIcon
	}
	stamp := now.UTC().Format(time.RFC3339Nano)

	// Phase 1 (store lock): resolve the target manifest + ensure the dir. Held
	// briefly — the multi-second build does NOT run under this lock, so listings
	// and reads are never starved by a publish.
	s.mu.Lock()
	app, err := s.resolveSaveManifestLocked(req, name, actor, icon, stamp)
	if err != nil {
		s.mu.Unlock()
		return CustomApp{}, err
	}
	dir := s.appDir(app.ID)
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		s.mu.Unlock()
		return CustomApp{}, fmt.Errorf("app: mkdir: %w", mkErr)
	}
	s.mu.Unlock()

	// Serialize concurrent publishes of the SAME app so two builds don't race in
	// one src/ dir. Different apps publish in parallel.
	pl := s.publishLock(app.ID)
	pl.Lock()
	defer pl.Unlock()

	// Phase 2 (no store lock): produce the html to publish. With source Files this
	// stages the source, overwrites the protected host-contract files with
	// canonical bytes, and builds server-side — restoring the prior source on a
	// build failure so a deliberately-broken publish can't leave tampered source
	// running in the live preview. Without Files it returns the submitted html.
	htmlBody, err := s.resolvePublishHTML(dir, req)
	if err != nil {
		return CustomApp{}, err
	}
	if err := validateCustomAppHTML(htmlBody); err != nil {
		return CustomApp{}, err
	}
	app.ContentHash = customAppContentHash(htmlBody)

	// Phase 3 (store lock): commit the published bytes + manifest + version
	// snapshot atomically with respect to other store operations.
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeFileAtomic(filepath.Join(dir, customAppEntry), []byte(htmlBody), 0o600); err != nil {
		return CustomApp{}, fmt.Errorf("app: write entry: %w", err)
	}
	manifestBytes, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return CustomApp{}, fmt.Errorf("app: marshal manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeFileAtomic(filepath.Join(dir, customAppManifestFile), manifestBytes, 0o600); err != nil {
		return CustomApp{}, fmt.Errorf("app: write manifest: %w", err)
	}
	// Retain this version's built bytes (append-only history) so a later edit
	// can roll back to a known-good build. Without this the version counter is
	// a false affordance — it promises an undo that cannot happen.
	if err := s.snapshotVersionLocked(dir, app, htmlBody); err != nil {
		return CustomApp{}, err
	}
	return app, nil
}

// resolveSaveManifestLocked builds the target manifest for a Save: an update
// reads + bumps the existing one, a create mints a fresh one. Must hold s.mu.
func (s *customAppStore) resolveSaveManifestLocked(req CustomAppWriteRequest, name, actor, icon, stamp string) (CustomApp, error) {
	var app CustomApp
	if id := strings.TrimSpace(req.ID); id != "" {
		if err := validateCustomAppID(id); err != nil {
			return CustomApp{}, err
		}
		existing, err := s.readManifestLocked(id)
		if err != nil {
			return CustomApp{}, newCustomAppCallerError("app: %s not found", id)
		}
		app = existing
		app.Name = name
		app.Icon = icon
		app.Summary = strings.TrimSpace(req.Summary)
		if desc := strings.TrimSpace(req.Description); desc != "" {
			app.Description = desc
		}
		app.Version = existing.Version + 1
		app.UpdatedBy = actor
		app.UpdatedAt = stamp
	} else {
		slug := slugifyNotebookEntry(name)
		if slug == "" {
			slug = "app"
		}
		app = CustomApp{
			ID:          customAppID(slug, name, stamp),
			Slug:        slug,
			Name:        name,
			Icon:        icon,
			Summary:     strings.TrimSpace(req.Summary),
			Description: strings.TrimSpace(req.Description),
			Entry:       customAppEntry,
			Version:     1,
			CreatedBy:   actor,
			UpdatedBy:   actor,
			CreatedAt:   stamp,
			UpdatedAt:   stamp,
		}
	}
	app.Entry = customAppEntry
	// A register_app is always a real published build, so it clears the
	// "building" draft status a pre-scaffolded app carries.
	app.Status = customAppStatusReady
	return app, nil
}

// resolvePublishHTML produces the bytes to store as the app's html. When req
// carries source Files it is the HOST-built bundle: protected host-contract files
// are overwritten with canonical embedded bytes, the source is persisted, and
// `bun install` + `bun run build` produces dist/index.html — the agent's req.HTML
// is discarded. Without Files it returns req.HTML unchanged (html-only fallback).
//
// It is called WITHOUT the store mutex (so the build never starves reads) but
// UNDER the per-app publish lock (so the src/ dir has a single writer). On a
// build failure it RESTORES the prior source, so a deliberately-broken publish
// cannot leave tampered source running in the live dev preview.
func (s *customAppStore) resolvePublishHTML(dir string, req CustomAppWriteRequest) (string, error) {
	if len(req.Files) == 0 {
		// html-only registration: no source project, trust the submitted html
		// (the sandbox policy still gates it in the caller).
		return req.HTML, nil
	}
	// Deterministic App Builder harness, BEFORE staging/building: reject a publish
	// that (a) re-runs work on tab focus or polls tighter than the floor, or
	// (b) abandons the fixed Mantine kit. AI_RULES advises these; this ENFORCES
	// them so a token-burner or off-stack app can never reach the sealed bundle.
	// The agent reads the file:line list and republishes.
	violations := checkAppSourceEfficiency(req.Files)
	violations = append(violations, checkAppStackConformance(req.Files)...)
	violations = append(violations, checkAppThemeDepth(req.Files)...)
	violations = append(violations, checkAppCardPile(req.Files)...)
	if len(violations) > 0 {
		return "", appEfficiencyGuardError(violations)
	}
	// The host owns the contract: discard the agent's protected files and replace
	// them with the canonical embedded versions before anything is persisted or
	// built.
	files, err := overwriteProtectedFiles(req.Files)
	if err != nil {
		return "", err
	}

	srcRoot := filepath.Join(dir, customAppSourceDir)
	// Snapshot the prior app-source so a failed build rolls back to it (the live
	// preview keeps running the last good source, not the tampered one).
	restore, cleanup, err := snapshotAppSource(srcRoot)
	if err != nil {
		return "", err
	}
	defer cleanup()

	if err := s.writeAppSource(srcRoot, files); err != nil {
		return "", err
	}

	build := s.buildBundle
	if build == nil {
		build = buildAppBundle
	}
	built, buildErr := build(srcRoot)
	if buildErr != nil {
		// Roll the source back to the last good state before surfacing the error.
		// Wrap both so the build failure stays a caller error (4xx) while the
		// restore failure rides along in the chain.
		if rerr := restore(); rerr != nil {
			return "", fmt.Errorf("%w (and restoring prior source failed: %w)", buildErr, rerr)
		}
		return "", buildErr
	}
	return string(built), nil
}

// scaffoldPlaceholderHTML is the sealed entry written for a not-yet-built app so
// the sealed view (and get_app) has a valid document before the first publish.
// The live preview never uses it — it boots the dev server on the source below.
const scaffoldPlaceholderHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width, initial-scale=1">` +
	`<title>Building…</title></head><body style="font:14px system-ui;padding:2rem;color:#555">` +
	`<p>This app is being built. The live preview shows progress as it is created.</p>` +
	`</body></html>`

// SetEditChannel stamps the app's persistent edit-thread channel onto its
// manifest, idempotently. It is the one mutation the broker performs on an app
// it did NOT just build: when an App Builder task mints its `task-<id>` channel,
// the broker records that slug here so the FE can later bind the per-app edit
// chat to it. A no-op when the channel is already set to the same value (a
// retried create) so it never churns the manifest or bumps anything.
//
// Unknown id → caller error (404 upstream). Never touches Version/Status/bytes.
func (s *customAppStore) SetEditChannel(id, channel string) error {
	if err := validateCustomAppID(id); err != nil {
		return err
	}
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return newCustomAppCallerError("app: edit channel is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	app, err := s.readManifestLocked(id)
	if err != nil {
		return newCustomAppCallerError("app: %s not found", id)
	}
	if app.EditChannel == channel {
		return nil
	}
	app.EditChannel = channel
	manifestBytes, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return fmt.Errorf("app: marshal manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeFileAtomic(filepath.Join(s.appDir(id), customAppManifestFile), manifestBytes, 0o600); err != nil {
		return fmt.Errorf("app: write manifest: %w", err)
	}
	return nil
}

// Scaffold materializes a brand-new app's editable source from the embedded
// starter template and records a "building" draft manifest, BEFORE the App
// Builder writes a single line of code. The live preview can then boot a real
// dev server on this source in seconds — turning the old multi-minute
// "Building…" dead air into an instant, running scaffold the human watches the
// agent shape. The agent publishes the finished build with register_app(app_id)
// using this same id, which flips the draft to a ready, listed app.
//
// Scaffold is idempotent: if the id already exists (draft or published) it
// returns the existing manifest untouched, so a retried/duplicate task create
// never clobbers in-flight work.
func (s *customAppStore) Scaffold(id, name, icon, actor string, now time.Time) (CustomApp, error) {
	if err := validateCustomAppID(id); err != nil {
		return CustomApp{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return CustomApp{}, newCustomAppCallerError("app: name is required")
	}
	if len(name) > customAppMaxNameBytes {
		return CustomApp{}, newCustomAppCallerError("app: name exceeds %d bytes", customAppMaxNameBytes)
	}
	if strings.ContainsRune(name, '\x00') {
		return CustomApp{}, newCustomAppCallerError("app: name must not contain NUL bytes")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "app-builder"
	}
	icon = strings.TrimSpace(icon)
	if icon == "" {
		icon = customAppDefaultIcon
	}
	slug := slugifyNotebookEntry(name)
	if slug == "" {
		slug = "app"
	}
	stamp := now.UTC().Format(time.RFC3339Nano)

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, err := s.readManifestLocked(id); err == nil {
		return existing, nil
	}

	app := CustomApp{
		ID:        id,
		Slug:      slug,
		Name:      name,
		Icon:      icon,
		Entry:     customAppEntry,
		Version:   0,
		Status:    customAppStatusBuilding,
		CreatedBy: actor,
		UpdatedBy: actor,
		CreatedAt: stamp,
		UpdatedAt: stamp,
	}
	dir := s.appDir(app.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CustomApp{}, fmt.Errorf("app: mkdir: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, customAppEntry), []byte(scaffoldPlaceholderHTML), 0o600); err != nil {
		return CustomApp{}, fmt.Errorf("app: write placeholder: %w", err)
	}
	if err := writeScaffoldSourceLocked(filepath.Join(dir, customAppSourceDir)); err != nil {
		return CustomApp{}, err
	}
	manifestBytes, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		return CustomApp{}, fmt.Errorf("app: marshal manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeFileAtomic(filepath.Join(dir, customAppManifestFile), manifestBytes, 0o600); err != nil {
		return CustomApp{}, fmt.Errorf("app: write manifest: %w", err)
	}
	return app, nil
}

// writeScaffoldSourceLocked copies the embedded starter template into srcRoot,
// stripping the "app-scaffold/" prefix so package.json/vite.config/index.html
// land at the project root (srcRoot) and the app's own code under srcRoot/src.
func writeScaffoldSourceLocked(srcRoot string) error {
	return fs.WalkDir(templates.AppScaffold, templates.AppScaffoldRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(templates.AppScaffoldRoot, p)
		if err != nil {
			return err
		}
		body, err := templates.AppScaffold.ReadFile(p)
		if err != nil {
			return err
		}
		full := filepath.Join(srcRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return fmt.Errorf("app: mkdir scaffold dir: %w", err)
		}
		if err := writeFileAtomic(full, body, 0o600); err != nil {
			return fmt.Errorf("app: write scaffold file %q: %w", rel, err)
		}
		return nil
	})
}

func (s *customAppStore) snapshotVersionLocked(dir string, app CustomApp, htmlBody string) error {
	if app.Version < 1 {
		return nil
	}
	vdir := filepath.Join(dir, customAppVersionsDir, fmt.Sprintf("v%d", app.Version))
	if err := os.MkdirAll(vdir, 0o700); err != nil {
		return fmt.Errorf("app: mkdir version: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(vdir, customAppEntry), []byte(htmlBody), 0o600); err != nil {
		return fmt.Errorf("app: write version snapshot: %w", err)
	}
	// Capture who/when beside the bytes so the history timeline can label each
	// build. Versions snapshotted before this file existed degrade gracefully to
	// a bare version number (readVersionMetaLocked returns ok=false).
	meta := customAppVersionMeta{Version: app.Version, UpdatedAt: app.UpdatedAt, UpdatedBy: app.UpdatedBy}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("app: marshal version meta: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(vdir, customAppVersionMetaFile), metaBytes, 0o600); err != nil {
		return fmt.Errorf("app: write version meta: %w", err)
	}
	return nil
}

// writeAppSource replaces src/ with the provided files (so deletes propagate).
// Each path is sanitized against traversal and build-tool config injection;
// build artifacts are rejected. A nil/empty map leaves any existing source
// untouched. Runs under the per-app publish lock (single writer of srcRoot), not
// the store mutex.
func (s *customAppStore) writeAppSource(srcRoot string, files map[string]string) error {
	if len(files) == 0 {
		return nil
	}
	if len(files) > customAppMaxSourceFiles {
		return newCustomAppCallerError("app: too many source files (%d > %d)", len(files), customAppMaxSourceFiles)
	}
	if err := clearSourceExceptArtifacts(srcRoot); err != nil {
		return fmt.Errorf("app: clear source: %w", err)
	}
	for rel, content := range files {
		clean, err := sanitizeAppSourcePath(rel)
		if err != nil {
			return err
		}
		if len(content) > customAppMaxSourceFileBytes {
			return newCustomAppCallerError("app: source file %q exceeds %d bytes", rel, customAppMaxSourceFileBytes)
		}
		full := filepath.Join(srcRoot, clean)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return fmt.Errorf("app: mkdir source dir: %w", err)
		}
		if err := writeFileAtomic(full, []byte(content), 0o600); err != nil {
			return fmt.Errorf("app: write source: %w", err)
		}
	}
	return nil
}

// clearSourceExceptArtifacts removes every top-level entry under src/
// EXCEPT the preserved build/install artifacts (node_modules/, dist/, .vite/).
// Replacing the source this way (instead of os.RemoveAll on the whole tree)
// lets a publish land new source while a live dev server keeps running on the
// same node_modules — Vite then hot-reloads the change rather than crashing on
// a deleted dependency tree. Source deletes still propagate because every
// non-preserved entry (including the app's own nested src/ dir) is removed and
// rewritten from the new file set.
func clearSourceExceptArtifacts(srcRoot string) error {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if customAppPreservedSrcDirs[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(srcRoot, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// blockedAppSourceBasenames are build-tool config files an app must never carry.
// They change the SERVER-SIDE build environment, not the app: .npmrc/.bunfig.toml
// redirect the registry the host's `bun install` resolves from (a supply-chain
// vector), and .env* files are read by Vite and inlined into the bundle as
// import.meta.env.VITE_*. The host owns the build config; the agent ships app
// source only.
var blockedAppSourceBasenames = map[string]bool{
	".npmrc":           true,
	".bunfig.toml":     true,
	".yarnrc":          true,
	".yarnrc.yml":      true,
	".env":             true,
	".env.local":       true,
	".env.development": true,
	".env.production":  true,
}

// sanitizeAppSourcePath returns a cleaned relative path under src/, or a caller
// error if it would escape the app dir, names a build artifact, or names a
// build-tool config file that would tamper with the server-side build.
func sanitizeAppSourcePath(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" {
		return "", newCustomAppCallerError("app: empty source path")
	}
	if strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") || strings.ContainsRune(rel, '\x00') {
		return "", newCustomAppCallerError("app: invalid source path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(filepath.ToSlash(clean), "../") {
		return "", newCustomAppCallerError("app: invalid source path %q", rel)
	}
	switch first := strings.SplitN(filepath.ToSlash(clean), "/", 2)[0]; first {
	case "node_modules", "dist", ".vite":
		return "", newCustomAppCallerError("app: source path %q is a build artifact; exclude node_modules, dist, and .vite", rel)
	}
	// Reject build-tool config by basename ANYWHERE in the tree — an .npmrc nested
	// under a subdir still affects the install run from the project root.
	base := strings.ToLower(filepath.Base(clean))
	if blockedAppSourceBasenames[base] || strings.HasPrefix(base, ".env.") {
		return "", newCustomAppCallerError("app: source path %q is a build-tool config; the host owns the build environment", rel)
	}
	return clean, nil
}

// Source returns the persisted source project (relative path -> content). Empty
// when an app has no source (html-only). Used by the App Builder via get_app.
func (s *customAppStore) Source(id string) (map[string]string, error) {
	if err := validateCustomAppID(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	srcRoot := filepath.Join(s.appDir(id), customAppSourceDir)
	out := map[string]string{}
	err := filepath.WalkDir(srcRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			// Never read back preserved build/install artifacts as "source" —
			// node_modules would be thousands of files and is not the app.
			if p != srcRoot && customAppPreservedSrcDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(srcRoot, p)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("app: read source: %w", err)
	}
	return out, nil
}

// CustomAppVersion is one retained build in an app's append-only history,
// surfaced by the version timeline. Metadata (who/when) is captured at snapshot
// time; builds snapshotted before that existed degrade to just the version
// number. Current marks the app's live build.
type CustomAppVersion struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	UpdatedBy string `json:"updatedBy,omitempty"`
	Current   bool   `json:"current"`
}

// customAppVersionMeta is the on-disk metadata stored beside each retained build
// (versions/v<N>/meta.json). Current is intentionally NOT persisted — it is
// derived against the manifest at read time so it can never go stale.
type customAppVersionMeta struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

// ListVersions returns the retained versions, newest first, each annotated with
// its capture metadata and whether it is the current build.
func (s *customAppStore) ListVersions(id string) ([]CustomAppVersion, error) {
	if err := validateCustomAppID(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Best-effort current version: a missing manifest just means nothing is
	// marked current (the versions dir read below still drives the list), so an
	// unknown/corrupt app degrades to an empty list rather than an error.
	var current int
	if app, err := s.readManifestLocked(id); err == nil {
		current = app.Version
	}
	dir := filepath.Join(s.appDir(id), customAppVersionsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []CustomAppVersion{}, nil
		}
		return nil, fmt.Errorf("app: read versions: %w", err)
	}
	out := []CustomAppVersion{}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "v") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "v"))
		if err != nil || n < 1 {
			continue
		}
		ver := CustomAppVersion{Version: n, Current: n == current}
		if meta, ok := s.readVersionMetaLocked(dir, n); ok {
			ver.UpdatedAt = meta.UpdatedAt
			ver.UpdatedBy = meta.UpdatedBy
		}
		out = append(out, ver)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// GetVersion returns a retained build's bytes plus its metadata WITHOUT changing
// the current version — the non-destructive read behind the timeline's preview.
// Restoring is the separate, explicit Rollback.
func (s *customAppStore) GetVersion(id string, version int) (CustomAppVersion, string, error) {
	if err := validateCustomAppID(id); err != nil {
		return CustomAppVersion{}, "", err
	}
	if version < 1 {
		return CustomAppVersion{}, "", newCustomAppCallerError("app: invalid version %d", version)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	versionsDir := filepath.Join(s.appDir(id), customAppVersionsDir)
	body, err := os.ReadFile(filepath.Join(versionsDir, fmt.Sprintf("v%d", version), customAppEntry))
	if err != nil {
		if os.IsNotExist(err) {
			return CustomAppVersion{}, "", newCustomAppCallerError("app: version v%d not found", version)
		}
		return CustomAppVersion{}, "", fmt.Errorf("app: read version: %w", err)
	}
	ver := CustomAppVersion{Version: version}
	if app, err := s.readManifestLocked(id); err == nil {
		ver.Current = version == app.Version
	}
	if meta, ok := s.readVersionMetaLocked(versionsDir, version); ok {
		ver.UpdatedAt = meta.UpdatedAt
		ver.UpdatedBy = meta.UpdatedBy
	}
	return ver, string(body), nil
}

// readVersionMetaLocked reads versions/v<N>/meta.json. ok=false (not an error)
// when the file is absent or unparseable, so legacy snapshots degrade quietly.
func (s *customAppStore) readVersionMetaLocked(versionsDir string, version int) (customAppVersionMeta, bool) {
	raw, err := os.ReadFile(filepath.Join(versionsDir, fmt.Sprintf("v%d", version), customAppVersionMetaFile))
	if err != nil {
		return customAppVersionMeta{}, false
	}
	var meta customAppVersionMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return customAppVersionMeta{}, false
	}
	return meta, true
}

// Rollback restores a prior version's built bytes as a NEW forward version.
// History stays append-only, so a rollback is itself reversible.
func (s *customAppStore) Rollback(id string, version int, actor string, now time.Time) (CustomApp, error) {
	if err := validateCustomAppID(id); err != nil {
		return CustomApp{}, err
	}
	if version < 1 {
		return CustomApp{}, newCustomAppCallerError("app: invalid version %d", version)
	}
	s.mu.Lock()
	app, err := s.readManifestLocked(id)
	if err != nil {
		s.mu.Unlock()
		return CustomApp{}, newCustomAppCallerError("app: %s not found", id)
	}
	if version == app.Version {
		s.mu.Unlock()
		return CustomApp{}, newCustomAppCallerError("app: v%d is already current", version)
	}
	// Compute the snapshot path under the lock, then read it WITHOUT the lock —
	// the bundle can be up to customAppMaxHTMLBytes, and holding s.mu across the
	// read would block every concurrent List/Get/Source (Save uses the same
	// lock-free-I/O discipline).
	snap := filepath.Join(s.appDir(id), customAppVersionsDir, fmt.Sprintf("v%d", version), customAppEntry)
	s.mu.Unlock()
	body, readErr := os.ReadFile(snap)
	if readErr != nil {
		return CustomApp{}, newCustomAppCallerError("app: version v%d not found", version)
	}
	// Save() re-locks and snapshots the restored bytes as a new version.
	return s.Save(CustomAppWriteRequest{
		ID:          id,
		Name:        app.Name,
		Icon:        app.Icon,
		Summary:     app.Summary,
		Description: app.Description,
		HTML:        string(body),
		Actor:       actor,
	}, now)
}

// validateCustomAppHTML enforces the app sandbox policy at write time. It is
// intentionally close to validateRichArtifactSandboxPolicy but permits <form>.
func validateCustomAppHTML(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return newCustomAppCallerError("app: html is required")
	}
	if len([]byte(raw)) > customAppMaxHTMLBytes {
		return newCustomAppCallerError("app: html exceeds %d bytes", customAppMaxHTMLBytes)
	}
	if strings.ContainsRune(raw, '\x00') {
		return newCustomAppCallerError("app: html must not contain NUL bytes")
	}
	return validateSandboxHTML(raw, sandboxHTMLPolicy{
		label:          "app",
		blockedElement: customAppBlockedElementReason,
		newErr:         newCustomAppCallerError,
	})
}

func customAppBlockedElementReason(tag string) (string, bool) {
	switch tag {
	case "base":
		return "base URLs can rewrite link targets inside the sandbox", true
	case "embed", "iframe", "object":
		return "nested browsing contexts and plugins are not part of the app sandbox", true
	case "link":
		return "external stylesheets and preloads are not allowed; inline your styles", true
	default:
		// NB: <form> is intentionally allowed — it is inert under the app
		// sandbox (no allow-forms / allow-same-origin) and real tools use it.
		return "", false
	}
}
