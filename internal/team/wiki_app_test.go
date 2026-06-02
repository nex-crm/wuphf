package team

// Tests for the embedded-app file surface: GET /wiki/app/<team-relative-path>.
//
// This route is the ONLY wiki surface that opts a document into
// `sandbox allow-scripts` (opaque origin, NO allow-same-origin), and it is
// gated by loopback RemoteAddr + Host instead of bearer auth — a sandboxed
// app cannot send the token. The tests pin that security boundary:
//
//	(a) an index.html inside an app folder → 200 + sandbox-allow-scripts CSP,
//	    never allow-same-origin;
//	(b) a relative asset (app/foo/app.js) → text/javascript, no app CSP;
//	(c) a markdown article NOT inside an app folder → 404 (least privilege);
//	(d) non-loopback RemoteAddr → 403;
//	(e) non-loopback Host header (DNS-rebind) → 403;
//	(f) traversal / absolute path → 400;
//	(g) symlink → 404 (target contents never served);
//	(h) double-encoded ../ (%252e%252e%252f) NEVER escapes team/ — the handler
//	    relies on Go's single decode of r.URL.Path and does NOT decode again;
//	(i) a binary asset whose dir is not an app bundle → 404, body never leaks
//	    the file contents (least privilege for non-html files too).
//
// Unlike the wiki_fs tests, these drive the handler directly via
// httptest.NewRecorder + a hand-built request so RemoteAddr and Host can be
// forged deterministically (an httptest.Server always reports a loopback
// RemoteAddr, which cannot exercise the negative gate cases). The repo +
// seedFile helpers are shared from wiki_fs_test.go.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newWikiAppTestBroker builds a broker backed by a fresh temp-dir Repo, the
// same wiring newWikiFSTestServer uses, but returns the broker so tests can
// invoke handleWikiApp directly with a forged request.
func newWikiAppTestBroker(t *testing.T) (*Broker, *Repo) {
	t.Helper()
	// Reuse the wiki_fs harness: it Inits a Repo and a WikiWorker.
	_, repo, cleanup := newWikiFSTestServer(t)
	t.Cleanup(cleanup)
	worker := NewWikiWorker(repo, &capturePublisher{events: make(chan wikiWriteEvent, 4)})
	broker := &Broker{wikiWorker: worker}
	return broker, repo
}

// callWikiApp invokes handleWikiApp for the given team-relative path with an
// explicit RemoteAddr and Host, returning the recorder. The path is the value
// after the /wiki/app/ prefix (e.g. "team/dash/index.html").
func callWikiApp(t *testing.T, b *Broker, relPath, remoteAddr, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, wikiAppPathPrefix+relPath, nil)
	req.RemoteAddr = remoteAddr
	req.Host = host
	rec := httptest.NewRecorder()
	b.handleWikiApp(rec, req)
	return rec
}

const (
	loopbackRemote = "127.0.0.1:1234"
	loopbackHost   = "127.0.0.1:7891"
)

// (a) index.html inside an app folder → 200 + sandbox-allow-scripts CSP,
// never allow-same-origin.
func TestWikiAppServesIndexWithSandboxCSP(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	// An app folder: index.html present, no index.md.
	const html = "<!doctype html><title>Dash</title><script>1</script>"
	seedFile(t, repo, "dash/index.html", html)

	rec := callWikiApp(t, b, "team/dash/index.html", loopbackRemote, loopbackHost)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") {
		t.Errorf("CSP = %q, want a sandbox directive", csp)
	}
	if !strings.Contains(csp, "allow-scripts") {
		t.Errorf("CSP = %q, want allow-scripts (embedded apps run JS)", csp)
	}
	// The load-bearing omission: same-origin must NOT be granted, or the app's
	// JS could read the WUPHF token / localStorage and ride the user session.
	if strings.Contains(csp, "allow-same-origin") {
		t.Errorf("CSP = %q must NOT contain allow-same-origin (opaque origin is the boundary)", csp)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache, must-revalidate" {
		t.Errorf("Cache-Control = %q, want no-cache, must-revalidate", cc)
	}
	// The app is meant to be framed by our SPA: X-Frame-Options must be absent.
	if xfo := rec.Header().Get("X-Frame-Options"); xfo != "" {
		t.Errorf("X-Frame-Options = %q, want empty (app is framed by the SPA)", xfo)
	}
	if body := rec.Body.String(); body != html {
		t.Errorf("body = %q, want %q", body, html)
	}
}

// (b) a relative asset inside an app folder → text/javascript, no app CSP.
func TestWikiAppServesAssetWithoutAppCSP(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	// app/ is an app bundle (index.html, no index.md); foo/app.js is a nested
	// asset belonging to that bundle.
	seedFile(t, repo, "app/index.html", "<!doctype html><title>App</title>")
	const js = "console.log('hi');\n"
	seedFile(t, repo, "app/foo/app.js", js)

	rec := callWikiApp(t, b, "team/app/foo/app.js", loopbackRemote, loopbackHost)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/javascript; charset=utf-8", ct)
	}
	// Assets do not carry the app document CSP — the sandbox is enforced by the
	// framing index.html's opaque origin.
	if csp := rec.Header().Get("Content-Security-Policy"); csp != "" {
		t.Errorf("asset CSP = %q, want no CSP on non-html assets", csp)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=300" {
		t.Errorf("Cache-Control = %q, want private, max-age=300", cc)
	}
	if body := rec.Body.String(); body != js {
		t.Errorf("body = %q, want %q", body, js)
	}
}

