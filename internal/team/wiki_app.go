package team

// wiki_app.go serves the bytes of self-contained embedded HTML apps/websites
// that live inside the wiki content tree (team/<...>/index.html + siblings),
// so the cabinet SPA can frame them. It is the ONLY wiki surface that opts a
// document into `sandbox allow-scripts`: the app runs JS, yet because it is
// granted an OPAQUE origin (NO allow-same-origin), its script cannot read the
// WUPHF bearer token, our localStorage, or call authenticated /wiki/* as the
// signed-in user.
//
// Security boundary
// =================
//
//   - No bearer auth. An embedded app frames sandboxed with an opaque origin
//     and cannot attach the bearer token, so a token check would simply break
//     it. Instead the route is gated EXACTLY like /web-token: it serves only
//     when BOTH the RemoteAddr and the Host header are loopback. That closes
//     the DNS-rebinding hole (Go's mux ignores Host, so a loopback-bound
//     listener would otherwise answer an attacker's rebound origin). We never
//     embed a token in the response or URL.
//
//   - Least privilege. /wiki/app/ must NOT become an unauthenticated reader of
//     all wiki content (articles, uploads, people pages). The requested file
//     is served only when it belongs to an actual embedded app bundle: some
//     ancestor directory (the file's own dir, up to team/) contains an
//     index.html AND no index.md — the same app/website classification the
//     tree walk uses (see buildDirNode). A file with no such ancestor is 404.
//
//   - Path safety + symlinks. The relpath is taken from r.URL.Path, which Go
//     has ALREADY percent-decoded once, and is fed straight to
//     resolveTeamRelPath WITHOUT a second decode. A second PathUnescape would
//     re-decode an attacker's %252e%252e ("%2e%2e" after Go's first decode)
//     into "..", smuggling dot-dot past the traversal check; deferring to Go's
//     single decode closes that. resolveTeamRelPath then enforces
//     traversal/absolute/control/team-confinement and the resolved file is
//     Lstat-rejected if it is a symlink, exactly like handleWikiFile, so a
//     symlink inside team/ cannot escape the tree.

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// wikiAppPathPrefix is the path-prefix route (trailing slash) under which app
// bundle bytes are served. The relpath after the prefix is repo-root relative
// and carries the team/ prefix, identical to /wiki/file's ?path shape.
const wikiAppPathPrefix = "/wiki/app/"

// wikiAppSandboxCSP is the Content-Security-Policy applied to the app's HTML
// document. The `sandbox` directive forces an opaque origin and re-grants only
// the capabilities an embedded app legitimately needs. Critically it does NOT
// include allow-same-origin: that omission is what keeps the app's JS unable
// to read the WUPHF token, localStorage, or ride the user's session against
// authenticated /wiki/* routes. It also omits allow-popups-to-escape-sandbox:
// letting a popup shed the sandbox is an unnecessary privilege for an embedded
// app, so the popup it opens stays under the same restrictions.
const wikiAppSandboxCSP = "sandbox allow-scripts allow-forms allow-popups " +
	"allow-modals allow-downloads"

