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
	"net/url"
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
	// broker is the in-process broker handle, set via SetBroker before the
	// controller's first start(). When non-nil and the broker has registered
	// a ShareTransport, start() routes invite creation through the adapter so
	// admit + revoke + invite-create all flow through the same surface. When
	// nil (e.g. the standalone `wuphf share` subcommand has no in-process
	// broker), start() falls back to the legacy HTTP path.
	broker *team.Broker
}

func newWebShareController(webPort int) *webShareController {
	return &webShareController{
		opts: shareOptions{
			webPort: webPort,
		},
	}
}

// SetBroker installs the in-process broker handle. Idempotent. Passing nil
// clears the handle so the controller falls back to the HTTP invite path.
// Called by main.go after the broker is up but before the controller's first
// start() invocation.
func (c *webShareController) SetBroker(b *team.Broker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.broker = b
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

func (c *webShareController) clearInviteLocked() {
	c.inviteURL = ""
	c.expiresAt = ""
}

// issueInviteLocked returns a fresh invite URL and its RFC3339 expiry. When an
// in-process broker handle with a registered ShareTransport is available it
// goes through the adapter (CreateInviteDetailed); otherwise it falls back to
// the legacy HTTP path against the broker. The absolute URL formula is
// identical in both branches so the user-facing link does not depend on which
// path produced it. Caller must hold c.mu.
//
// brokerTokenFn is invoked lazily — only when the HTTP fallback path is taken.
// This keeps the in-process adapter path independent of broker-token-file
// availability: a missing or unreadable token file no longer blocks invite
// creation when an adapter handle is registered. The fn shape (rather than a
// pre-read string) makes the lazy contract explicit at every call site.
func (c *webShareController) issueInviteLocked(ctx context.Context, bind string, port int, brokerURL string, brokerTokenFn func() (string, error)) (string, string, error) {
	if c.broker != nil {
		if st := c.broker.ShareTransport(); st != nil {
			st.SetURLBuilder(func(token string) string {
				return shareJoinURL(bind, port, token)
			})
			details, err := st.CreateInviteDetailed(ctx)
			if err != nil {
				return "", "", err
			}
			return details.URL, details.ExpiresAt, nil
		}
	}
	brokerToken, err := brokerTokenFn()
	if err != nil {
		return "", "", err
	}
	invite, err := createShareInvite(brokerURL, brokerToken)
	if err != nil {
		return "", "", err
	}
	return shareJoinURL(bind, port, invite.Token), invite.Invite.ExpiresAt, nil
}

// shareJoinURL is the canonical "http://<bind>:<port>/join/<token>" formatter
// used by both the adapter and HTTP invite paths so the user-facing URL shape
// stays in one place.
func shareJoinURL(bind string, port int, token string) string {
	return fmt.Sprintf("http://%s:%d/join/%s", bind, port, token)
}

func (c *webShareController) start() (team.WebShareStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	opts := c.opts
	if opts.webPort == 0 {
		opts.webPort = 7891
	}
	brokerURL := brokeraddr.ResolveBaseURL()

	if c.running && c.server != nil {
		// readBrokerToken is passed by reference so the running branch only
		// pays the file I/O when issueInviteLocked actually falls back to the
		// HTTP path (i.e. the adapter handle is missing). With the adapter
		// registered, a fresh invite mints with no token-file read.
		inviteURL, expiresAt, err := c.issueInviteLocked(context.Background(), c.bind, opts.webPort, brokerURL, readBrokerToken)
		if err != nil {
			c.err = err.Error()
			return c.statusLocked(), err
		}
		c.brokerURL = brokerURL
		c.inviteURL = inviteURL
		c.expiresAt = expiresAt
		c.err = ""
		return c.statusLocked(), nil
	}

	// Fresh start: the token is needed for newShareHTTPServer regardless of
	// which invite-creation path issueInviteLocked picks, so read it once here
	// and reuse the value via a constant closure for the invite path.
	token, err := readBrokerToken()
	if err != nil {
		c.err = err.Error()
		return c.statusLocked(), err
	}

	bind, iface, err := resolveShareBind(opts)
	if err != nil {
		c.running = false
		c.server = nil
		c.clearInviteLocked()
		c.err = webShareErrorMessage(err)
		return c.statusLocked(), err
	}
	inviteURL, expiresAt, err := c.issueInviteLocked(context.Background(), bind.String(), opts.webPort, brokerURL, func() (string, error) { return token, nil })
	if err != nil {
		c.running = false
		c.server = nil
		c.clearInviteLocked()
		c.err = err.Error()
		return c.statusLocked(), err
	}

	server := newShareHTTPServer(bind.String(), opts.webPort, brokerURL, token, nil)
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", server.Addr)
	if err != nil {
		c.running = false
		c.server = nil
		c.clearInviteLocked()
		c.err = fmt.Sprintf("share listener failed on %s: %v", server.Addr, err)
		return c.statusLocked(), errors.New(c.err)
	}

	c.server = server
	c.running = true
	c.bind = bind.String()
	c.iface = iface
	c.brokerURL = brokerURL
	c.inviteURL = inviteURL
	c.expiresAt = expiresAt
	c.err = ""

	go func() {
		err := server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.mu.Lock()
			if c.server == server {
				c.running = false
				c.server = nil
				c.clearInviteLocked()
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
	c.mu.Unlock()

	if server == nil {
		c.mu.Lock()
		c.running = false
		c.clearInviteLocked()
		c.err = ""
		c.mu.Unlock()
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
	c.mu.Lock()
	if c.server == server {
		c.server = nil
		c.running = false
		c.clearInviteLocked()
		c.err = ""
	}
	c.mu.Unlock()
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
	inviteURL := shareJoinURL(bind.String(), opts.webPort, invite.Token)
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
			if (want == "wireguard" || (want == "" && interfaceLooksLikeWireGuard(name))) && isPrivateIP(ip) {
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

func interfaceLooksLikeWireGuard(name string) bool {
	return strings.Contains(name, "wg") || strings.Contains(name, "wireguard") || strings.HasPrefix(name, "utun")
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
		switch r.Method {
		case http.MethodGet:
			// Redirect (rather than serve index.html under /join/) so the
			// SPA's relative asset URLs do not need a path rewrite.
			http.Redirect(w, r, "/?invite="+url.QueryEscape(token), http.StatusFound)
		case http.MethodPost:
			handleShareJoinSubmit(w, r, brokerURL, token, onJoin)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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

// shareJoinRedirect is the post-acceptance landing path for a freshly
// invited team member. Lives next to the broker exchange so renaming the
// route updates the response shape in one place.
const shareJoinRedirect = "/#/channels/general"

// shareJoinSuccess and shareJoinError mirror the joiner-side
// JoinInviteSuccess/JoinInviteFailure types in
// web/src/api/joinInvite.ts. Keep the JSON tags and the field set in sync
// with that file. Error codes are the closed set the React client
// recognises; anything else collapses to "unknown" client-side.
type shareJoinSuccess struct {
	OK          bool   `json:"ok"`
	Redirect    string `json:"redirect"`
	DisplayName string `json:"display_name"`
}

type shareJoinError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// maxShareJoinBodyBytes caps the unauthenticated POST body so a stranger
// holding only an invite link cannot exhaust memory by streaming gigabytes
// into the JSON decoder. 8 KiB is ample for a display_name payload.
const maxShareJoinBodyBytes = 8 << 10

func handleShareJoinSubmit(w http.ResponseWriter, r *http.Request, brokerURL, token string, onJoin func()) {
	var submission struct {
		DisplayName string `json:"display_name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxShareJoinBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&submission); err != nil {
		writeShareJoinError(w, http.StatusBadRequest, "invalid_request", "We could not read your invite submission. Reload and try again.")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeShareJoinError(w, http.StatusBadRequest, "invalid_request", "We could not read your invite submission. Reload and try again.")
		return
	}
	displayName := strings.TrimSpace(submission.DisplayName)
	if displayName == "" {
		displayName = "Team member"
	}
	payload, err := json.Marshal(map[string]string{
		"token":        token,
		"display_name": displayName,
		"device":       r.UserAgent(),
	})
	if err != nil {
		writeShareJoinError(w, http.StatusInternalServerError, "invalid_request", "WUPHF could not encode your invite submission.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, brokerURL+"/humans/invites/accept", bytes.NewReader(payload))
	if err != nil {
		writeShareJoinError(w, http.StatusBadGateway, "broker_unreachable", "WUPHF is not reachable from this invite. Ask the host to restart sharing.")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := shareHTTPClient.Do(req)
	if err != nil {
		writeShareJoinError(w, http.StatusBadGateway, "broker_unreachable", "WUPHF is not reachable from this invite. Ask the host to restart sharing.")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			writeShareJoinError(w, http.StatusGone, "invite_expired_or_used", "This invite is no longer valid. Ask the host for a new team-member invite.")
		case resp.StatusCode >= 500:
			writeShareJoinError(w, http.StatusBadGateway, "broker_failed", "WUPHF could not accept this invite right now. Ask the host to retry sharing.")
		default:
			writeShareJoinError(w, resp.StatusCode, "invite_invalid", "This invite could not be accepted. Ask the host for a fresh team-member invite.")
		}
		return
	}
	for _, cookie := range resp.Cookies() {
		http.SetCookie(w, cookie)
	}
	if onJoin != nil {
		onJoin()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(shareJoinSuccess{
		OK:          true,
		Redirect:    shareJoinRedirect,
		DisplayName: displayName,
	})
}

func writeShareJoinError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(shareJoinError{Error: code, Message: message})
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
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
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
			"working_dir":       "",
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
