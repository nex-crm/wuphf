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
		versions, err := b.appStore().ListVersions(id)
		if err != nil {
			writeAppError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
	case "rollback":
		b.handleAppRollback(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
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
		// needs it (to edit), so the FE view never asks for it.
		if r.URL.Query().Get("source") == "1" {
			source, err := b.appStore().Source(id)
			if err != nil {
				writeAppError(w, err)
				return
			}
			out["source"] = source
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
	return os.RemoveAll(dir)
}