// handleWikiApp serves GET /wiki/app/<team-relative-path>. It is registered
// WITHOUT requireAuth; the loopback gate below is the security boundary (see
// the file header). Range requests are served as 206 via http.ServeContent.
func (b *Broker) handleWikiApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// GATE: loopback RemoteAddr AND loopback Host, identical to /web-token.
	// This stands in for bearer auth because a sandboxed embedded app cannot
	// send the token. Validating both closes the DNS-rebinding hole. No body
	// detail on rejection.
	if !isLoopbackRemote(r) || !hostHeaderIsLoopback(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	worker := b.requireWikiWorker(w, "wiki app")
	if worker == nil {
		return
	}
	repo := worker.Repo()

	// r.URL.Path is already percent-decoded once by net/http, so
	// "team/my%20app/index.html" arrives as "team/my app/index.html". We feed
	// the trimmed path straight to resolveTeamRelPath WITHOUT a second decode:
	// a second PathUnescape would re-decode "%2e%2e" (what an attacker's
	// %252e%252e becomes after Go's first decode) into "..", smuggling a
	// traversal past the check below. Deferring to Go's single decode is the fix.
	relPath := strings.TrimPrefix(r.URL.Path, wikiAppPathPrefix)

	_, abs, err := resolveTeamRelPath(repo.Root(), relPath)
	if err != nil {
		// Every resolveTeamRelPath failure (errWikiFSBadPath and any other) is a
		// caller error → 400 invalid path. The error text is fixed strings plus
		// the caller path; it never reveals server filesystem layout, and we do
		// not forward it to the client either way.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	// Lstat BEFORE os.Open: resolveTeamRelPath proves the *path* stays under
	// team/, but os.Open follows symlinks, so a symlink inside team/ pointing
	// outside the repo would otherwise be served. Reject any symlink as a plain
	// 404 so the response is indistinguishable from a missing file.
	linfo, err := os.Lstat(abs) //nolint:gosec // abs is confined to team/ by resolveTeamRelPath
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		// Never forward the raw err: it can leak filesystem layout.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat file"})
		return
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	// A directory path is not a servable file; 404 rather than stream dir bytes.
	if linfo.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	// LEAST PRIVILEGE: the file must belong to an embedded app bundle. If no
	// ancestor directory (up to team/) is an app root (index.html present,
	// index.md absent), this is ordinary wiki content and must NOT be served
	// here. 404 keeps the route from becoming an unauthenticated reader of all
	// wiki content.
	if !fileInsideAppBundle(repo.Root(), abs) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}

	f, err := os.Open(abs) //nolint:gosec // abs is confined to team/ and Lstat-checked above
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open file"})
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "stat file"})
		return
	}

	ext := strings.ToLower(filepath.Ext(abs))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if ext == ".html" {
		// The app document: opaque-origin sandbox that re-grants scripts but
		// never same-origin (see wikiAppSandboxCSP). No X-Frame-Options: the
		// app is meant to be framed by our SPA.
		w.Header().Set("Content-Type", wikiFSContentType(ext))
		w.Header().Set("Content-Security-Policy", wikiAppSandboxCSP)
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	} else {
		// App assets (js, css, images, fonts, …). No app CSP — the sandbox
		// already governs them via the framing document's opaque origin.
		w.Header().Set("Content-Type", wikiFSContentType(ext))
		w.Header().Set("Cache-Control", "private, max-age=300")
	}

	// http.ServeContent handles Range (206), Accept-Ranges, If-Modified-Since,
	// and Content-Length. It fills Content-Type only when unset, so our
	// explicit type above is preserved.
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// fileInsideAppBundle reports whether absFile lives inside an embedded app
// bundle: some ancestor directory between absFile's own directory and the
// team/ root (inclusive of the own dir, exclusive of team/ itself unless team/
// is itself an app root) contains an index.html AND no index.md. This mirrors
// the app/website classification in buildDirNode and is the least-privilege
// gate that prevents /wiki/app/ from serving arbitrary wiki content.
//
// The walk stops at teamDir: directories above team/ are never app roots, and
// continuing past team/ would defeat team confinement. absFile is assumed to
// already be confined to teamDir by resolveTeamRelPath.
func fileInsideAppBundle(repoRoot, absFile string) bool {
	teamDir := filepath.Clean(filepath.Join(repoRoot, "team"))
	dir := filepath.Dir(filepath.Clean(absFile))

	for {
		// Only consider directories at or below team/. isPathWithin treats
		// teamDir as within itself, so team/ as an app root is allowed.
		if !isPathWithin(teamDir, dir) {
			return false
		}
		if dirIsAppRoot(dir) {
			return true
		}
		if dir == teamDir {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without hitting teamDir; stop.
			return false
		}
		dir = parent
	}
}

// dirIsAppRoot reports whether dir is the root of an embedded app/website:
// it contains a regular index.html and does NOT contain an index.md. This is
// the exact predicate buildDirNode uses to classify a directory as app/website
// rather than a plain dir. regularFileExists uses Lstat, so a symlinked
// index.html cannot promote a directory to an app root.
func dirIsAppRoot(dir string) bool {
	return regularFileExists(filepath.Join(dir, "index.html")) &&
		!regularFileExists(filepath.Join(dir, "index.md"))
}
