package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/team"
)

// cloudflaredURLPattern matches the public hostname cloudflared emits to
// stderr after spinning up a TryCloudflare tunnel. The hostname is a hyphen-
// separated set of lowercase words plus digits, e.g.
// "https://winter-soft-banana-42.trycloudflare.com". Anchoring on the scheme
// avoids matching the bare "trycloudflare.com" the binary prints in
// onboarding banners with no URL attached.
var cloudflaredURLPattern = regexp.MustCompile(`https://[a-z0-9][a-z0-9-]*\.trycloudflare\.com`)

// cloudflaredStartTimeout is how long start() waits for cloudflared to print
// a public URL before considering the launch failed. 45s is generous —
// Cloudflare's bring-up usually completes in 5–15s but home networks behind
// CGNAT can take longer to negotiate the QUIC path.
const cloudflaredStartTimeout = 45 * time.Second

// cloudflaredStopTimeout is the grace period for a clean Shutdown before we
// SIGKILL.
const cloudflaredStopTimeout = 5 * time.Second

// cloudflaredMissingMessage is the user-facing error when the binary is not
// next to the wuphf executable AND not on PATH. The npm postinstall ships
// cloudflared into the same directory as the wuphf binary so this path
// almost never fires for npm users; it shows up for `go install` users and
// for npm users whose corp proxy blocked the github.com download. The
// recovery hint covers both: reinstall from npm (refreshes the bundle), or
// install cloudflared via the platform package manager.
func cloudflaredMissingMessage() string {
	var manual string
	switch runtime.GOOS {
	case "darwin":
		manual = "  brew install cloudflared"
	case "windows":
		manual = "  winget install --id Cloudflare.cloudflared"
	default:
		manual = "  See https://github.com/cloudflare/cloudflared#installing-cloudflared"
	}
	return "cloudflared is not installed.\n\n" +
		"WUPHF normally bundles cloudflared with the npm install. If you see this,\n" +
		"either the bundle download was blocked (corp proxy / offline install) or\n" +
		"you installed wuphf via `go install` and we did not stage it.\n\n" +
		"Fix: reinstall wuphf with `npm install -g wuphf@latest`, or install\n" +
		"cloudflared manually:\n\n" +
		manual + "\n\n" +
		"Then click \"Start public tunnel\" again."
}

