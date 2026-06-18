package team

// custom_app_dev.go — live dev-server preview for App Builder apps (SOTA, the
// dyad model adapted to our boundary; see docs/specs/app-builder-live-preview.md).
//
// Instead of waiting minutes for `bun run build` → single-file before showing
// anything, the broker runs a real Vite dev server per app and previews it live
// with HMR: edits hot-reload in milliseconds. The single-file build stays as the
// sealed "ship" artifact (register_app); this is purely the live preview.
//
// Security: the dev server runs the agent's own source on 127.0.0.1, but a
// generated app must still be unable to exfiltrate. We do NOT trust the app's
// own vite.config for that — the CSP is injected by a broker-owned reverse proxy
// (httputil.ReverseProxy, which also tunnels Vite's HMR WebSocket for free), so
// a generated config cannot strip it. The iframe loads the proxy origin (a
// distinct ephemeral 127.0.0.1 port), so allow-same-origin grants the app only
// its OWN origin, never the parent app's session. connect-src 'self' blocks all
// network except the same-origin HMR WS; data still flows only via the parent
// postMessage bridge.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	appDevIdleTimeout = 10 * time.Minute
	appDevBootLogMax  = 16 * 1024
	appDevGCInterval  = 1 * time.Minute
)

// appDevCSP builds the CSP the proxy injects on every dev response. connect-src
// is restricted to the SAME-ORIGIN HMR WebSocket only (the proxy port) — no
// wildcard `ws:`/`wss:` (exfil to any host) and no `'self'` for fetch/XHR (the
// app reads data only through the parent postMessage bridge, never the network;
// Vite loads modules via <script>, governed by script-src). Every other network
// destination is blocked.
func appDevCSP(proxyPort int) string {
	hmr := fmt.Sprintf("ws://127.0.0.1:%d wss://127.0.0.1:%d", proxyPort, proxyPort)
	return "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; " +
		"font-src 'self' data:; " +
		"media-src 'self' data: blob:; " +
		"connect-src " + hmr + "; " +
		"frame-src 'none'; " +
		"base-uri 'self'"
}

// appDevURLRe scrapes the local dev URL from Vite's "Local:" line specifically
// (capture group 1), not any URL anywhere in stdout — so a generated vite.config
// can't print a decoy "http://127.0.0.1:<attacker-port>" earlier and redirect
// the proxy's target to a server it controls.
var appDevURLRe = regexp.MustCompile(`(?i)Local:\s+(https?://(?:localhost|127\.0\.0\.1):\d+)`)

// cspMetaRe matches a <meta http-equiv="Content-Security-Policy" …> tag (any
// attribute order/quoting) so the proxy can strip it from served HTML.
var cspMetaRe = regexp.MustCompile(`(?is)<meta\b[^>]*http-equiv\s*=\s*["']?content-security-policy["']?[^>]*>`)

// appDevStatus is the FE-facing snapshot (GET /apps/{id}/dev[/status]).
type appDevStatus struct {
	Ready   bool   `json:"ready"`
	URL     string `json:"url,omitempty"`      // proxy origin to load in the iframe
	BootLog string `json:"boot_log,omitempty"` // streaming install/boot output
	Error   string `json:"error,omitempty"`
}

// appDevServer is one running app preview: a `bun run dev` process plus a
// broker-owned reverse proxy that fronts it (CSP injection + HMR WS passthrough).
type appDevServer struct {
	id     string
	srcDir string

	mu        sync.Mutex
	cmd       *exec.Cmd // current child (install, then dev) — killed on shutdown
	devURL    string    // scraped Vite origin, e.g. http://127.0.0.1:5173
	proxyPort int       // the origin the iframe loads
	proxySrv  *http.Server
	target    *url.URL
	proxy     *httputil.ReverseProxy // built once when ready (not per request)
	ready     bool
	exited    bool
	errMsg    string
	bootLog   appDevRing
	startedAt time.Time
	lastUsed  time.Time
}

