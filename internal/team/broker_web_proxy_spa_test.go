package team

// broker_web_proxy_spa_test.go pins the SPA fallback: deep links into
// client-routed app paths (Slack Home tab buttons, task-link footnotes) must
// serve index.html, while real bundle files are served as themselves. A plain
// http.FileServer 404'd every deep link.

import (
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAFileServerServesDeepLinksAndRealFiles(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":       {Data: []byte("<html>app shell</html>")},
		"assets/main-x.js": {Data: []byte("console.log(1)")},
	}
	h := spaFileServer(assets)

	get := func(path string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code, rec.Body.String()
	}

	// Real files serve as themselves.
	if code, body := get("/assets/main-x.js"); code != 200 || !strings.Contains(body, "console.log") {
		t.Fatalf("real asset: code=%d body=%q", code, body)
	}
	// Root serves the shell.
	if code, body := get("/"); code != 200 || !strings.Contains(body, "app shell") {
		t.Fatalf("root: code=%d body=%q", code, body)
	}
	// Client-routed deep links fall back to the shell — including paths with
	// file-ish extensions (the wiki viewer routes /wiki/team/people/x.md).
	for _, p := range []string{"/tasks", "/tasks/OFFICE-41", "/wiki/team/people/nazz.md", "/inbox"} {
		if code, body := get(p); code != 200 || !strings.Contains(body, "app shell") {
			t.Fatalf("deep link %s: code=%d body=%q", p, code, body)
		}
	}
}
