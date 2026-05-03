package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	wuphf "github.com/nex-crm/wuphf"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/team"
)

const shareCookieName = "wuphf_human_session"

const shareBrokerRequestTimeout = 30 * time.Second

var shareHTTPClient = &http.Client{Timeout: shareBrokerRequestTimeout}

type shareOptions struct {
	bind             string
	webPort          int
	jsonOut          bool
	stop             bool
	unsafeLAN        bool
	unsafePublicBind bool
}

type shareInviteResponse struct {
	Invite struct {
		ID        string `json:"id"`
		ExpiresAt string `json:"expires_at"`
	} `json:"invite"`
	Token string `json:"token"`
}

type shareJSONOutput struct {
	OK        bool   `json:"ok"`
	Bind      string `json:"bind,omitempty"`
	Interface string `json:"interface,omitempty"`
	InviteURL string `json:"invite_url,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

type webShareController struct {
	mu        sync.Mutex
	opts      shareOptions
	server    *http.Server
	running   bool
	bind      string
	iface     string
	brokerURL string
	inviteURL string
	expiresAt string
	err       string
}

func newWebShareController(webPort int) *webShareController {
	return &webShareController{
		opts: shareOptions{
			bind:    "tailscale",
			webPort: webPort,
		},
	}
}

func (c *webShareController) status() team.WebShareStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

func (c *webShareController) statusLocked() team.WebShareStatus {
	return team.WebShareStatus{
		Running:   c.running,
		Bind:      c.bind,
		Interface: c.iface,
		InviteURL: c.inviteURL,
		ExpiresAt: c.expiresAt,
		Error:     c.err,
	}
}

func (c *webShareController) start() (team.WebShareStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	opts := c.opts
	if opts.bind == "" {
		opts.bind = "tailscale"
	}
	if opts.webPort == 0 {
		opts.webPort = 7891
	}
	token, err := readBrokerToken()
	if err != nil {
		c.err = err.Error()
		return c.statusLocked(), err
	}
	brokerURL := brokeraddr.ResolveBaseURL()

	if c.running && c.server != nil {
		invite, err := createShareInvite(brokerURL, token)
		if err != nil {
			c.err = err.Error()
			return c.statusLocked(), err
		}
		c.brokerURL = brokerURL
		c.inviteURL = fmt.Sprintf("http://%s:%d/join/%s", c.bind, opts.webPort, invite.Token)
		c.expiresAt = invite.Invite.ExpiresAt
		c.err = ""
		return c.statusLocked(), nil
	}

	bind, iface, err := resolveShareBind(opts)
	if err != nil {
		c.running = false
		c.server = nil
		c.err = webShareErrorMessage(err)
		return c.statusLocked(), err
	}
	invite, err := createShareInvite(brokerURL, token)
	if err != nil {
		c.running = false
		c.server = nil
		c.err = err.Error()
		return c.statusLocked(), err
	}

	server := newShareHTTPServer(bind.String(), opts.webPort, brokerURL, token, nil)
	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		c.running = false
		c.server = nil
		c.err = fmt.Sprintf("share listener failed on %s: %v", server.Addr, err)
		return c.statusLocked(), errors.New(c.err)
	}

	c.server = server
	c.running = true
	c.bind = bind.String()
	c.iface = iface
	c.brokerURL = brokerURL
	c.inviteURL = fmt.Sprintf("http://%s:%d/join/%s", c.bind, opts.webPort, invite.Token)
	c.expiresAt = invite.Invite.ExpiresAt
	c.err = ""

	go func() {
		err := server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.mu.Lock()
			if c.server == server {
				c.running = false
				c.err = err.Error()
			}
			c.mu.Unlock()
		}
	}()

	return c.statusLocked(), nil
}

func webShareErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "no private network interface found") {
		return "No private network interface found.\n\nWUPHF only creates team-member invites over a private network by default. Start Tailscale or WireGuard, then click Create invite again."
	}
	if strings.Contains(msg, "no WireGuard interface found") {
		return "No WireGuard interface found.\n\nStart WireGuard, then click Create invite again."
	}
	return msg
}

func (c *webShareController) stop() error {
	c.mu.Lock()
	server := c.server
	c.server = nil
	c.running = false
	c.inviteURL = ""
	c.expiresAt = ""
	c.err = ""
	c.mu.Unlock()

	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		c.mu.Lock()
		c.err = err.Error()
		c.mu.Unlock()
		return err
	}
	return nil
}

func runShare(args []string) {
	opts := shareOptions{}
	fs := flag.NewFlagSet("share", flag.ContinueOnError)
	fs.StringVar(&opts.bind, "bind", "tailscale", "Private address or interface preference (tailscale, wireguard, or 100.x.y.z)")
	fs.IntVar(&opts.webPort, "web-port", 7891, "Port for the shared web UI")
	fs.BoolVar(&opts.jsonOut, "json", false, "Emit invite details as JSON")
	fs.BoolVar(&opts.stop, "stop", false, "Stop accepting new invite traffic (reserved)")
	fs.BoolVar(&opts.unsafeLAN, "unsafe-lan", false, "Allow RFC1918 LAN addresses with an explicit warning")
	fs.BoolVar(&opts.unsafePublicBind, "unsafe-public-bind", false, "Allow a public bind address (internal, unsafe)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printSubcommandHelp("share") }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}
	if opts.stop {
		fmt.Fprintln(os.Stderr, "error: `wuphf share --stop` is not active because share runs in the foreground. Press Ctrl+C in the share terminal.")
		os.Exit(1)
	}
	if err := runShareServer(opts); err != nil {
		if opts.jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(shareJSONOutput{OK: false, Error: err.Error()})
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runShareServer(opts shareOptions) error {
	token, err := readBrokerToken()
	if err != nil {
		return err
	}
	bind, iface, err := resolveShareBind(opts)
	if err != nil {
		return err
	}
	brokerURL := brokeraddr.ResolveBaseURL()
	invite, err := createShareInvite(brokerURL, token)
	if err != nil {
		return err
	}
	inviteURL := fmt.Sprintf("http://%s:%d/join/%s", bind.String(), opts.webPort, invite.Token)
	out := shareJSONOutput{
		OK:        true,
		Bind:      bind.String(),
		Interface: iface,
		InviteURL: inviteURL,
		ExpiresAt: invite.Invite.ExpiresAt,
	}
	if opts.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(out)
	} else {
		fmt.Println("WUPHF share")
		fmt.Printf("Private network: %s %s\n", iface, bind.String())
		if opts.unsafeLAN {
			fmt.Println("Public bind: blocked; LAN override enabled")
		} else if opts.unsafePublicBind {
			fmt.Println("Public bind: unsafe override enabled")
		} else {
			fmt.Println("Public bind: blocked")
		}
		fmt.Printf("Invite: %s\n", inviteURL)
		fmt.Println("Expires: 24h, one use")
		fmt.Println("Waiting for team member to join...")
	}
	server := newShareHTTPServer(bind.String(), opts.webPort, brokerURL, token, func() {
		if !opts.jsonOut {
			fmt.Println("Team member joined. Open #general and work together.")
		}
	})
	return server.ListenAndServe()
}

func readBrokerToken() (string, error) {
	path := brokeraddr.ResolveTokenFile()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("WUPHF broker is not running. Start it with `wuphf`, then run `wuphf share`")
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("broker token file is empty: %s", path)
	}
	return token, nil
}

func createShareInvite(brokerURL, token string) (shareInviteResponse, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, brokerURL+"/humans/invites", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return shareInviteResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := shareHTTPClient.Do(req)
	if err != nil {
		return shareInviteResponse{}, fmt.Errorf("broker unreachable at %s", brokerURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return shareInviteResponse{}, fmt.Errorf("create invite failed: %s", strings.TrimSpace(string(body)))
	}
	var out shareInviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return shareInviteResponse{}, err
	}
	if out.Token == "" {
		return shareInviteResponse{}, fmt.Errorf("create invite response did not include a token")
	}
	return out, nil
}

func resolveShareBind(opts shareOptions) (net.IP, string, error) {
	want := strings.ToLower(strings.TrimSpace(opts.bind))
	if ip := net.ParseIP(want); ip != nil {
		if err := validateShareIP(ip, opts); err != nil {
			return nil, "", err
		}
		return ip, "manual", nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if want == "tailscale" && !strings.Contains(name, "tailscale") && !strings.HasPrefix(name, "ts") && !strings.HasPrefix(name, "utun") {
			continue
		}
		if want == "wireguard" && !strings.Contains(name, "wg") && !strings.Contains(name, "wireguard") && !strings.HasPrefix(name, "utun") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if ip == nil || ip.To4() == nil {
				continue
			}
			if want == "tailscale" && !isTailscaleIP(ip) {
				continue
			}
			validateOpts := opts
			if want == "wireguard" && isPrivateIP(ip) {
				validateOpts.unsafeLAN = true
			}
			if err := validateShareIP(ip, validateOpts); err != nil {
				continue
			}
			return ip, iface.Name, nil
		}
	}
	if want == "tailscale" {
		return nil, "", fmt.Errorf("no private network interface found\n\nWUPHF only shares over a private network by default.\nFix: start Tailscale, then run `wuphf share` again.\nOverride: `wuphf share --bind 100.x.y.z`")
	}
	if want == "wireguard" {
		return nil, "", fmt.Errorf("no WireGuard interface found\n\nFix: start WireGuard, then run `wuphf share --bind wireguard` again")
	}
	return nil, "", fmt.Errorf("no usable private interface found")
}

func validateShareIP(ip net.IP, opts shareOptions) error {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return fmt.Errorf("refusing to expose WUPHF on %s", ip)
	}
	if isTailscaleIP(ip) || isPrivateIP(ip) && opts.unsafeLAN || opts.unsafePublicBind {
		return nil
	}
	if isPrivateIP(ip) {
		return fmt.Errorf("refusing LAN address %s without --unsafe-lan", ip)
	}
	return fmt.Errorf("refusing to expose WUPHF on a public interface\n\nDetected: %s\nFix: use a Tailscale/WireGuard address or keep WUPHF local.\nUnsafe local-LAN escape hatch: `wuphf share --unsafe-lan`", ip)
}

func ipFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func isTailscaleIP(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}

func isPrivateIP(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return ip.IsPrivate()
	}
	return v4[0] == 10 || (v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31) || (v4[0] == 192 && v4[1] == 168)
}

func newShareHTTPServer(bind string, port int, brokerURL, brokerToken string, onJoin func()) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", bind, port),
		Handler:           newShareHandler(brokerURL, brokerToken, onJoin),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func newShareHandler(brokerURL, brokerToken string, onJoin func()) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/join/", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, "/join/")
		if token == "" {
			http.Error(w, "invite token required", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodGet {
			writeShareJoinPage(w, token, "")
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			writeShareJoinPage(w, token, "Could not read the form. Reload the invite and try again.")
			return
		}
		displayName := strings.TrimSpace(r.FormValue("display_name"))
		if displayName == "" {
			displayName = "Team member"
		}
		body := map[string]string{
			"token":        token,
			"display_name": displayName,
			"device":       r.UserAgent(),
		}
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, brokerURL+"/humans/invites/accept", bytes.NewReader(raw))
		if err != nil {
			http.Error(w, "invite failed", http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := shareHTTPClient.Do(req)
		if err != nil {
			writeShareJoinPage(w, token, "WUPHF is not reachable from this invite. Ask the host to restart sharing.")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			writeShareJoinPage(w, token, "This invite is no longer valid. Ask the host for a new team-member invite.")
			return
		}
		for _, cookie := range resp.Cookies() {
			http.SetCookie(w, cookie)
		}
		if onJoin != nil {
			onJoin()
		}
		http.Redirect(w, r, "/#/channels/general", http.StatusFound)
	})
	mux.HandleFunc("/api-token", func(w http.ResponseWriter, r *http.Request) {
		if !shareRequestHasSession(r, brokerURL) {
			http.Error(w, `{"error":"session_required"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "", "broker_url": ""})
	})
	mux.Handle("/api/", shareProxyHandler(brokerURL))
	mux.Handle("/", shareStaticHandler())
	return mux
}