// (c) a markdown article NOT inside an app folder → 404. This is the
// least-privilege boundary: /wiki/app/ must not become an unauthenticated
// reader of ordinary wiki content.
func TestWikiAppRejectsNonAppContent(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	// A people page that lives in a plain content dir, not an app bundle.
	seedFile(t, repo, "people/nazz.md", "# Nazz Mohammad\n\nFounder.\n")

	rec := callWikiApp(t, b, "team/people/nazz.md", loopbackRemote, loopbackHost)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (article not inside an app folder); body: %s",
			rec.Code, rec.Body.String())
	}
	// The body must not leak the article contents.
	if strings.Contains(rec.Body.String(), "Founder") {
		t.Errorf("404 body leaked article contents: %q", rec.Body.String())
	}
}

// TestWikiAppRejectsContentAdjacentToApp proves the bundle check is scoped to
// the app subtree: a sibling file in the SAME parent as an app folder, but not
// inside it, is not served.
func TestWikiAppRejectsContentAdjacentToApp(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	// section/ has both an app subfolder AND a loose markdown file. The app
	// folder is section/widget; section/notes.md is adjacent, not inside it.
	seedFile(t, repo, "section/widget/index.html", "<!doctype html><title>W</title>")
	seedFile(t, repo, "section/notes.md", "# Notes\n\nsecret-adjacent\n")

	// The asset inside the app folder is served.
	ok := callWikiApp(t, b, "team/section/widget/index.html", loopbackRemote, loopbackHost)
	if ok.Code != http.StatusOK {
		t.Fatalf("app index status = %d, want 200", ok.Code)
	}
	// The adjacent markdown is not.
	rec := callWikiApp(t, b, "team/section/notes.md", loopbackRemote, loopbackHost)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("adjacent content status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
}

// TestWikiAppDirWithIndexMdIsNotAppRoot proves a directory holding BOTH
// index.html and index.md is a plain content dir (a curated page bundle), NOT
// an app — so even its index.html is not served via /wiki/app/.
func TestWikiAppDirWithIndexMdIsNotAppRoot(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	seedFile(t, repo, "guide/index.html", "<!doctype html><title>Guide</title>")
	seedFile(t, repo, "guide/index.md", "# Guide\n")

	rec := callWikiApp(t, b, "team/guide/index.html", loopbackRemote, loopbackHost)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (index.md present → not an app root); body: %s",
			rec.Code, rec.Body.String())
	}
}

// (d) non-loopback RemoteAddr → 403.
func TestWikiAppRejectsNonLoopbackRemote(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)
	seedFile(t, repo, "dash/index.html", "<!doctype html><title>Dash</title>")

	rec := callWikiApp(t, b, "team/dash/index.html", "203.0.113.7:5555", loopbackHost)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for non-loopback RemoteAddr", rec.Code)
	}
	// 403 must not leak the (would-be) file contents.
	if strings.Contains(rec.Body.String(), "Dash") {
		t.Errorf("403 body leaked file contents: %q", rec.Body.String())
	}
}

// (e) non-loopback Host header → 403 (DNS-rebind defense). RemoteAddr is
// loopback (the listener binds 127.0.0.1) but Host is attacker-controlled.
func TestWikiAppRejectsNonLoopbackHost(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)
	seedFile(t, repo, "dash/index.html", "<!doctype html><title>Dash</title>")

	rec := callWikiApp(t, b, "team/dash/index.html", loopbackRemote, "rebind.example.com")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for non-loopback Host (DNS-rebind)", rec.Code)
	}
}

// (f) traversal / absolute path → 400.
func TestWikiAppRejectsBadPaths(t *testing.T) {
	b, _ := newWikiAppTestBroker(t)

	cases := []struct {
		name    string
		relPath string
	}{
		{"traversal", "team/../secret.txt"},
		{"deep traversal", "team/../../etc/passwd"},
		{"outside team root", "secret.txt"},
		{"prefix confusion", "team-secrets/x.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := callWikiApp(t, b, tc.relPath, loopbackRemote, loopbackHost)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("path %q: status %d, want 400 (body: %s)",
					tc.relPath, rec.Code, rec.Body.String())
			}
		})
	}
}

