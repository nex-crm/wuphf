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

// cloudflaredMaxAttempts caps how many times start() respawns cloudflared
// when bring-up fails with a recognizable transient TryCloudflare API error
// (e.g. trycloudflare.com returns a 500/HTML body and cloudflared exits
// before publishing a URL). Quick-tunnel API hiccups exit fast (<1s), so
// 3 attempts add a couple of seconds rather than minutes. Non-transient
// failures (timeout, missing binary, ctx cancel) skip the retry path.
const cloudflaredMaxAttempts = 3

// cloudflaredRetryBackoffStep is the base delay between retry attempts.
// Attempt N waits N * step before respawning, giving Cloudflare's
// edge a moment to recover from a transient 1101.
const cloudflaredRetryBackoffStep = 1500 * time.Millisecond

// quickTunnelTransientPatterns lists tail-line substrings that indicate
// cloudflared failed to bring up because the trycloudflare.com QuickTunnel
// API itself was unhealthy (5xx / non-JSON body), as opposed to a local
// binary, network, or auth problem. Matching any of these makes the
// failure eligible for a respawn — everything else is treated as
// permanent and surfaced to the user immediately.
var quickTunnelTransientPatterns = []string{
	"Error unmarshaling QuickTunnel response",
	"failed to unmarshal quick Tunnel",
	"500 Internal Server Error",
	"502 Bad Gateway",
	"503 Service Unavailable",
	"504 Gateway Timeout",
}