// appDevManager owns the lifecycle of every running app dev server. Methods are
// safe for concurrent use; per-server state is guarded by the server's own mu.
type appDevManager struct {
	store   *customAppStore
	mu      sync.Mutex
	servers map[string]*appDevServer
	gcOnce  sync.Once
	stopGC  chan struct{}
}

func newAppDevManager(store *customAppStore) *appDevManager {
	return &appDevManager{
		store:   store,
		servers: make(map[string]*appDevServer),
		stopGC:  make(chan struct{}),
	}
}

// Ensure starts (or reuses) the dev server for an app and returns its current
// status. Booting is asynchronous: the first call returns ready=false with a
// streaming boot log; callers poll Status until ready, then load status.URL.
func (m *appDevManager) Ensure(id string) (appDevStatus, error) {
	if m == nil || m.store == nil {
		return appDevStatus{}, errors.New("app dev manager not initialised")
	}
	if err := validateCustomAppID(id); err != nil {
		return appDevStatus{}, err
	}
	srcDir := filepath.Join(m.store.appDir(id), customAppSourceDir)
	if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
		return appDevStatus{}, newCustomAppCallerError("app %q has no editable source to preview", id)
	}

	m.mu.Lock()
	srv, ok := m.servers[id]
	if ok && srv.alive() {
		srv.touch()
		m.mu.Unlock()
		return srv.status(), nil
	}
	srv = &appDevServer{id: id, srcDir: srcDir, startedAt: time.Now(), lastUsed: time.Now()}
	m.servers[id] = srv
	m.mu.Unlock()

	m.gcOnce.Do(func() { go m.gcLoop() })

	if err := srv.start(); err != nil {
		srv.fail(err.Error())
		return srv.status(), nil
	}
	go srv.run()
	return srv.status(), nil
}

// Status returns the current snapshot without starting anything.
func (m *appDevManager) Status(id string) (appDevStatus, bool) {
	m.mu.Lock()
	srv, ok := m.servers[id]
	m.mu.Unlock()
	if !ok {
		return appDevStatus{}, false
	}
	srv.touch()
	return srv.status(), true
}

// Stop tears down one app's dev server (process group + proxy).
func (m *appDevManager) Stop(id string) {
	m.mu.Lock()
	srv, ok := m.servers[id]
	if ok {
		delete(m.servers, id)
	}
	m.mu.Unlock()
	if ok {
		srv.shutdown()
	}
}

// StopAll tears down every running dev server (broker shutdown).
func (m *appDevManager) StopAll() {
	if m == nil {
		return
	}
	select {
	case <-m.stopGC:
	default:
		close(m.stopGC)
	}
	m.mu.Lock()
	servers := make([]*appDevServer, 0, len(m.servers))
	for _, srv := range m.servers {
		servers = append(servers, srv)
	}
	m.servers = make(map[string]*appDevServer)
	m.mu.Unlock()
	for _, srv := range servers {
		srv.shutdown()
	}
}

func (m *appDevManager) gcLoop() {
	ticker := time.NewTicker(appDevGCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopGC:
			return
		case <-ticker.C:
			now := time.Now()
			m.mu.Lock()
			var idle []*appDevServer
			for id, srv := range m.servers {
				if now.Sub(srv.lastUsedAt()) > appDevIdleTimeout {
					idle = append(idle, srv)
					delete(m.servers, id)
				}
			}
			m.mu.Unlock()
			for _, srv := range idle {
				srv.shutdown()
			}
		}
	}
}

// ── per-server lifecycle ────────────────────────────────────────────────────

