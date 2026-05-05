package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	wuphf "github.com/nex-crm/wuphf"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
)

// Web UI server. Owns:
//   - ServeWebUI: stands up the static web UI on its own port (separate
//     from the broker API), serving the Vite build (filesystem or embedded
//     FS) plus a same-origin proxy back to the broker API.
//   - cacheControlMiddleware: cache-bust index.html, long-cache hashed bundles.
//   - webUIProxyHandler: reverse-proxies /api and /onboarding to the broker,
//     attaching the Bearer token server-side and streaming SSE responses.
//
// The DNS-rebinding guard (webUIRebindGuard) and the artist file mount
// are wired here for the same reason: this is the file that defines what
// the public web UI port serves.

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

func (b *Broker) ServeWebUI(port int) error {
	b.webUIOrigins = []string{
		fmt.Sprintf("http://localhost:%d", port),
		fmt.Sprintf("http://127.0.0.1:%d", port),
	}

	// Resolution order for the web UI assets:
	//   1. filesystem web/dist/ (local dev after `bun run build`)
	//   2. embedded FS (single-binary installs via curl | bash)
	exePath, _ := os.Executable()
	webDir := filepath.Join(filepath.Dir(exePath), "web")
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}
	var fileServer http.Handler
	distDir := filepath.Join(webDir, "dist")
	distIndex := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(distIndex); err == nil {
		// Real Vite build output on disk — use it.
		fileServer = http.FileServer(http.Dir(distDir))
	} else if embeddedFS, ok := wuphf.WebFS(); ok {
		// No on-disk build; use embedded assets.
		fileServer = http.FileServer(http.FS(embeddedFS))
	} else {
		// Source checkout without web/dist. Do not serve raw Vite source files:
		// browsers load /src/main.tsx as text/plain and the page stalls on
		// "Loading WUPHF". Return an actionable setup page instead.
		fileServer = missingWebAssetsHandler()
	}
	mux := http.NewServeMux()
	brokerURL := brokeraddr.ResolveBaseURL()
	if addr := strings.TrimSpace(b.Addr()); addr != "" {
		brokerURL = "http://" + addr
	}
	// Same-origin proxy to the broker for app API routes and onboarding wizard routes.
	// Both are wrapped in webUIRebindGuard: the proxy auto-attaches the broker's
	// Bearer token server-side, so without a Host/RemoteAddr check, a DNS-rebinding
	// attack against an attacker-controlled hostname that resolves to 127.0.0.1
	// would ride the token and control the entire office.
	mux.Handle("/api/share/status", webUIRebindGuard(http.HandlerFunc(b.handleWebShareStatus)))
	mux.Handle("/api/share/start", webUIRebindGuard(http.HandlerFunc(b.handleWebShareStart)))
	mux.Handle("/api/share/stop", webUIRebindGuard(http.HandlerFunc(b.handleWebShareStop)))
	mux.Handle("/api/broker/restart", webUIRebindGuard(http.HandlerFunc(b.handleWebBrokerRestart)))
	mux.Handle("/api/", webUIRebindGuard(b.webUIProxyHandler(brokerURL, "/api")))
	mux.Handle("/onboarding/", webUIRebindGuard(b.webUIProxyHandler(brokerURL, "")))
	// Token endpoint — no auth needed, but we require a same-origin loopback request.
	// Otherwise this endpoint leaks the broker bearer to any browser page that
	// can reach the web UI port via DNS rebinding.
	mux.Handle("/api-token", webUIRebindGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      b.token,
			"broker_url": brokerURL,
		})
	})))
	// Cache policy: index.html must be re-fetched every load so users pick up
	// new JS/CSS bundle hashes immediately after an upgrade. Hashed assets
	// under /assets/ are content-addressed and safe to cache aggressively.
	// Without this, users stay pinned to a stale bundle for days because
	// Chrome's heuristic cache revalidates HTML only occasionally.
	// Serve generated images from ~/.wuphf/office/artist/ so the BoardRoom
	// can render them inline via standard markdown <img>. Browsers can't
	// fetch file:// URLs and don't carry the broker's bearer token on
	// <img> requests, so this mount must live on the web-UI port (no auth)
	// rather than the API mux. Path traversal is bounded by http.FileServer
	// + http.Dir; we strip the prefix so requests resolve relative to the
	// artist root.
	artistRoot := imagegenArtistRoot()
	mux.Handle("/artist-files/", http.StripPrefix(
		"/artist-files/",
		http.FileServer(http.Dir(artistRoot)),
	))

	mux.Handle("/", cacheControlMiddleware(fileServer))
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	// noctx: net.Listen is the blocking primitive; the lint rule is meant
	// for HTTP clients. Use ListenConfig.Listen with a Background context
	// so the linter's intent (no caller-controllable cancellation lost) is
	// satisfied without changing the actual lifecycle.
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return fmt.Errorf("web UI: listen on %s: %w", addr, err)
	}
	srv := &http.Server{Handler: mux}
	b.brokerRestartMu.Lock()
	b.webUIServer = srv
	b.webUIListener = ln
	b.brokerRestartMu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("broker web UI proxy: serve on :%d: %v", port, err)
		}
	}()
	return nil
}

func missingWebAssetsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>WUPHF web UI assets missing</title>
  <style>
    body { margin: 0; font: 16px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #1f2933; background: #f5f3ef; }
    main { max-width: 760px; margin: 10vh auto; padding: 0 24px; }
    h1 { font-size: 28px; line-height: 1.2; margin: 0 0 12px; }
    p { margin: 0 0 16px; }
    pre { overflow-x: auto; padding: 16px; border: 1px solid #d7d0c6; border-radius: 8px; background: #fffaf2; }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 14px; }
  </style>
</head>
<body>
  <main>
    <h1>WUPHF web UI assets are missing</h1>
    <p>This source build did not include <code>web/dist/index.html</code>, and the embedded bundle is empty.</p>
    <p>Build the frontend bundle before rebuilding the Go binary:</p>
    <pre><code>cd web
bun install
bun run build
cd ..
go build -o wuphf ./cmd/wuphf
./wuphf</code></pre>
  </main>
</body>
</html>`)
	})
}

// cacheControlMiddleware sets conservative cache headers on the web UI so
// clients always receive fresh HTML and mutable assets, while long-cached
// hashed bundles under /assets/ stay immutable for efficiency.
func cacheControlMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/assets/"):
			// Vite bundles hashed filenames; they never change for a given URL.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			// Everything else (index.html, themes/*.css, favicons that share a
			// stable path) must re-validate on each load so upgrades land
			// immediately.
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		next.ServeHTTP(w, r)
	})
}

func (b *Broker) webUIProxyHandler(brokerURL, stripPrefix string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetPath := r.URL.Path
		if stripPrefix != "" {
			targetPath = strings.TrimPrefix(targetPath, stripPrefix)
		}
		if targetPath == "" {
			targetPath = "/"
		}
		target := brokerURL + targetPath
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		if err != nil {
			http.Error(w, "proxy error", http.StatusBadGateway)
			return
		}
		setProxyClientIPHeaders(proxyReq.Header, r.RemoteAddr)
		proxyReq.Header.Set("Authorization", "Bearer "+b.token)
		proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

		client := http.DefaultClient
		if r.Header.Get("Accept") == "text/event-stream" {
			client = &http.Client{Timeout: 0}
		}
		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, "broker unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		skipHeaders := responseHeadersToSkip(resp.Header)
		for k, v := range resp.Header {
			if _, skip := skipHeaders[strings.ToLower(k)]; skip {
				continue
			}
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(resp.StatusCode)

		if resp.Header.Get("Content-Type") == "text/event-stream" {
			flusher, canFlush := w.(http.Flusher)
			buf := make([]byte, 4096)
			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					w.Write(buf[:n]) //nolint:errcheck
					if canFlush {
						flusher.Flush()
					}
				}
				if readErr != nil {
					break
				}
			}
			return
		}
		_, _ = io.Copy(w, resp.Body)
	})
}

func responseHeadersToSkip(header http.Header) map[string]struct{} {
	skip := make(map[string]struct{}, len(hopByHopHeaders))
	for name := range hopByHopHeaders {
		skip[name] = struct{}{}
	}
	for _, token := range strings.Split(header.Get("Connection"), ",") {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" {
			skip[token] = struct{}{}
		}
	}
	return skip
}
