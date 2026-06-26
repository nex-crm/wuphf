package team

// broker_apps.go owns the HTTP surface for agent-generated internal tools
// ("Apps"). Routes are reached only via the /api proxy (ServeWebUI strips /api
// and forwards to this mux), so they never collide with the SPA's client-side
// /apps/<id> route — the browser navigation hits the SPA fallback, the data
// fetch hits these handlers.
//
//	GET    /apps          -> { apps: [...] }            (sidebar listing)
//	POST   /apps          -> { app }                    (App Builder register/update)
//	GET    /apps/{id}      -> { app, html }             (render in sandboxed iframe)
//	DELETE /apps/{id}      -> { ok: true }              (remove an app)

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// appStore lazily constructs the custom-app store on first use so we avoid
// constructor surgery on NewBroker. Safe for concurrent callers via sync.Once.
func (b *Broker) appStore() *customAppStore {
	b.customAppOnce.Do(func() {
		b.customApps = newCustomAppStore(CustomAppsRootDir())
	})
	return b.customApps
}

func (b *Broker) handleApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		apps, err := b.appStore().List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
	case http.MethodPost:
		var body CustomAppWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		// Resolve the writer the same way rich artifacts do: prefer the
		// authenticated agent slug, fall back to the human session identity.
		actor, status, err := richArtifactAuthenticatedSlug(r, body.Actor, "actor")
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		// Only the App Builder (the lone agent with register_app) or a human
		// session may write app bytes. A random agent holding the broker token
		// must not register apps directly and bypass the build path.
		if !b.appWriterAllowed(r, actor) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the App Builder may register apps"})
			return
		}
		body.Actor = actor
		app, err := b.appStore().Save(body, time.Now())
		if err != nil {
			writeAppError(w, err)
			return
		}
		// A republish rewrites the source and may change the dependency set, so
		// any running live-preview server is now stale. Stop it and pre-warm a
		// fresh one in the background: the new deps (e.g. the refine stack)
		// install off the request path, so the Live tab is ready instead of a
		// blank cold boot when the human opens it.
		mgr := b.appDevManager()
		mgr.Stop(app.ID)
		go func(id string) { _, _ = mgr.Ensure(id) }(app.ID)
		writeJSON(w, http.StatusOK, map[string]any{"app": app})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleAppByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/apps/"), "/")
	parts := strings.Split(rest, "/")
	id := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}
	if err := validateCustomAppID(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	switch sub {
	case "":
		b.handleAppRoot(w, r, id)
	case "versions":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// /apps/{id}/versions/{n} reads one retained build for non-destructive
		// preview; /apps/{id}/versions lists them.
		if len(parts) > 2 {
			b.handleAppVersion(w, r, id, parts[2])
			return
		}
		versions, err := b.appStore().ListVersions(id)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
	case "rollback":
		b.handleAppRollback(w, r, id)
	case "edit-session":
		b.handleAppEditSession(w, r, id)
	case "dev":
		b.handleAppDev(w, r, id, parts)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

// appDevManager lazily constructs the live-preview dev-server manager on first
// use (mirrors appStore). It shares the same custom-app store.
func (b *Broker) appDevManager() *appDevManager {
	b.appDevOnce.Do(func() {
		b.appDev = newAppDevManager(b.appStore())
	})
	return b.appDev
}

// handleAppDev is the CONTROL plane for live previews: ensure/status/stop. The
// preview content itself is served by a per-app reverse proxy on its own
// ephemeral 127.0.0.1 port (see custom_app_dev.go); these endpoints just manage
// its lifecycle and hand the FE the proxy origin to load.
//
//	GET  /apps/{id}/dev         -> ensure running, returns {ready,url,boot_log,error}
//	GET  /apps/{id}/dev/status  -> current status without (re)starting
//	POST /apps/{id}/dev/stop    -> tear down (App Builder / human only)
func (b *Broker) handleAppDev(w http.ResponseWriter, r *http.Request, id string, parts []string) {
	action := ""
	if len(parts) > 2 {
		action = parts[2]
	}
	mgr := b.appDevManager()
	switch {
	case r.Method == http.MethodGet && action == "":
		st, err := mgr.Ensure(id)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	case r.Method == http.MethodGet && action == "status":
		st, ok := mgr.Status(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no preview running"})
			return
		}
		writeJSON(w, http.StatusOK, st)
	case r.Method == http.MethodPost && action == "stop":
		actor, status, err := richArtifactAuthenticatedSlug(r, "", "actor")
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		if !b.appWriterAllowed(r, actor) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a human or the App Builder may stop a preview"})
			return
		}
		mgr.Stop(id)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleAppRoot(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		app, htmlBody, err := b.appStore().Get(id)
		if err != nil {
			writeAppError(w, err)
			return
		}
		out := map[string]any{"app": app, "html": htmlBody}
		// ?source=1 includes the app's source project — only the App Builder
		// needs it (to edit), so the FE view never asks for it. Alongside the raw
		// source we attach a deterministic capability summary (data model, APIs,
		// office writes, UI) so the agent edits from the app's REAL shape instead
		// of guessing or inventing capabilities it lacks.
		if r.URL.Query().Get("source") == "1" {
			source, err := b.appStore().Source(id)
			if err != nil {
				writeAppError(w, err)
				return
			}
			out["source"] = source
			out["capabilities"] = introspectAppSource(source)
		}
		writeJSON(w, http.StatusOK, out)
	case http.MethodDelete:
		actor, _, _ := richArtifactAuthenticatedSlug(r, "", "actor")
		if !b.appWriterAllowed(r, actor) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a human or the App Builder may delete apps"})
			return
		}
		if err := b.appStore().Delete(id); err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAppVersion serves GET /apps/{id}/versions/{n}: one retained build's
// bytes + metadata for non-destructive preview. It NEVER changes the current
// version — restoring is the separate POST /apps/{id}/rollback. The bytes were
// validated at write time and render in the same sandboxed frame as the sealed
// current view, so this adds no new rendering surface.
func (b *Broker) handleAppVersion(w http.ResponseWriter, r *http.Request, id, raw string) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version"})
		return
	}
	ver, htmlBody, err := b.appStore().GetVersion(id, n)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":   ver.Version,
		"updatedAt": ver.UpdatedAt,
		"updatedBy": ver.UpdatedBy,
		"current":   ver.Current,
		"html":      htmlBody,
	})
}