// start allocates the proxy listener synchronously (so the iframe origin is
// known before the dev server — or even its install — is done) and serves it.
// The dev process is spawned in run().
func (s *appDevServer) start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("allocate preview port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveProxy)
	srv := &http.Server{Handler: mux}
	s.mu.Lock()
	s.proxyPort = port
	s.proxySrv = srv
	s.mu.Unlock()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// run installs dependencies if the project is cold, spawns the Vite dev server,
// scrapes its origin (flipping ready), and blocks until it exits. Runs in its
// own goroutine so Ensure returns immediately with a streaming boot log.
func (s *appDevServer) run() {
	if err := s.installIfNeeded(); err != nil {
		s.fail("dependency install failed: " + err.Error())
		s.markExited()
		return
	}
	s.runDev()
	s.markExited()
}

// installIfNeeded runs `bun install` when the installed tree is missing or stale
// (the dependency set changed since the last install), streaming to the boot
// log. On a warm, current tree it no-ops (the scaffold commits bun.lock so the
// install is fast/cache-resolved).
func (s *appDevServer) installIfNeeded() error {
	if devTreeFresh(s.srcDir) {
		return nil
	}
	cmd := exec.Command("bun", "install")
	cmd.Dir = s.srcDir
	configureHeadlessProcess(cmd)
	return s.pipeAndWait(cmd, nil)
}

// devTreeFresh reports whether node_modules exists AND is no older than the
// lockfile. A republish rewrites the source — including bun.lock — with a fresh
// mtime; when that is newer than the installed node_modules the dependency set
// changed (e.g. a plain-React app republished onto the refine + Mantine stack),
// so the tree MUST be reinstalled. Otherwise the dev server boots against the
// previous version's deps and the app fails to import its packages, surfacing as
// a blank live preview. An absent node_modules is never fresh; a missing
// lockfile leaves the existing tree usable.
func devTreeFresh(srcDir string) bool {
	nm, err := os.Stat(filepath.Join(srcDir, "node_modules"))
	if err != nil {
		return false
	}
	lock, err := os.Stat(filepath.Join(srcDir, "bun.lock"))
	if err != nil {
		return true
	}
	return !lock.ModTime().After(nm.ModTime())
}

// runDev spawns `bun run dev`, scrapes the Vite origin (flips ready), and waits.
func (s *appDevServer) runDev() {
	cmd := exec.Command("bun", "run", "dev")
	cmd.Dir = s.srcDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("WUPHF_HMR_CLIENT_PORT=%d", s.proxyPortValue()))
	configureHeadlessProcess(cmd)
	if err := s.pipeAndWait(cmd, s.onDevChunk); err != nil {
		s.mu.Lock()
		if !s.ready && s.errMsg == "" {
			s.errMsg = "dev server exited: " + err.Error()
		}
		s.mu.Unlock()
	}
}

// pipeAndWait runs cmd with stdout+stderr folded into the boot log; onChunk (if
// non-nil) is also called per chunk (used to scrape the dev URL). It records cmd
// as the current process (so shutdown can kill it), starts it, and blocks until
// it exits.
func (s *appDevServer) pipeAndWait(cmd *exec.Cmd, onChunk func([]byte)) error {
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return err
	}
	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := pr.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				s.appendBootLog(chunk)
				if onChunk != nil {
					onChunk(chunk)
				}
			}
			if rerr != nil {
				break
			}
		}
		close(done)
	}()
	werr := cmd.Wait()
	_ = pw.Close()
	<-done
	return werr
}

func (s *appDevServer) onDevChunk(_ []byte) {
	if s.isReady() {
		return
	}
	// Match against the ACCUMULATED log (the chunk was already appended) so a
	// "Local:" header and its URL split across read boundaries still match.
	s.mu.Lock()
	log := s.bootLog.String()
	s.mu.Unlock()
	if m := appDevURLRe.FindStringSubmatch(log); m != nil {
		s.setReady(m[1])
	}
}

