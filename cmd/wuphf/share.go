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
	"time"

	wuphf "github.com/nex-crm/wuphf"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
)

const shareCookieName = "wuphf_human_session"

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
		fmt.Println("Waiting for co-founder to join...")
	}
	server := newShareHTTPServer(bind.String(), opts.webPort, brokerURL, token, func() {
		if !opts.jsonOut {
			fmt.Println("Co-founder joined. Open #general and work together.")
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
	resp, err := http.DefaultClient.Do(req)
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
		body := map[string]string{
			"token":        token,
			"display_name": "Co-founder",
			"device":       r.UserAgent(),
		}
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, brokerURL+"/humans/invites/accept", bytes.NewReader(raw))
		if err != nil {
			http.Error(w, "invite failed", http.StatusBadGateway)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, "broker unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
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
	resp, err := http.DefaultClient.Do(req)
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
		client := http.DefaultClient
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