// findCloudflared returns the path to a usable cloudflared binary. Search
// order is: bundled-next-to-wuphf, then PATH. The bundled lookup is first
// because npm postinstall stages a SHA256-verified release into the same
// directory as the wuphf binary, and a system-installed cloudflared on
// PATH may be older / unsigned / config-clobbered. Returns the empty
// string + non-nil error when neither location resolves.
func findCloudflared() (string, error) {
	binaryName := "cloudflared"
	if runtime.GOOS == "windows" {
		binaryName = "cloudflared.exe"
	}
	if exe, err := os.Executable(); err == nil {
		// Resolve symlinks (e.g. brew links wuphf to opt/wuphf/bin/wuphf
		// from a Cellar path) so the sibling lookup hits the real install
		// dir, not the link tree.
		if real, errEval := filepath.EvalSymlinks(exe); errEval == nil {
			exe = real
		}
		candidate := filepath.Join(filepath.Dir(exe), binaryName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("cloudflared"); err == nil {
		return path, nil
	}
	return "", errors.New("cloudflared not found")
}

// webTunnelController owns the cloudflared subprocess and a loopback share
// HTTP server it forwards to. Lifecycle is host-only and serialized by mu.
// start() is idempotent — re-clicking after a successful start mints a fresh
// invite without restarting cloudflared — and stop() tears everything down
// so a subsequent start re-runs cleanly.
type webTunnelController struct {
	mu       sync.Mutex
	binary   string // override path for tests; falls back to PATH lookup
	server   *http.Server
	listener net.Listener
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	// stopGuard is closed by stop() to signal the wait-goroutine that the
	// teardown was intentional and it should not flip running/err state.
	stopGuard chan struct{}
	running   bool
	// starting is true between the moment start() commits the in-flight
	// cmd/server/listener under c.mu and the moment waitForTunnelURL
	// completes. Concurrent start() callers see it and refuse rather than
	// spawning a second cloudflared. stop() observes c.cmd != nil and
	// nulls everything as usual; the in-flight start() detects the null
	// when it re-acquires the lock and returns cancelled.
	starting  bool
	publicURL string
	inviteURL string
	// inviteToken is the token portion of inviteURL (the bit after /join/).
	// Tracked separately so the join handler can identify the most-recent
	// invite without re-parsing the URL on every request.
	inviteToken string
	// passcode is the second factor displayed next to inviteURL. Rotates
	// with each minted invite; the gate accepts only this passcode for
	// the current inviteToken.
	passcode string
	// passcodes maps every still-redeemable invite token to its passcode.
	// Old tokens linger here until the tunnel is stopped — that is fine
	// because (a) the broker independently expires tokens after 24h and
	// (b) joiners who already grabbed a stale URL may legitimately submit
	// before the host clicks "Create new invite".
	passcodes   map[string]string
	expiresAt   string
	err         string
	missing     bool
	broker      *team.Broker
	rateLimiter *joinRateLimiter
}

func newWebTunnelController() *webTunnelController {
	return &webTunnelController{
		passcodes:   make(map[string]string),
		rateLimiter: newJoinRateLimiter(),
	}
}

// SetBroker installs the in-process broker handle. Required before start():
// the tunnel uses ShareTransport to mint invite tokens the same way the
// private-network share path does, so admit/revoke flow through one surface.
func (c *webTunnelController) SetBroker(b *team.Broker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.broker = b
}

func (c *webTunnelController) status() team.WebTunnelStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statusLocked()
}

func (c *webTunnelController) statusLocked() team.WebTunnelStatus {
	return team.WebTunnelStatus{
		Running:            c.running,
		PublicURL:          c.publicURL,
		InviteURL:          c.inviteURL,
		Passcode:           c.passcode,
		ExpiresAt:          c.expiresAt,
		Error:              c.err,
		CloudflaredMissing: c.missing,
	}
}

func (c *webTunnelController) clearInviteLocked() {
	c.inviteURL = ""
	c.inviteToken = ""
	c.passcode = ""
	c.expiresAt = ""
}

func (c *webTunnelController) start() (team.WebTunnelStatus, error) {
	c.mu.Lock()

	// Re-clicking "Start public tunnel" while one is already up should mint a
	// fresh invite against the existing public URL rather than tearing the
	// tunnel down and back up — same shape as webShareController.start().
	if c.running && c.cmd != nil && c.publicURL != "" {
		defer c.mu.Unlock()
		inviteURL, passcode, expiresAt, err := c.mintInviteLocked(c.publicURL)
		if err != nil {
			c.err = err.Error()
			return c.statusLocked(), err
		}
		c.inviteURL = inviteURL
		c.passcode = passcode
		c.expiresAt = expiresAt
		c.err = ""
		return c.statusLocked(), nil
	}

	// Serialize concurrent starts: a second click while cloudflared is
	// still coming up MUST NOT spawn a second subprocess.
	if c.starting {
		defer c.mu.Unlock()
		return c.statusLocked(), errors.New("tunnel start is already in progress; wait or click Stop to cancel")
	}

	if c.broker == nil {
		defer c.mu.Unlock()
		err := errors.New("tunnel controller has no broker handle")
		c.err = err.Error()
		return c.statusLocked(), err
	}

	binary := c.binary
	if binary == "" {
		path, lookErr := findCloudflared()
		if lookErr != nil {
			defer c.mu.Unlock()
			c.missing = true
			c.err = cloudflaredMissingMessage()
			c.running = false
			return c.statusLocked(), errors.New(c.err)
		}
		binary = path
	}
	c.missing = false

	// Loopback listener on a random port. cloudflared dials this address
	// outbound; the fact that we never bind to a routable interface is what
	// keeps the service local-only when the tunnel is stopped.
	listenCfg := &net.ListenConfig{}
	ln, err := listenCfg.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		defer c.mu.Unlock()
		c.err = fmt.Sprintf("tunnel loopback listener failed: %v", err)
		return c.statusLocked(), errors.New(c.err)
	}
	loopbackAddr := ln.Addr().String()
	brokerURL := brokeraddr.ResolveBaseURL()
	brokerToken, err := readBrokerToken()
	if err != nil {
		_ = ln.Close()
		defer c.mu.Unlock()
		c.err = err.Error()
		return c.statusLocked(), err
	}
	// brokerToken is read above only because readBrokerToken's failure mode
	// is the same one we want to surface here (broker not running). The
	// share HTTP handler itself doesn't need the token — see the doc on
	// shareHandlerConfig.
	_ = brokerToken
	server := &http.Server{
		Addr: loopbackAddr,
		Handler: newShareHandler(shareHandlerConfig{
			BrokerURL:   brokerURL,
			OnJoin:      nil,
			JoinGate:    c.joinGate,
			RateLimiter: c.rateLimiter,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary,
		"tunnel",
		"--no-autoupdate",
		"--url", "http://"+loopbackAddr,
		"--metrics", "127.0.0.1:0",
	)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		_ = ln.Close()
		defer c.mu.Unlock()
		c.err = fmt.Sprintf("cloudflared stderr pipe failed: %v", err)
		return c.statusLocked(), errors.New(c.err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = ln.Close()
		defer c.mu.Unlock()
		c.err = fmt.Sprintf("cloudflared stdout pipe failed: %v", err)
		return c.statusLocked(), errors.New(c.err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = ln.Close()
		defer c.mu.Unlock()
		c.err = fmt.Sprintf("cloudflared failed to start: %v", err)
		return c.statusLocked(), errors.New(c.err)
	}

	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.server == server {
				c.err = fmt.Sprintf("tunnel loopback server failed: %v", err)
				c.running = false
				c.clearInviteLocked()
				c.publicURL = ""
			}
		}
	}()

	urlCh := make(chan string, 1)
	tailCh := make(chan []string, 1)
	go scanCloudflaredOutput(stderr, urlCh, tailCh)
	// Drain stdout so a chatty cloudflared can't stall on a full pipe;
	// recent versions emit little here, so noop the bytes.
	go func() { _, _ = io.Copy(io.Discard, stdout) }()

	// Commit the in-flight resources to c.* under the lock so a concurrent
	// stop() can find and tear them down. Then release the lock around the
	// (up to 45s) waitForTunnelURL call so status() polls and stop() clicks
	// from the UI don't block on c.mu.
	c.starting = true
	c.cmd = cmd
	c.cancel = cancel
	c.server = server
	c.listener = ln
	c.mu.Unlock()

	publicURL, tail, perr := waitForTunnelURL(ctx, urlCh, tailCh, cloudflaredStartTimeout)

	c.mu.Lock()
	c.starting = false

	// stop() observed the in-flight resources during the wait and tore
	// them down. Don't double-free; just report cancelled.
	if c.cmd != cmd {
		defer c.mu.Unlock()
		return c.statusLocked(), errors.New("tunnel start was cancelled")
	}

	if perr != nil {
		// Wait failed (timeout, cmd died early, ctx cancelled). Cancel,
		// then release c.mu before the cmd.Wait + ln.Close cleanup so a
		// hung pipe drain can't block status() callers under the same
		// lock-window invariant the watcher goroutine already follows.
		cancel()
		c.cmd = nil
		c.cancel = nil
		cleanupServer := c.server
		c.server = nil
		cleanupLn := c.listener
		c.listener = nil
		c.running = false
		msg := perr.Error()
		if len(tail) > 0 {
			msg += "\n\nLast cloudflared output:\n" + strings.Join(tail, "\n")
		}
		c.err = msg
		c.mu.Unlock()
		_ = cmd.Wait()
		if cleanupServer != nil {
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cloudflaredStopTimeout)
			_ = cleanupServer.Shutdown(shutdownCtx)
			cancelShutdown()
		}
		if cleanupLn != nil {
			_ = cleanupLn.Close()
		}
		return team.WebTunnelStatus{Error: msg}, errors.New(msg)
	}

	c.publicURL = publicURL
	c.running = true
	c.stopGuard = make(chan struct{})
	log.Printf("tunnel: cloudflared up at %s", publicURL)

	// Mint the first invite against the freshly-published public URL.
	inviteURL, passcode, expiresAt, ierr := c.mintInviteLocked(publicURL)
	if ierr != nil {
		// Tunnel is up but the invite call failed. Surface the error but
		// keep the tunnel running — a retry can mint a new invite without
		// re-spinning cloudflared.
		defer c.mu.Unlock()
		c.err = ierr.Error()
		return c.statusLocked(), ierr
	}
	c.inviteURL = inviteURL
	c.passcode = passcode
	c.expiresAt = expiresAt
	c.err = ""

	// Watch the subprocess for unexpected exit. If it crashes, flip Running
	// off so the next status poll surfaces the failure.
	stopGuard := c.stopGuard
	go func() {
		err := cmd.Wait()
		select {
		case <-stopGuard:
			// stop() already handled teardown; nothing to do.
			return
		default:
		}
		c.mu.Lock()
		if c.cmd != cmd {
			c.mu.Unlock()
			return
		}
		// Capture server/listener before releasing the lock; do the
		// blocking Shutdown OUTSIDE the lock so a hung in-flight
		// loopback connection cannot freeze status() / start() / stop()
		// callers waiting on c.mu for the full cloudflaredStopTimeout
		// window.
		server := c.server
		c.running = false
		c.cmd = nil
		c.cancel = nil
		c.publicURL = ""
		c.server = nil
		c.listener = nil
		c.passcodes = make(map[string]string)
		c.clearInviteLocked()
		if err != nil && !errors.Is(err, context.Canceled) {
			c.err = fmt.Sprintf("cloudflared exited unexpectedly: %v", err)
		}
		c.mu.Unlock()
		if server != nil {
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cloudflaredStopTimeout)
			_ = server.Shutdown(shutdownCtx)
			cancelShutdown()
		}
	}()

	defer c.mu.Unlock()
	return c.statusLocked(), nil
}

