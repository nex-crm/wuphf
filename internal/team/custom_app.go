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
	customAppSourceDir          = "src"
	customAppMaxSourceFiles     = 300
	customAppMaxSourceFileBytes = 512 * 1024
)

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
}

func newCustomAppStore(root string) *customAppStore {
	return &customAppStore{root: root}
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

// Save creates a new app (empty req.ID) or updates an existing one. It
// validates the HTML against the app sandbox policy, writes the manifest +
// entry bundle, and returns the stored manifest.
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
	htmlBody := req.HTML
	if err := validateCustomAppHTML(htmlBody); err != nil {
		return CustomApp{}, err
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

	s.mu.Lock()
	defer s.mu.Unlock()

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
		app.ContentHash = customAppContentHash(htmlBody)
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
			ContentHash: customAppContentHash(htmlBody),
		}
	}
	app.Entry = customAppEntry

	dir := s.appDir(app.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CustomApp{}, fmt.Errorf("app: mkdir: %w", err)
	}
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
	if err := s.snapshotVersionLocked(dir, app.Version, htmlBody); err != nil {
		return CustomApp{}, err
	}
	// Persist the source project when provided so edits modify real files.
	if err := s.writeAppSourceLocked(dir, req.Files); err != nil {
		return CustomApp{}, err
	}
	return app, nil
}

func (s *customAppStore) snapshotVersionLocked(dir string, version int, htmlBody string) error {
	if version < 1 {
		return nil
	}
	vdir := filepath.Join(dir, customAppVersionsDir, fmt.Sprintf("v%d", version))
	if err := os.MkdirAll(vdir, 0o700); err != nil {
		return fmt.Errorf("app: mkdir version: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(vdir, customAppEntry), []byte(htmlBody), 0o600); err != nil {
		return fmt.Errorf("app: write version snapshot: %w", err)
	}
	return nil
}

// writeAppSourceLocked replaces src/ with the provided files (so deletes
// propagate). Each path is sanitized against traversal; build artifacts are
// rejected. A nil/empty map leaves any existing source untouched.
func (s *customAppStore) writeAppSourceLocked(dir string, files map[string]string) error {
	if len(files) == 0 {
		return nil
	}
	if len(files) > customAppMaxSourceFiles {
		return newCustomAppCallerError("app: too many source files (%d > %d)", len(files), customAppMaxSourceFiles)
	}
	srcRoot := filepath.Join(dir, customAppSourceDir)
	if err := os.RemoveAll(srcRoot); err != nil {
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

// sanitizeAppSourcePath returns a cleaned relative path under src/, or a caller
// error if it would escape the app dir or names a build artifact.
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
	case "node_modules", "dist":
		return "", newCustomAppCallerError("app: source path %q is a build artifact; exclude node_modules and dist", rel)
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

// ListVersions returns the retained version numbers, newest first.
func (s *customAppStore) ListVersions(id string) ([]int, error) {
	if err := validateCustomAppID(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.appDir(id), customAppVersionsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []int{}, nil
		}
		return nil, fmt.Errorf("app: read versions: %w", err)
	}
	out := []int{}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "v") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "v"))
		if err != nil || n < 1 {
			continue
		}
		out = append(out, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	return out, nil
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
	snap := filepath.Join(s.appDir(id), customAppVersionsDir, fmt.Sprintf("v%d", version), customAppEntry)
	body, readErr := os.ReadFile(snap)
	s.mu.Unlock()
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