// Absolute paths are exercised separately: httptest.NewRequest needs a target
// that round-trips through URL parsing, so we build the request explicitly.
func TestWikiAppRejectsAbsolutePath(t *testing.T) {
	b, _ := newWikiAppTestBroker(t)

	// "/wiki/app//etc/passwd" → relpath "/etc/passwd" after the prefix, which
	// resolveTeamRelPath rejects as absolute.
	req := httptest.NewRequest(http.MethodGet, "/wiki/app//etc/passwd", nil)
	req.RemoteAddr = loopbackRemote
	req.Host = loopbackHost
	rec := httptest.NewRecorder()
	b.handleWikiApp(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("absolute path: status %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

// (h) A double-encoded "../" must NEVER escape team/. The raw request path
// carries %252e%252e%252f; net/http decodes it ONCE to "%2e%2e%2f" in
// r.URL.Path, and the handler must NOT decode a second time. A second
// PathUnescape would turn that into "../" and smuggle a traversal past
// resolveTeamRelPath. With the single-decode handler the segment stays a
// literal filename confined under team/, so the request resolves to a missing
// file (404) and never a file outside the app bundle. We build the request
// explicitly so the encoded bytes survive into r.URL.Path verbatim.
func TestWikiAppRejectsDoubleEncodedTraversal(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	// A real secret OUTSIDE team/ that a successful traversal would expose.
	const secret = "DOUBLE-DECODE PAYLOAD that must never be served"
	target := filepath.Join(repo.Root(), "secret.txt")
	if err := os.WriteFile(target, []byte(secret), 0o644); err != nil {
		t.Fatalf("seed secret: %v", err)
	}

	// Raw path: /wiki/app/team/%252e%252e%252fsecret.txt. After Go's single
	// decode this is "team/%2e%2e%2fsecret.txt" (literal percent-escapes, no
	// dot-dot). It must NOT escape team/.
	req := httptest.NewRequest(http.MethodGet, "/wiki/app/team/%252e%252e%252fsecret.txt", nil)
	req.RemoteAddr = loopbackRemote
	req.Host = loopbackHost
	rec := httptest.NewRecorder()
	b.handleWikiApp(rec, req)

	// Never a success: a 200 here would mean the double-encoded ../ escaped.
	// 400 (rejected as invalid) or 404 (treated as a missing literal file) are
	// both acceptable; the only forbidden outcome is serving a file outside the
	// app bundle.
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Fatalf("double-encoded ../: status %d, want 400 or 404 (never an escape); body: %s",
			rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("double-encoded ../ leaked secret contents: %q", rec.Body.String())
	}
}

// (i) Least privilege for binary assets: a binary file living in a directory
// that is NOT an app bundle (no index.html in any ancestor up to team/) must be
// 404, with no contents leaked. This proves the bundle gate covers non-html
// uploads, not just markdown — /wiki/app/ must not become an unauthenticated
// reader of arbitrary uploaded binaries.
func TestWikiAppRejectsBinaryOutsideBundle(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	// team/assets/ holds an uploaded binary but has NO index.html, so it is not
	// an app root and no ancestor up to team/ is one either.
	const xlsx = "PK\x03\x04 binary spreadsheet bytes — payroll secrets"
	seedFile(t, repo, "assets/secret.xlsx", xlsx)

	rec := callWikiApp(t, b, "team/assets/secret.xlsx", loopbackRemote, loopbackHost)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("binary outside bundle: status %d, want 404 (least privilege); body: %s",
			rec.Code, rec.Body.String())
	}
	// The 404 body must not leak the file contents.
	if strings.Contains(rec.Body.String(), "payroll secrets") {
		t.Fatalf("404 body leaked binary contents: %q", rec.Body.String())
	}
}

// (g) symlink → 404, and the symlink target contents are never served.
func TestWikiAppRejectsSymlink(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)

	// An app bundle so the least-privilege gate would otherwise pass.
	seedFile(t, repo, "app/index.html", "<!doctype html><title>App</title>")

	// A real secret OUTSIDE team/, and a symlink inside the app folder that
	// points to it. resolveTeamRelPath confines the path, but os.Open would
	// follow the symlink — the Lstat reject closes that hole.
	const secret = "TOP SECRET PAYLOAD that must never be served"
	target := filepath.Join(repo.Root(), "secret-target.txt")
	if err := os.WriteFile(target, []byte(secret), 0o644); err != nil {
		t.Fatalf("seed secret target: %v", err)
	}
	link := filepath.Join(repo.TeamDir(), "app", "escape.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unsupported on this platform: %v", err)
	}

	rec := callWikiApp(t, b, "team/app/escape.txt", loopbackRemote, loopbackHost)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("symlink: status %d, want 404", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, secret) {
		t.Fatalf("symlink GET leaked target contents: %q", body)
	}
}

// TestWikiAppRejectsNonGetMethod pins the method guard.
func TestWikiAppRejectsNonGetMethod(t *testing.T) {
	b, repo := newWikiAppTestBroker(t)
	seedFile(t, repo, "dash/index.html", "<!doctype html><title>Dash</title>")

	req := httptest.NewRequest(http.MethodPost, wikiAppPathPrefix+"team/dash/index.html", nil)
	req.RemoteAddr = loopbackRemote
	req.Host = loopbackHost
	rec := httptest.NewRecorder()
	b.handleWikiApp(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d, want 405", rec.Code)
	}
}