func (b *Broker) handleAppRollback(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Version int    `json:"version"`
		Actor   string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	actor, status, err := richArtifactAuthenticatedSlug(r, body.Actor, "actor")
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if !b.appWriterAllowed(r, actor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a human or the App Builder may roll back apps"})
		return
	}
	app, err := b.appStore().Rollback(id, body.Version, actor, time.Now())
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app": app})
}

// handleAppEditSession opens (or returns) the app's persistent edit thread —
// the per-app "chat to edit" channel:
//
//	POST /apps/{id}/edit-session -> { channel }
//
// Every app should be editable, but an app minted before edit-channel stamping
// (or registered html-only, with no backing task) carries no channel, so the FE
// could not surface Edit for it. This lazily mints one: it creates an App
// Builder "Edit app: <name>" task, and the task-create hook
// (stampAppEditChannelForTaskLocked) stamps the new task-<id> channel onto the
// app SYNCHRONOUSLY, so the bound channel is readable the moment MutateTask
// returns. The App Builder then greets the human in that channel and waits; a
// human post there wakes it through the same task_followup path edits use.
//
// Idempotent: an app already bound to an edit thread returns it untouched, so a
// double-click never spawns a second task. Gated like register/delete — a human
// session or the App Builder only.
func (b *Broker) handleAppEditSession(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actor, status, err := richArtifactAuthenticatedSlug(r, "", "actor")
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if !b.appWriterAllowed(r, actor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a human or the App Builder may open an edit session"})
		return
	}
	app, _, err := b.appStore().Get(id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	// Idempotent: already bound → return the existing thread, no new task.
	if ch := strings.TrimSpace(app.EditChannel); ch != "" {
		writeJSON(w, http.StatusOK, map[string]any{"channel": ch})
		return
	}
	// Ground the edit thread in the app's REAL shape (data model, APIs, writes,
	// UI), derived from its source, so the agent never invents capabilities.
	capsSummary := ""
	if source, serr := b.appStore().Source(id); serr == nil {
		capsSummary = renderAppCapabilities(introspectAppSource(source))
	}
	title, details := appEditSessionBrief(app, capsSummary)
	// MutateTask locks internally; this handler holds no broker lock (same
	// pattern as maybeSpawnAppBuilderTaskFromProposal).
	if _, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Channel:   "general",
		Title:     title,
		Details:   details,
		Owner:     appBuilderSlug,
		CreatedBy: "human",
		TaskType:  "issue",
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// The create hook stamped the new task-<id> channel onto the app
	// synchronously; re-read the manifest to return the bound channel.
	updated, _, err := b.appStore().Get(id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	ch := strings.TrimSpace(updated.EditChannel)
	if ch == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "edit session created but no channel was bound"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": ch})
}

// appWriterAllowed reports whether the request may write/delete app bytes:
// either the authenticated agent is the App Builder, or the caller is a human
// session. Other agents (which all hold the broker token) are rejected so they
// cannot register or remove apps directly outside the build path.
func (b *Broker) appWriterAllowed(r *http.Request, actor string) bool {
	if strings.EqualFold(strings.TrimSpace(actor), appBuilderSlug) {
		return true
	}
	if a, ok := requestActorFromContext(r.Context()); ok && a.Kind == requestActorKindHuman {
		return true
	}
	return false
}

func writeAppError(w http.ResponseWriter, err error) {
	switch {
	case isCustomAppCallerError(err):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, os.ErrNotExist):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

// Delete removes an app directory. Returns a caller error for an unknown id so
// the handler maps it to 404 rather than 500.
func (s *customAppStore) Delete(id string) error {
	if err := validateCustomAppID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.appDir(id)
	if _, err := os.Stat(filepath.Join(dir, customAppManifestFile)); err != nil {
		if os.IsNotExist(err) {
			return newCustomAppCallerError("app: %s not found", id)
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	// Evict the per-app publish mutex so the publishMu map doesn't grow without
	// bound across create/delete cycles. Safe: the dir is gone, so no publish for
	// this id can be in flight or start; a future same-id app lazily re-creates it.
	s.publishMu.Delete(id)
	return nil
}