// mintInviteLocked issues a fresh invite token via the registered share
// transport, generates a one-off passcode for it, and formats the joiner-
// facing URL against the tunnel's public origin. Side-effect: registers
// (token -> passcode) in c.passcodes so the share-handler joinGate can
// verify subsequent /join POSTs. Caller must hold c.mu.
//
// Returns (inviteURL, passcode, expiresAt, err).
//
// Uses CreateInviteDetailedWithBuilder so the tunnel-URL builder is bound
// atomically to this single invite-creation — the network-share path can
// run a parallel call against the same ShareTransport without overwriting
// our builder mid-flight. SetURLBuilder is intentionally NOT used here; it
// would recreate the very race the atomic-builder API exists to prevent.
func (c *webTunnelController) mintInviteLocked(publicURL string) (string, string, string, error) {
	if c.broker == nil {
		return "", "", "", errors.New("tunnel controller has no broker handle")
	}
	st := c.broker.ShareTransport()
	if st == nil {
		return "", "", "", errors.New("share transport is not registered; tunnel cannot mint invites")
	}
	// Capture the token via the URL builder closure: CreateInviteDetailedWithBuilder
	// returns the formatted URL, not the bare token, and we need the bare
	// token to key the passcode map.
	var capturedToken string
	details, err := st.CreateInviteDetailedWithBuilder(context.Background(), func(token string) string {
		capturedToken = token
		return tunnelJoinURL(publicURL, token)
	})
	if err != nil {
		return "", "", "", err
	}
	// CreateInviteDetailedWithBuilder is contractually expected to call the
	// builder exactly once on success; guard against a future refactor that
	// returns success without invoking it. Without this check we'd register
	// c.passcodes[""] = passcode and the joinGate would key off the empty
	// string forever — the join handler rejects empty tokens at 400 so the
	// security posture is fail-closed, but the symptom is a silently-broken
	// invite. Better to fail the start() with a clear error.
	if capturedToken == "" {
		return "", "", "", errors.New("tunnel: invite builder was not called; token capture failed")
	}
	passcode, err := generatePasscode()
	if err != nil {
		return "", "", "", fmt.Errorf("tunnel: generate passcode: %w", err)
	}
	if c.passcodes == nil {
		c.passcodes = make(map[string]string)
	}
	c.passcodes[capturedToken] = passcode
	c.inviteToken = capturedToken
	tokenPrefix := capturedToken
	if len(tokenPrefix) > 6 {
		tokenPrefix = tokenPrefix[:6]
	}
	log.Printf("tunnel: invite minted token=%s… expires=%s", tokenPrefix, details.ExpiresAt)
	return details.URL, passcode, details.ExpiresAt, nil
}

