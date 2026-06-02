package team

// wiki_fs_handlers.go holds the two thin HTTP handlers for the cabinet-style
// wiki file experience:
//
//	GET /wiki/tree            — the directory + page + app/website tree
//	GET /wiki/file?path=...   — raw bytes of a single file, Range-aware
//
// Both reach the wiki repo through b.requireWikiWorker(...).Repo(), matching
// the rest of the /wiki/* surface. Route registration lives in broker.go and
// is intentionally NOT done here (see the integrator note in the task).

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleWikiTree serves GET /wiki/tree. An optional ?path= names a subtree
// root relative to repo root (e.g. team/people); the default is the whole
// team/ tree. The response is {"nodes": TreeNode[]}.
func (b *Broker) handleWikiTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "wiki tree")
	if worker == nil {
		return
	}
	repo := worker.Repo()

	subPath := strings.TrimSpace(r.URL.Query().Get("path"))
	nodes, err := buildWikiTree(repo.Root(), subPath)
	if err != nil {
		if errors.Is(err, errWikiFSBadPath) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if nodes == nil {
		nodes = []TreeNode{}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

// handleWikiFile serves GET /wiki/file?path=<relpath>. The path is repo-root
// relative and must resolve to a real file under team/. Range requests are
// served as 206 via http.ServeContent.
func (b *Broker) handleWikiFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.requireWikiWorker(w, "wiki file")
	if worker == nil {
		return
	}
	repo := worker.Repo()

	relPath := r.URL.Query().Get("path")
	_, abs, err := resolveTeamRelPath(repo.Root(), relPath)
	if err != nil {
		// All resolveTeamRelPath failures are caller errors → 400.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Lstat BEFORE os.Open: the string-containment check in resolveTeamRelPath
	// proves the *path* stays under team/, but os.Open follows symlinks, so a
	// symlink inside team/ pointing outside the repo would otherwise be served.
	// Reject any symlink as a plain 404 — do NOT reveal that it is a symlink, so
	// the response is indistinguishable from a missing file.
	linfo, err := os.Lstat(abs) //nolint:gosec // abs is confined to team/ by resolveTeamRelPath
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		// Never forward the raw err: it can leak filesystem layout. Fixed strings only.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat file"})
		return
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	f, err := os.Open(abs) //nolint:gosec // abs is confined to team/ by resolveTeamRelPath and Lstat-checked above
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		// Never forward the raw err: it can leak filesystem layout. Fixed strings only.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open file"})
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		// Never forward the raw err: it can leak filesystem layout. Fixed strings only.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat file"})
		return
	}
	// A directory path (e.g. ?path=team/people) is not a servable file. Treat
	// it as a 404 rather than streaming directory bytes.
	if info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	ext := strings.ToLower(filepath.Ext(abs))
	w.Header().Set("Content-Type", wikiFSContentType(ext))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Script-capable, user-authored content (HTML/SVG/XML) is served from the
	// SAME ORIGIN as the app, so a naive inline response is a stored-XSS sink:
	// a wiki author or agent could store <script> that, on direct navigation to
	// /wiki/file, runs in our origin and calls authenticated /wiki/* endpoints.
	// The CSP `sandbox` directive WITHOUT allow-scripts neutralizes scripts,
	// plugins, and forms and forces an opaque origin — for top-level navigation
	// AND inside frames (the response-header sandbox is the most-restrictive
	// floor; an iframe sandbox attribute cannot re-grant scripts). A generic
	// file fetch must never execute scripts. The deliberate embedded-app surface
	// is the only path that opts into `sandbox allow-scripts` (opaque origin, no
	// allow-same-origin), so apps run JS yet cannot read WUPHF credentials.
	if isScriptCapableExt(ext) {
		w.Header().Set("Content-Security-Policy",
			"sandbox; default-src 'none'; img-src 'self' data: blob:; style-src 'unsafe-inline'; font-src 'self' data:; media-src 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		// Re-generated content: force revalidation so a stale build is never served.
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	} else {
		// Inert assets (images, fonts, pdf, media, csv, …) are content enough to
		// cache briefly per-client.
		w.Header().Set("Cache-Control", "private, max-age=300")
	}

	// http.ServeContent handles Range (206 Partial Content), Accept-Ranges,
	// If-Modified-Since, and Content-Length. It also sets Content-Type by
	// sniffing/extension, so we set our explicit type above first; ServeContent
	// only fills it when unset, leaving our value intact.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// isScriptCapableExt reports whether a file extension can execute script when
// rendered as a top-level document or framed document — the stored-XSS surface
// for same-origin file serving. Such responses get a scripts-disabled sandbox
// CSP. Inert types (png, pdf, css, js-as-text, …) are excluded: navigating to
// a .js/.css serves it as text, it is not executed as a page.
func isScriptCapableExt(ext string) bool {
	switch ext {
	case ".html", ".htm", ".svg", ".xhtml", ".xml":
		return true
	default:
		return false
	}
}
