package onboarding

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/runtimebin"
)

// PrereqResult describes the detection outcome for a single prerequisite binary.
type PrereqResult struct {
	// Name is the binary name (e.g. "node", "git", "claude").
	Name string `json:"name"`

	// Required is true when wuphf cannot function without this binary.
	Required bool `json:"required"`

	// Found is true when the binary was located on PATH.
	Found bool `json:"found"`

	// OK is a compatibility alias for Found used by the browser onboarding UI.
	OK bool `json:"ok"`

	// Version is the parsed version string from <name> --version, or empty.
	Version string `json:"version,omitempty"`

	// InstallURL is the canonical install page for this binary.
	InstallURL string `json:"install_url,omitempty"`

	// SessionProbed is true when the runtime supports a session-status
	// subcommand and the probe ran to completion (regardless of result).
	// False means "we did not attempt the probe" — either because the
	// binary has no session, or no probe is wired for this runtime.
	// Issue #932: distinguishes "we asked and got no session" (block the
	// tile, show a sign-in CTA) from "we don't know" (let the user click
	// and learn from the agent loop, the legacy behavior).
	SessionProbed bool `json:"session_probed,omitempty"`

	// SignedIn is true when the runtime reports an active auth session.
	// Only meaningful when SessionProbed is true. For CLI runtimes
	// (claude, codex, opencode) this is the result of the per-runtime
	// session-status subcommand; see runtimeSessionProbes in prereqs.go.
	SignedIn bool `json:"signed_in,omitempty"`

	// SignInCommand is the suggested shell command for the user to run
	// when SessionProbed is true and SignedIn is false. Frontend renders
	// this in a copy-to-clipboard "Sign in" CTA. Empty when irrelevant.
	SignInCommand string `json:"sign_in_command,omitempty"`
}

// prereqSpec defines static metadata for each required binary.
type prereqSpec struct {
	required   bool
	installURL string
}

var prereqSpecs = map[string]prereqSpec{
	"node":     {required: true, installURL: "https://nodejs.org"},
	"git":      {required: true, installURL: "https://git-scm.com"},
	"claude":   {required: false, installURL: "https://claude.ai/code"},
	"codex":    {required: false, installURL: "https://github.com/openai/codex"},
	"opencode": {required: false, installURL: "https://opencode.ai"},
	"cursor":   {required: false, installURL: "https://cursor.com/"},
	"windsurf": {required: false, installURL: "https://codeium.com/windsurf"},
}

// CheckAll returns a PrereqResult for each tracked binary in a stable order:
// node, git, claude, codex, opencode, cursor, windsurf. At least one of the
// CLI runtimes must be present for wuphf to actually run a turn, but all are
// marked optional here so the user can proceed with whichever runtime
// they have.
//
// Probes run concurrently. CheckOne's per-probe timeout is 10s (see comment
// there for rationale) and CheckAll is invoked from an HTTP handler with a
// 5s client deadline at cmd/wuphf/onboarding.go; running probes serially
// would mean worst-case wall-clock = 7 × 10s = 70s, far past any sane HTTP
// budget. Concurrent probes cap wall-clock at max(probe), well under the
// client timeout. Order of `names` is preserved in the returned slice.
//
// ctx is the parent (request-scoped) context; per-probe timeouts derive from
// it so a cancelled HTTP request stops in-flight subprocess probes instead
// of leaking them. Pass context.Background() only from tests or boot paths
// that have no request lifecycle.
func CheckAll(ctx context.Context) []PrereqResult {
	names := []string{"node", "git", "claude", "codex", "opencode", "cursor", "windsurf"}
	results := make([]PrereqResult, len(names))
	var wg sync.WaitGroup
	wg.Add(len(names))
	for i, name := range names {
		go func(i int, name string) {
			defer wg.Done()
			results[i] = CheckOne(ctx, name)
		}(i, name)
	}
	wg.Wait()
	return results
}

// CheckOne probes a single binary by name. It resolves from PATH plus common
// CLI install directories, then invokes `<name> --version` to capture the
// version string. If resolution fails the binary is considered absent and the
// version field is left empty.
//
// ctx is the parent (request-scoped) context; per-call timeouts derive from
// it so probe subprocesses can't outlive a cancelled HTTP request.
func CheckOne(ctx context.Context, name string) PrereqResult {
	spec := prereqSpecs[name]
	r := PrereqResult{
		Name:       name,
		Required:   spec.required,
		InstallURL: spec.installURL,
	}

	path, err := runtimebin.LookPath(name)
	if err != nil {
		return r
	}
	r.Found = true
	r.OK = true

	// Best-effort version capture; ignore errors.
	//
	// 10s (up from 3s) to keep the probe reliable when the machine is under
	// parallel test load — `go test ./...` can stack 20+ concurrent fork+exec
	// calls, and a 3s window was flaky on a developer laptop running the
	// pre-push hook. This is a one-shot `--version` probe, not a hot path;
	// the timeout is a floor on machine health, not on binary response time.
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	defer versionCancel()
	out, err := exec.CommandContext(versionCtx, path, "--version").Output()
	if err == nil {
		r.Version = parseVersion(string(out))
	}

	// Issue #932: session-status probe. Run the per-runtime auth-status
	// subcommand. The goal is to distinguish "claude installed" (current
	// behavior) from "claude installed AND signed in" — the latter is the
	// actually-load-bearing state for an agent loop's first LLM call.
	//
	// Probe failure modes are intentionally lenient: a non-zero exit, parse
	// error, or timeout all set SignedIn=false (which the SPA renders as
	// a "sign in" CTA) rather than blocking the user. The cost of a false
	// negative is a friction-y but recoverable click; the cost of a false
	// positive (current behavior) is letting the user complete onboarding
	// only to fail on the first agent call.
	probe, ok := runtimeSessionProbes[name]
	if ok && probe != nil {
		probeCtx, probeCancel := context.WithTimeout(ctx, 3*time.Second)
		defer probeCancel()
		signedIn := probe(probeCtx, path)
		r.SessionProbed = true
		r.SignedIn = signedIn
		if !signedIn {
			r.SignInCommand = sessionSignInCommands[name]
		}
	}
	return r
}