// joinGate is the share-handler hook. It enforces:
//   - the invite token must be one this tunnel issued (an attacker who
//     guesses or steals a network-share token cannot redeem it through the
//     tunnel),
//   - a non-empty passcode must be supplied,
//   - the supplied passcode must match the one we minted alongside the
//     token (constant-time compare).
//
// Internally the three failure modes use distinct sentinels so the audit
// log in share.go can attribute attempts (unknown token / no passcode /
// wrong passcode), but the wire response collapses to one shape — see
// shareJoinPasscodeRequiredMessage and the indistinguishability test.
func (c *webTunnelController) joinGate(token, supplied string) error {
	c.mu.Lock()
	expected, ok := c.passcodes[token]
	c.mu.Unlock()
	if !ok {
		return errJoinPasscodeRequired
	}
	// Trim once and use the canonicalised value for BOTH the emptiness
	// check AND the constant-time compare. Without this, a programmatic
	// caller submitting "835291 " (trailing space) would hit the
	// "wrong passcode" path even though the digits match — the bundled
	// React client strips non-digits before submit, so this is
	// unreachable from the UI, but the gate is what enforces the shape.
	//
	// Distinguishing "joiner submitted nothing" from "joiner submitted
	// wrong digits" is intentional: both produce the same wire response
	// (the indistinguishability invariant), but the audit log loses
	// fidelity if both collapse to one sentinel — operators want to tell
	// brute-force attempts apart from a click-through-without-passcode.
	trimmed := strings.TrimSpace(supplied)
	if trimmed == "" {
		return errJoinPasscodeRequired
	}
	if !constantTimeCompare(trimmed, expected) {
		return errJoinPasscodeInvalid
	}
	return nil
}