// serveProxy fronts the Vite dev server, injecting the agent-proof CSP. Returns
// 503 while the dev server is still starting (target not yet scraped).
func (s *appDevServer) serveProxy(w http.ResponseWriter, r *http.Request) {
	// DNS-rebinding guard: the ephemeral preview proxy is loopback-only, so a
	// request whose Host isn't 127.0.0.1/localhost is a rebinding attempt.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	proxy := s.proxy
	s.lastUsed = time.Now()
	s.mu.Unlock()
	if proxy == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("preview starting…\n"))
		return
	}
	proxy.ServeHTTP(w, r)
}

// stripCSPMeta removes any CSP <meta> from an HTML response body so the proxy's
// header CSP stays authoritative (the browser would otherwise intersect a meta
// CSP with it). No-ops on non-HTML and on encoded bodies — Vite dev serves
// uncompressed, so that is the common path.
func stripCSPMeta(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil
	}
	if resp.Header.Get("Content-Encoding") != "" {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	cleaned := cspMetaRe.ReplaceAll(body, nil)
	resp.Body = io.NopCloser(bytes.NewReader(cleaned))
	resp.ContentLength = int64(len(cleaned))
	resp.Header.Set("Content-Length", strconv.Itoa(len(cleaned)))
	return nil
}

func (s *appDevServer) setReady(devURL string) {
	target, err := url.Parse(devURL)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if s.errMsg == "" {
			s.errMsg = "could not parse dev url: " + err.Error()
		}
		return
	}
	// Build the reverse proxy ONCE (not per request — that leaked a fresh
	// Transport + connection pool each time). The CSP is fixed for this server.
	csp := appDevCSP(s.proxyPort)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(resp *http.Response) error {
		// We own the CSP — a generated vite.config cannot weaken it. Strip any
		// CSP <meta> so the browser can't intersect it with (and break) ours.
		resp.Header.Set("Content-Security-Policy", csp)
		resp.Header.Del("X-Frame-Options")
		return stripCSPMeta(resp)
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, _ *http.Request, _ error) {
		rw.WriteHeader(http.StatusBadGateway)
	}
	s.devURL = devURL
	s.target = target
	s.proxy = proxy
	s.ready = true
}

func (s *appDevServer) appendBootLog(chunk []byte) {
	s.mu.Lock()
	s.bootLog.Write(chunk)
	s.mu.Unlock()
}

func (s *appDevServer) isReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

func (s *appDevServer) proxyPortValue() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proxyPort
}

func (s *appDevServer) fail(msg string) {
	s.mu.Lock()
	if s.errMsg == "" {
		s.errMsg = msg
	}
	s.mu.Unlock()
}

func (s *appDevServer) markExited() {
	s.mu.Lock()
	s.exited = true
	s.mu.Unlock()
}

func (s *appDevServer) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

func (s *appDevServer) lastUsedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastUsed
}

// alive reports whether the preview is still usable (not exited or failed).
func (s *appDevServer) alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.exited && s.errMsg == ""
}

func (s *appDevServer) status() appDevStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := appDevStatus{Ready: s.ready, BootLog: s.bootLog.String(), Error: s.errMsg}
	if s.ready && s.proxyPort > 0 {
		st.URL = fmt.Sprintf("http://127.0.0.1:%d/", s.proxyPort)
	}
	return st
}

func (s *appDevServer) shutdown() {
	s.mu.Lock()
	srv := s.proxySrv
	cmd := s.cmd
	s.proxySrv = nil
	s.exited = true
	s.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	terminateHeadlessProcess(cmd)
}

// ── appDevRing: a small capped boot-log buffer ──────────────────────────────

type appDevRing struct {
	buf []byte
}

func (r *appDevRing) Write(p []byte) {
	r.buf = append(r.buf, p...)
	if len(r.buf) > appDevBootLogMax {
		r.buf = r.buf[len(r.buf)-appDevBootLogMax:]
	}
}

func (r *appDevRing) String() string {
	return strings.TrimRight(string(r.buf), "\x00")
}