// runtimeSessionProbes maps a binary name to the per-runtime session probe.
// The probe returns true only when the runtime reports an active auth
// session; any non-zero exit, parse error, or timeout returns false. CLIs
// without a session-status surface (cursor, windsurf) have no entry — the
// frontend renders them with SessionProbed=false and treats picking as
// always-allowed.
var runtimeSessionProbes = map[string]func(ctx context.Context, path string) bool{
	"claude":   probeClaudeSession,
	"codex":    probeCodexSession,
	"opencode": probeOpencodeSession,
}

// sessionSignInCommands maps a binary name to the shell command the user
// should run to sign in. Returned to the SPA so the "Sign in" CTA can
// copy the command to the clipboard. Keep these in sync with the CLI
// surface — these are documented login flows, not best-guesses.
var sessionSignInCommands = map[string]string{
	"claude":   "claude auth login",
	"codex":    "codex login",
	"opencode": "opencode auth login",
}

// probeClaudeSession runs `claude auth status` and reports whether the
// CLI is signed in. Claude Code emits a JSON document on stdout with a
// `loggedIn` boolean; non-zero exit, malformed JSON, or `loggedIn: false`
// all return false.
func probeClaudeSession(ctx context.Context, path string) bool {
	out, err := exec.CommandContext(ctx, path, "auth", "status").Output()
	if err != nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(string(out)))
	// Claude Code emits structured JSON; this substring match avoids a
	// JSON parser dependency for a tiny payload. The negative case
	// `"loggedin": false` is matched first so it doesn't false-positive
	// on the substring `"loggedin": true`.
	if strings.Contains(text, `"loggedin": false`) || strings.Contains(text, `"loggedin":false`) {
		return false
	}
	return strings.Contains(text, `"loggedin": true`) || strings.Contains(text, `"loggedin":true`)
}

// probeCodexSession runs `codex login status` and reports whether the
// CLI is signed in. Codex prints a single line like "Logged in using
// ChatGPT" / "Logged in using API key" on success; a "not logged in" or
// equivalent message on failure. Exit code is 0 in both cases (as of
// codex 0.55), so we have to text-classify.
func probeCodexSession(ctx context.Context, path string) bool {
	out, err := exec.CommandContext(ctx, path, "login", "status").CombinedOutput()
	if err != nil {
		// Some codex versions exit non-zero on "not logged in" — that's
		// still an unambiguous unauthenticated state.
		return false
	}
	text := strings.ToLower(string(out))
	if strings.Contains(text, "not logged in") ||
		strings.Contains(text, "no auth") ||
		strings.Contains(text, "no credentials") {
		return false
	}
	return strings.Contains(text, "logged in")
}

// probeOpencodeSession runs `opencode providers list` and reports whether
// any provider has stored credentials. The CLI prints a banner with a
// count like "0 credentials" / "2 credentials". Zero-count or parse
// failure → not signed in.
func probeOpencodeSession(ctx context.Context, path string) bool {
	out, err := exec.CommandContext(ctx, path, "providers", "list").CombinedOutput()
	if err != nil {
		return false
	}
	text := strings.ToLower(string(out))
	if strings.Contains(text, "0 credentials") {
		return false
	}
	// Match "<N> credential" where N >= 1. The trailing space (or "s")
	// disambiguates from "0 credentials".
	for _, n := range []string{"1 credential", "2 credential", "3 credential", "4 credential", "5 credential", "6 credential", "7 credential", "8 credential", "9 credential"} {
		if strings.Contains(text, n) {
			return true
		}
	}
	// Fallback: any provider name in the rendered table implies a session.
	// Suppress noise from the "Credentials ~/.local/share/..." header by
	// requiring a known provider label.
	for _, name := range []string{"anthropic", "openai", "google", "mistral", "groq", "openrouter"} {
		if strings.Contains(text, name) {
			return true
		}
	}
	return false
}

// parseVersion trims whitespace and returns the first non-empty line from
// the version output. Many CLIs output one line; some (like git) prefix with
// the program name which we preserve verbatim.
func parseVersion(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