// tunnelJoinURL is the canonical "<public-base>/join/<token>" formatter.
// Lives next to shareJoinURL so the join-path shape stays in one place even
// though the host part comes from cloudflared instead of a network bind.
func tunnelJoinURL(publicURL, token string) string {
	return strings.TrimRight(publicURL, "/") + "/join/" + token
}

func (c *webTunnelController) stop() error {
	c.mu.Lock()
	cmd := c.cmd
	cancel := c.cancel
	server := c.server
	ln := c.listener
	stopGuard := c.stopGuard
	wasRunning := c.running
	c.cmd = nil
	c.cancel = nil
	c.server = nil
	c.listener = nil
	c.stopGuard = nil
	c.running = false
	c.publicURL = ""
	c.passcodes = make(map[string]string)
	c.clearInviteLocked()
	c.err = ""
	c.mu.Unlock()
	if wasRunning {
		log.Printf("tunnel: stopped")
	}

	if stopGuard != nil {
		close(stopGuard)
	}
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		// CommandContext's cancel() sends SIGKILL on most platforms; give
		// Wait a brief deadline so we don't block the UI thread on a stuck
		// child.
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(cloudflaredStopTimeout):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	if server != nil {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cloudflaredStopTimeout)
		_ = server.Shutdown(shutdownCtx)
		cancelShutdown()
	}
	if ln != nil {
		_ = ln.Close()
	}
	return nil
}

// scanCloudflaredOutput reads cloudflared's stderr line by line, sends the
// first matching public URL on urlCh, and on EOF (or scanner error) returns
// the trailing lines on tailCh so callers can quote them in an error
// message. Lines are kept short to bound memory.
//
// Closes urlCh on exit. This goroutine is the sole sender, so closing here
// is safe — and waitForTunnelURL treats a closed-without-URL channel as
// "cloudflared exited before publishing a URL", returning fast instead of
// burning the full cloudflaredStartTimeout. Without the close, a crash
// during bring-up made start() wait the full 45s before giving up.
func scanCloudflaredOutput(r io.Reader, urlCh chan<- string, tailCh chan<- []string) {
	defer close(urlCh)
	const tailMax = 8
	tail := make([]string, 0, tailMax)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	urlSent := false
	for scanner.Scan() {
		line := scanner.Text()
		tail = append(tail, line)
		if len(tail) > tailMax {
			tail = tail[len(tail)-tailMax:]
		}
		if !urlSent {
			if match := cloudflaredURLPattern.FindString(line); match != "" {
				select {
				case urlCh <- match:
				default:
				}
				urlSent = true
			}
		}
	}
	tailCopy := make([]string, len(tail))
	copy(tailCopy, tail)
	select {
	case tailCh <- tailCopy:
	default:
	}
}

// waitForTunnelURL blocks until cloudflared publishes a URL, the context is
// cancelled (subprocess died), or the timeout elapses.
func waitForTunnelURL(ctx context.Context, urlCh <-chan string, tailCh <-chan []string, timeout time.Duration) (string, []string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case url, ok := <-urlCh:
		if !ok || url == "" {
			return "", drainTail(tailCh), errors.New("cloudflared exited before publishing a tunnel URL")
		}
		return url, nil, nil
	case <-timer.C:
		return "", drainTail(tailCh), fmt.Errorf("cloudflared did not publish a tunnel URL within %s", timeout)
	case <-ctx.Done():
		return "", drainTail(tailCh), errors.New("cloudflared was cancelled before publishing a tunnel URL")
	}
}

func drainTail(tailCh <-chan []string) []string {
	select {
	case tail := <-tailCh:
		return tail
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}
