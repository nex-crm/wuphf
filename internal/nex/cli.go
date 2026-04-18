// Package nex wraps the nex-cli binary. WUPHF no longer speaks the legacy
// app.nex.ai HTTP API for detection, registration, or memory recall — those
// paths now shell out to the nex-cli binary from the nex-as-a-skill project:
//
//	https://github.com/nex-crm/nex-as-a-skill
//
// The user is considered "Nex-connected" when nex-cli is on PATH and the
// --no-nex flag (WUPHF_NO_NEX) is not set. Every shell-out uses a context
// with a real timeout; failures are returned to callers so they can log and
// fall back gracefully — missing binary is NOT a crash.
package nex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/config"
)

// DefaultTimeout is the deadline applied to every nex-cli shell-out when the
// caller does not provide one of their own.
const DefaultTimeout = 8 * time.Second

// ErrNotInstalled is returned when nex-cli is not on PATH.
var ErrNotInstalled = errors.New("nex-cli not installed")

// ErrDisabled is returned when --no-nex (WUPHF_NO_NEX) is set for this run.
var ErrDisabled = errors.New("nex disabled via --no-nex")

// binaryCandidates lists the executable names we accept as "nex-cli",
// in priority order. `nex-cli` is the canonical name; `nex` is the alias
// the npm package ships with (`@nex-ai/nex`).
var binaryCandidates = []string{"nex-cli", "nex"}

// BinaryPath returns the absolute path to the nex-cli binary, or the empty
// string if none of the candidate names are on PATH. Lookups are cheap but
// hit the filesystem — callers that need to branch on availability should
// prefer IsInstalled() which wraps this.
func BinaryPath() string {
	for _, name := range binaryCandidates {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// IsInstalled reports whether the nex-cli binary is available on PATH.
// Does NOT consult --no-nex — use Connected() for the full picture.
func IsInstalled() bool {
	return BinaryPath() != ""
}

// Disabled reports whether the user has turned Nex off for this session
// via the --no-nex flag / WUPHF_NO_NEX env var. --no-nex takes precedence
// over auto-detection: even if nex-cli is installed, Disabled() wins.
func Disabled() bool {
	return config.ResolveNoNex()
}

// Connected reports whether WUPHF should treat the user as Nex-connected
// for this session. True iff nex-cli is on PATH AND the user hasn't set
// --no-nex. This is the replacement for the legacy `nex_connected` HTTP
// ping check.
func Connected() bool {
	if Disabled() {
		return false
	}
	return IsInstalled()
}

// quoteArg wraps an argument in double quotes (escaping inner quotes and
// backslashes) when it contains whitespace or characters the nex-cli REPL
// parser treats as word separators. Plain tokens pass through untouched.
func quoteArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// Run executes `nex-cli --cmd "<args...>"` with the given context/timeout and
// returns stdout (trimmed) on success. nex-cli refuses to start without a TTY
// unless --cmd/--script is used, so every shell-out goes through --cmd with
// the args joined (and quoted where necessary) into a single command string.
// A non-zero exit code, missing binary, or context timeout are all returned
// as errors. The caller is responsible for logging and falling back.
func Run(ctx context.Context, args ...string) (string, error) {
	if Disabled() {
		return "", ErrDisabled
	}
	bin := BinaryPath()
	if bin == "" {
		return "", ErrNotInstalled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = quoteArg(a)
	}
	cmdStr := strings.Join(quoted, " ")
	cmd := exec.CommandContext(ctx, bin, "--cmd", cmdStr)
	cmd.Env = appendClientEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("nex-cli %s: timeout after %s", cmdStr, DefaultTimeout)
		}
		trimmed := strings.TrimSpace(stderr.String())
		if trimmed != "" {
			return "", fmt.Errorf("nex-cli %s: %s", cmdStr, trimmed)
		}
		return "", fmt.Errorf("nex-cli %s: %w", cmdStr, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Register shells out to `nex-cli --cmd "setup <email>"`. nex-cli exposes
// non-interactive registration as the `setup` subcommand (there is no
// `register`). Used by the WUPHF onboarding flow in place of the legacy
// POST to /api/v1/agents/register on app.nex.ai. Blocks until the command
// exits (or the default timeout trips).
func Register(ctx context.Context, email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", fmt.Errorf("register: email is required")
	}
	return Run(ctx, "setup", email)
}

// Recall shells out to `nex-cli recall <query>` and returns the trimmed
// stdout. Used by FetchEntityBrief and any future memory-lookup call sites.
// Callers that need a short deadline should pass a pre-scoped context.
func Recall(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("recall: query is required")
	}
	return Run(ctx, "recall", query)
}

// NexClientEnvVar is the env var nex-cli (and downstream Nex services)
// can read to attribute a call to the client that initiated it. Exposed
// so server-side tooling doesn't have to guess the name.
const NexClientEnvVar = "NEX_CLIENT"

// appendClientEnv adds NEX_CLIENT=wuphf/<version> unless it's already
// set in env. Respecting an existing value lets integrators nest clients
// (e.g. a wrapper that sets NEX_CLIENT=myapp/wuphf/<version>) without us
// stomping on it.
func appendClientEnv(env []string) []string {
	prefix := NexClientEnvVar + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return env
		}
	}
	return append(env, prefix+"wuphf/"+buildinfo.Current().Version)
}