func isTransientQuickTunnelFailure(tail []string) bool {
	for _, line := range tail {
		for _, pat := range quickTunnelTransientPatterns {
			if strings.Contains(line, pat) {
				return true
			}
		}
	}
	return false
}

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

	// Commit listener+server to c.* under the lock so a concurrent stop()
	// can find and tear them down even if the cloudflared spawn is still
	// in progress. Server/listener are stable across retry attempts; only
	// the cmd/ctx/pipes cycle per attempt.
	c.starting = true
	c.server = server
	c.listener = ln
	c.mu.Unlock()

	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.server == server {
				c.err = fmt.Sprintf("tunnel loopback server failed: %v", err)
				c.running = false
				c.publicURL = ""
				// Mirror the watcher-goroutine + stop() teardown: when
				// the loopback server dies, the cloudflared subprocess
				// is still running but cannot reach the broker, so the
				// session is unusable. Reset c.passcodes and the IP
				// rate limiter so a subsequent Start (which the user
				// will need to click manually) gets a clean slate
				// instead of inheriting the dead session's buckets.
				c.passcodes = make(map[string]string)
				c.rateLimiter = newJoinRateLimiter()
				c.clearInviteLocked()
			}
		}
	}()

	var (
		publicURL string
		tail      []string
		perr      error
	)
	for attempt := 1; attempt <= cloudflaredMaxAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(attempt-1) * cloudflaredRetryBackoffStep
			log.Printf("tunnel: cloudflared bring-up failed (transient TryCloudflare API error); retrying %d/%d in %s", attempt, cloudflaredMaxAttempts, backoff)
			time.Sleep(backoff)
			// stop() may have torn the listener down while we slept.
			c.mu.Lock()
			cancelled := c.server != server
			c.mu.Unlock()
			if cancelled {
				return team.WebTunnelStatus{}, errors.New("tunnel start was cancelled")
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, binary,
			"tunnel",
			"--no-autoupdate",
			"--url", "http://"+loopbackAddr,
			"--metrics", "127.0.0.1:0",
		)
		stderr, errPipe := cmd.StderrPipe()
		if errPipe != nil {
			cancel()
			c.mu.Lock()
			c.starting = false
			c.server = nil
			c.listener = nil
			c.mu.Unlock()
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cloudflaredStopTimeout)
			_ = server.Shutdown(shutdownCtx)
			cancelShutdown()
			_ = ln.Close()
			c.mu.Lock()
			defer c.mu.Unlock()
			c.err = fmt.Sprintf("cloudflared stderr pipe failed: %v", errPipe)
			return c.statusLocked(), errors.New(c.err)
		}
		stdout, errPipe := cmd.StdoutPipe()
		if errPipe != nil {
			cancel()
			c.mu.Lock()
			c.starting = false
			c.server = nil
			c.listener = nil
			c.mu.Unlock()
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cloudflaredStopTimeout)
			_ = server.Shutdown(shutdownCtx)
			cancelShutdown()
			_ = ln.Close()
			c.mu.Lock()
			defer c.mu.Unlock()
			c.err = fmt.Sprintf("cloudflared stdout pipe failed: %v", errPipe)
			return c.statusLocked(), errors.New(c.err)
		}
		if errStart := cmd.Start(); errStart != nil {
			cancel()
			c.mu.Lock()
			c.starting = false
			c.server = nil
			c.listener = nil
			c.mu.Unlock()
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cloudflaredStopTimeout)
			_ = server.Shutdown(shutdownCtx)
			cancelShutdown()
			_ = ln.Close()
			c.mu.Lock()
			defer c.mu.Unlock()
			c.err = fmt.Sprintf("cloudflared failed to start: %v", errStart)
			return c.statusLocked(), errors.New(c.err)
		}

		urlCh := make(chan string, 1)
		tailCh := make(chan []string, 1)
		go scanCloudflaredOutput(stderr, urlCh, tailCh)
		// Drain stdout so a chatty cloudflared can't stall on a full pipe;
		// recent versions emit little here, so noop the bytes.
		go func() { _, _ = io.Copy(io.Discard, stdout) }()

		c.mu.Lock()
		// stop() may have run between iterations.
		if c.server != server {
			c.mu.Unlock()
			cancel()
			_ = cmd.Wait()
			return team.WebTunnelStatus{}, errors.New("tunnel start was cancelled")
		}
		c.cmd = cmd
		c.cancel = cancel
		c.mu.Unlock()

		publicURL, tail, perr = waitForTunnelURL(ctx, urlCh, tailCh, cloudflaredStartTimeout)

		c.mu.Lock()
		// stop() observed the in-flight resources during the wait and tore
		// them down. Don't double-free; just report cancelled.
		if c.cmd != cmd {
			defer c.mu.Unlock()
			return c.statusLocked(), errors.New("tunnel start was cancelled")
		}

		if perr == nil {
			// Success: keep cmd/cancel committed and exit the loop.
			c.mu.Unlock()
			break
		}

		// Wait failed. Tear down this attempt's cmd before deciding
		// whether to retry. Don't release listener/server yet — they
		// may carry to the next attempt or the final failure cleanup.
		cancel()
		c.cmd = nil
		c.cancel = nil
		c.mu.Unlock()
		_ = cmd.Wait()

		// Only TryCloudflare API failures are retryable. Timeouts,
		// missing binary, and ctx cancellation get surfaced
		// immediately so the user isn't waiting through pointless
		// respawns.
		if attempt >= cloudflaredMaxAttempts || !isTransientQuickTunnelFailure(tail) {
			break
		}
	}

	c.mu.Lock()
	c.starting = false

	if perr != nil {
		// All attempts failed. Release c.mu before the server.Shutdown
		// + ln.Close cleanup so a hung in-flight loopback connection
		// cannot block status() callers under the same lock-window
		// invariant the watcher goroutine already follows.
		cleanupServer := c.server
		c.server = nil
		cleanupLn := c.listener
		c.listener = nil
		c.running = false
		msg := perr.Error()
		if isTransientQuickTunnelFailure(tail) {
			msg += fmt.Sprintf(" (after %d attempts; trycloudflare.com appears to be having issues — try again in a minute)", cloudflaredMaxAttempts)
		}
		if len(tail) > 0 {
			msg += "\n\nLast cloudflared output:\n" + strings.Join(tail, "\n")
		}
		c.err = msg
		c.mu.Unlock()
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

	// Success path uses the cmd committed by the winning attempt — the
	// watcher goroutine below Waits on it. Cancel stays in c.cancel for
	// stop() to drive teardown; we don't need a local copy here.
	cmd := c.cmd

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
		// Each tunnel session is its own share window — give the next
		// start() a fresh per-IP bucket map so a joiner who burned their
		// burst on the previous invite isn't throttled when the host
		// rotates the tunnel and shares a new URL with them.
		c.rateLimiter = newJoinRateLimiter()
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
	//
	// mintInviteLocked runs under c.mu, so a stalled broker call would
	// hold the mutex and block status() / stop() / re-clicks of Start.
	// Cap the broker call with a tight deadline — invite creation is an
	// in-process function call to the broker today, so a healthy mint is
	// sub-millisecond; 10s is a generous ceiling on a hung broker before
	// we surface the error to the UI rather than freeze the tunnel card.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var capturedToken string
	details, err := st.CreateInviteDetailedWithBuilder(ctx, func(token string) string {
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
	// Same fresh-bucket-per-session reasoning as the watcher-goroutine
	// teardown path above: a joiner whose IP burned the burst on the
	// previous invite gets a clean slate on the next Start.
	c.rateLimiter = newJoinRateLimiter()
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
