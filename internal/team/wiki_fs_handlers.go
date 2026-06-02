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
	// HTML backs self-contained apps/websites the user re-generates; force
	// revalidation so a stale build is never served. Everything else is
	// content-addressed enough to cache briefly per-client.
	if ext == ".html" {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		// Clickjacking close for served HTML: allow same-origin framing (our own
		// SPA embeds these apps/websites) but block cross-origin framing. We do
		// NOT set a restrictive default-src — embedded apps load their own
		// resources; frame-ancestors 'self' is the targeted control here.
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=300")
	}

	// http.ServeContent handles Range (206 Partial Content), Accept-Ranges,
	// If-Modified-Since, and Content-Length. It also sets Content-Type by
	// sniffing/extension, so we set our explicit type above first; ServeContent
	// only fills it when unset, leaving our value intact.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
