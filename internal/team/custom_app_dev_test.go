package team

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStripCSPMetaFromHTML is the regression for the blank-preview bug: the
// scaffold's sealed-mode <meta> CSP (script-src 'unsafe-inline', no 'self')
// intersected with the proxy header CSP and blocked Vite's own /src modules. The
// proxy must strip the meta so its header CSP is the sole authority.
func TestStripCSPMetaFromHTML(t *testing.T) {
	html := `<!doctype html><html><head>` +
		`<meta http-equiv="Content-Security-Policy" content="script-src 'unsafe-inline'">` +
		`<title>x</title></head><body><div id="root"></div></body></html>`
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/html"}},
		Body:   io.NopCloser(strings.NewReader(html)),
	}
	if err := stripCSPMeta(resp); err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	if strings.Contains(strings.ToLower(string(out)), "content-security-policy") {
		t.Fatalf("CSP meta not stripped: %s", out)
	}
	if !strings.Contains(string(out), "<title>x</title>") {
		t.Fatalf("stripping removed unrelated content: %s", out)
	}
}

func TestStripCSPMetaSkipsNonHTML(t *testing.T) {
	js := `const x = 1 // content-security-policy mention in a comment`
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/javascript"}},
		Body:   io.NopCloser(strings.NewReader(js)),
	}
	if err := stripCSPMeta(resp); err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	if string(out) != js {
		t.Fatalf("non-HTML body was modified: %s", out)
	}
}

func TestAppDevRingCapsBootLog(t *testing.T) {
	var r appDevRing
	r.Write([]byte(strings.Repeat("a", appDevBootLogMax+500)))
	if len(r.String()) > appDevBootLogMax {
		t.Fatalf("ring exceeded cap: %d > %d", len(r.String()), appDevBootLogMax)
	}
}

func TestAppDevURLScrape(t *testing.T) {
	cases := map[string]string{
		"  ➜  Local:   http://localhost:5173/": "http://localhost:5173",
		"➜  Local:   http://127.0.0.1:5199/":   "http://127.0.0.1:5199",
		// A decoy URL NOT on a Vite "Local:" line must NOT match — an agent's
		// vite.config plugin could print one early to redirect the proxy.
		"plugin printed http://127.0.0.1:9999/ early": "",
		"no url in this line":                         "",
	}
	for in, want := range cases {
		got := ""
		if m := appDevURLRe.FindStringSubmatch(in); m != nil {
			got = m[1]
		}
		if got != want {
			t.Fatalf("scrape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestAppDevProxyEnforcesCSP locks the security invariant: the broker-owned
// proxy injects the App-Builder CSP on every dev response and strips
// X-Frame-Options, REGARDLESS of what the (agent-controlled) dev server sent —
// so a generated vite.config cannot weaken the no-exfiltration guarantee.
func TestAppDevProxyEnforcesCSP(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A hostile/loose dev server tries to open the CSP and block framing.
		w.Header().Set("Content-Security-Policy", "default-src *")
		w.Header().Set("X-Frame-Options", "DENY")
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()
	s := &appDevServer{id: "app_0000000000000000", proxyPort: 4321}
	s.setReady(backend.URL) // builds the proxy + injects the CSP

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	s.serveProxy(rec, req)
	res := rec.Result()

	want := appDevCSP(4321)
	if got := res.Header.Get("Content-Security-Policy"); got != want {
		t.Fatalf("proxy did not enforce the App Builder CSP; got %q want %q", got, want)
	}
	if got := res.Header.Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options must be stripped so the preview can be framed; got %q", got)
	}
	// connect-src must be the same-origin HMR socket ONLY — no wildcard ws/wss
	// and no 'self' (no arbitrary fetch/XHR), so the app can't reach the network.
	if strings.Contains(want, "ws: wss:") || strings.Contains(want, "connect-src *") || strings.Contains(want, "connect-src 'self'") {
		t.Fatalf("dev CSP connect-src too permissive: %q", want)
	}
	if !strings.Contains(want, "connect-src ws://127.0.0.1:4321 wss://127.0.0.1:4321") {
		t.Fatalf("dev CSP must allow only the same-origin HMR socket: %q", want)
	}
}

func TestAppDevProxyRejectsForeignHost(t *testing.T) {
	s := &appDevServer{id: "app_0000000000000000", proxyPort: 4321}
	s.setReady("http://127.0.0.1:5173")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com" // DNS-rebinding attempt
	rec := httptest.NewRecorder()
	s.serveProxy(rec, req)
	if rec.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host must be rejected (DNS-rebind guard), got %d", rec.Result().StatusCode)
	}
}

func TestAppDevProxy503BeforeReady(t *testing.T) {
	s := &appDevServer{id: "app_0000000000000000", proxyPort: 4321} // not ready, no proxy
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	s.serveProxy(rec, req)
	if rec.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 before the dev server is ready, got %d", rec.Result().StatusCode)
	}
}

func TestAppDevStatusURLOnlyWhenReady(t *testing.T) {
	s := &appDevServer{id: "app_0000000000000000", proxyPort: 4321}
	if st := s.status(); st.Ready || st.URL != "" {
		t.Fatalf("not-ready status must omit the url, got %+v", st)
	}
	s.setReady("http://127.0.0.1:5173")
	st := s.status()
	if !st.Ready || st.URL != "http://127.0.0.1:4321/" {
		t.Fatalf("ready status should expose the proxy origin, got %+v", st)
	}
}