func writeShareJoinPage(w http.ResponseWriter, token, errorMessage string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	errorHTML := ""
	if strings.TrimSpace(errorMessage) != "" {
		errorHTML = fmt.Sprintf(`<div class="error">%s</div>`, htmlEscape(errorMessage))
	}
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Join WUPHF</title>
  <style>
    :root { color-scheme: light; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #f8f8f9; color: #28292a; }
    main { width: min(92vw, 460px); background: white; border: 1px solid #e9eaeb; border-radius: 16px; padding: 28px; box-shadow: 0 24px 80px rgba(30, 31, 31, 0.08); }
    .eyebrow { margin: 0 0 10px; color: #686c6e; font-size: 12px; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
    h1 { margin: 0 0 10px; font-size: 28px; line-height: 1.1; letter-spacing: 0; }
    p { margin: 0 0 20px; color: #575a5c; line-height: 1.55; }
    label { display: block; margin: 0 0 8px; font-size: 13px; font-weight: 700; }
    input { width: 100%%; min-height: 46px; border: 1px solid #cfd1d2; border-radius: 10px; padding: 0 12px; font: inherit; }
    button { width: 100%%; min-height: 46px; margin-top: 14px; border: 0; border-radius: 999px; background: #28292a; color: white; font: inherit; font-weight: 700; cursor: pointer; }
    .note { margin-top: 18px; font-size: 12px; color: #85898b; }
    .error { margin: 0 0 16px; padding: 10px 12px; border-radius: 10px; background: #ffeeeb; color: #8c1727; font-size: 13px; }
  </style>
</head>
<body>
  <main>
    <p class="eyebrow">Team member invite</p>
    <h1>Join this WUPHF office</h1>
    <p>Use the name your teammate should see in messages, requests, and office activity.</p>
    %s
    <form method="post" action="/join/%s">
      <label for="display_name">Display name</label>
      <input id="display_name" name="display_name" autocomplete="name" placeholder="e.g. Maya" autofocus>
      <button type="submit">Enter office</button>
    </form>
    <p class="note">This creates a scoped team-member browser session. It does not expose the host's broker token.</p>
  </main>
</body>
</html>`, errorHTML, htmlEscape(token))
}

func htmlEscape(s string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return replacer.Replace(s)
}

func shareRequestHasSession(r *http.Request, brokerURL string) bool {
	cookie, err := r.Cookie(shareCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, brokerURL+"/humans/me", nil)
	if err != nil {
		return false
	}
	req.AddCookie(cookie)
	resp, err := shareHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func shareProxyHandler(brokerURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shareRequestHasSession(r, brokerURL) {
			http.Error(w, `{"error":"session_required"}`, http.StatusUnauthorized)
			return
		}
		targetPath := strings.TrimPrefix(r.URL.Path, "/api")
		if targetPath == "" {
			targetPath = "/"
		}
		if targetPath == "/onboarding/state" {
			writeShareProxyJSON(w, http.StatusOK, map[string]bool{"onboarded": true})
			return
		}
		if handled := writeShareSyntheticHostOnlyResponse(w, r, targetPath); handled {
			return
		}
		if !shareProxyPathAllowed(targetPath) {
			http.Error(w, `{"error":"host_only"}`, http.StatusForbidden)
			return
		}
		target := brokerURL + targetPath
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		if err != nil {
			http.Error(w, "proxy error", http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
		req.Header.Set("Cookie", r.Header.Get("Cookie"))
		client := shareHTTPClient
		if r.Header.Get("Accept") == "text/event-stream" {
			client = &http.Client{Timeout: 0}
		}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "broker unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, values := range resp.Header {
			if strings.EqualFold(k, "Connection") || strings.EqualFold(k, "Transfer-Encoding") {
				continue
			}
			for _, value := range values {
				w.Header().Add(k, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if resp.Header.Get("Content-Type") == "text/event-stream" {
			flusher, _ := w.(http.Flusher)
			buf := make([]byte, 4096)
			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					_, _ = w.Write(buf[:n])
					if flusher != nil {
						flusher.Flush()
					}
				}
				if readErr != nil {
					return
				}
			}
		}
		_, _ = io.Copy(w, resp.Body)
	})
}

func writeShareSyntheticHostOnlyResponse(w http.ResponseWriter, r *http.Request, path string) bool {
	switch path {
	case "/upgrade-check":
		if r.Method != http.MethodGet {
			break
		}
		writeShareProxyJSON(w, http.StatusOK, map[string]any{
			"current":           "dev",
			"latest":            "dev",
			"upgrade_available": false,
			"is_dev_build":      true,
			"upgrade_command":   "",
			"install_method":    "unknown",
			"install_command":   "",
		})
		return true
	case "/workspaces/list":
		if r.Method != http.MethodGet {
			break
		}
		writeShareProxyJSON(w, http.StatusOK, map[string]any{
			"workspaces": []any{},
		})
		return true
	case "/config":
		if r.Method != http.MethodGet {
			break
		}
		writeShareProxyJSON(w, http.StatusOK, map[string]any{
			"llm_provider":   "claude-code",
			"memory_backend": "markdown",
			"team_lead_slug": "ceo",
		})
		return true
	}
	return false
}

func writeShareProxyJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func shareProxyPathAllowed(path string) bool {
	path = "/" + strings.TrimLeft(path, "/")
	if path == "/web-token" {
		return false
	}
	for _, prefix := range []string{
		"/admin/",
		"/workspaces/",
		"/workspace/",
		"/upgrade",
		"/reset",
		"/config",
		"/nex/register",
		"/image-providers",
		"/notebook/",
		"/humans/invites",
		"/humans/sessions",
	} {
		if path == strings.TrimRight(prefix, "/") || strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

func shareStaticHandler() http.Handler {
	exePath, _ := os.Executable()
	webDir := filepath.Join(filepath.Dir(exePath), "web")
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}
	distDir := filepath.Join(webDir, "dist")
	if _, err := os.Stat(filepath.Join(distDir, "index.html")); err == nil {
		return http.FileServer(http.Dir(distDir))
	}
	if embeddedFS, ok := wuphf.WebFS(); ok {
		return http.FileServer(http.FS(embeddedFS))
	}
	return http.FileServer(http.Dir(webDir))
}
