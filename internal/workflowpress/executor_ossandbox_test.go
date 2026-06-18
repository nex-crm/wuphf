package workflowpress

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// PORTABLE UNIT TESTS — these run on every platform (darwin dev box AND Linux
// CI). They exercise the gates and the pure profile/args generators WITHOUT
// spawning a real process (the runner is faked), so the boundary logic is proven
// independent of which sandbox tool is installed.
// ============================================================================

// fakeRunner records the wrapped argv it is handed and returns a canned result,
// so a test can assert the EXACT command the backend would have spawned without
// running anything.
type fakeRunner struct {
	gotArgv []string
	out     []byte
	code    int
	err     error
}

func (f *fakeRunner) run(_ context.Context, argv []string) ([]byte, int, error) {
	f.gotArgv = argv
	return f.out, f.code, f.err
}

// newTestExecutor builds an osSandboxExecutor pinned to goos with a fake runner
// and a lookPath that always succeeds, so the gate/wrap logic is tested without a
// real sandbox tool.
func newTestExecutor(goos string, runner commandRunner) *osSandboxExecutor {
	return &osSandboxExecutor{
		goos:     goos,
		runner:   runner,
		lookPath: func(s string) (string, error) { return s, nil },
	}
}

func argvPayload(t *testing.T, argv ...string) []byte {
	t.Helper()
	b, err := json.Marshal(argv)
	if err != nil {
		t.Fatalf("marshal argv: %v", err)
	}
	return b
}

// TestOSSandboxBackendName documents the audit label.
func TestOSSandboxBackendName(t *testing.T) {
	t.Parallel()
	if got := NewOSSandboxExecutor().Backend(); got != "os-sandbox" {
		t.Fatalf("Backend() = %q, want os-sandbox", got)
	}
}

// TestOSSandboxRefusesUnknownKind proves the allow-list gate fires before any
// process is spawned: an unrecognised kind (which Mutates() classifies as a read,
// failing OPEN) is refused with ErrNotAuthorized even with approval and an
// allow-listed writable target.
func TestOSSandboxRefusesUnknownKind(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	exec := newTestExecutor("darwin", runner)
	cfg := ExecConfig{
		WorkflowID:      "wf",
		Version:         1,
		ApprovalGranted: true,
		FS:              []FSCap{{Path: "/tmp/x", Write: true}},
	}
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "smuggled", Kind: ActionKind("exfiltrate"), Target: "/tmp/x",
		Payload: argvPayload(t, "/bin/echo", "hi"),
	})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Execute(unknown kind) = %v, want ErrNotAuthorized", err)
	}
	if runner.gotArgv != nil {
		t.Fatalf("runner was invoked for a refused action: %v", runner.gotArgv)
	}
}

// TestOSSandboxRefusesMutatingWithoutApproval proves a state-changing action is
// refused unless ApprovalGranted, before any process is spawned.
func TestOSSandboxRefusesMutatingWithoutApproval(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	exec := newTestExecutor("darwin", runner)
	cfg := ExecConfig{
		WorkflowID:      "wf",
		Version:         1,
		ApprovalGranted: false,
		FS:              []FSCap{{Path: "/tmp/x", Write: true}},
	}
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "route_to_ae", Kind: ActionInternalWrite, Target: "/tmp/x",
		Payload: argvPayload(t, "/bin/echo", "hi"),
	})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Execute(mutating, no approval) = %v, want ErrNotAuthorized", err)
	}
	if runner.gotArgv != nil {
		t.Fatalf("runner was invoked for a refused action: %v", runner.gotArgv)
	}
}

// TestOSSandboxRefusesUnlistedTarget proves deny-by-default: a read whose target
// is not in the FS allow-list is refused.
func TestOSSandboxRefusesUnlistedTarget(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	exec := newTestExecutor("darwin", runner)
	cfg := ExecConfig{WorkflowID: "wf", Version: 1} // empty allow-list == deny all
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "enrich", Kind: ActionRead, Target: "/tmp/secret",
		Payload: argvPayload(t, "/bin/cat", "/tmp/secret"),
	})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Execute(unlisted target) = %v, want ErrNotAuthorized", err)
	}
	if runner.gotArgv != nil {
		t.Fatalf("runner was invoked for a refused action: %v", runner.gotArgv)
	}
}

// TestOSSandboxRefusesNetworkAllowlist proves a non-empty Net allow-list is
// refused rather than silently widened to allow-all network. Egress is deny-all
// in this tier; a Net entry is a deferred proxy follow-up.
func TestOSSandboxRefusesNetworkAllowlist(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	exec := newTestExecutor("darwin", runner)
	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		FS:  []FSCap{{Path: "/tmp/x"}},
		Net: []NetCap{{Host: "api.example.com", Ports: []int{443}}},
	}
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "call_api", Kind: ActionRead, Target: "/tmp/x",
		Payload: argvPayload(t, "/bin/echo", "hi"),
	})
	if !errors.Is(err, ErrNetworkAllowlistUnsupported) {
		t.Fatalf("Execute(net allow-list) = %v, want ErrNetworkAllowlistUnsupported", err)
	}
	if runner.gotArgv != nil {
		t.Fatalf("runner was invoked for a refused action: %v", runner.gotArgv)
	}
}

// TestOSSandboxRefusesUnsupportedPlatform proves the backend NEVER runs
// unsandboxed: a platform without a wrapper is refused with ErrNotAuthorized
// rather than executing.
func TestOSSandboxRefusesUnsupportedPlatform(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	exec := newTestExecutor("windows", runner)
	cfg := ExecConfig{WorkflowID: "wf", Version: 1, FS: []FSCap{{Path: "/tmp/x"}}}
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "read", Kind: ActionRead, Target: "/tmp/x",
		Payload: argvPayload(t, "/bin/echo", "hi"),
	})
	if !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Execute(unsupported platform) = %v, want ErrNotAuthorized", err)
	}
	if runner.gotArgv != nil {
		t.Fatalf("runner was invoked on an unsupported platform: %v", runner.gotArgv)
	}
}

// TestOSSandboxFailsClosedWhenToolMissing proves the backend refuses (does not
// run unsandboxed) when the platform sandbox tool is not installed.
func TestOSSandboxFailsClosedWhenToolMissing(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	for _, goos := range []string{"darwin", "linux"} {
		goos := goos
		t.Run(goos, func(t *testing.T) {
			t.Parallel()
			exec := &osSandboxExecutor{
				goos:     goos,
				runner:   runner,
				lookPath: func(string) (string, error) { return "", errors.New("not found") },
			}
			cfg := ExecConfig{WorkflowID: "wf", Version: 1, FS: []FSCap{{Path: "/tmp/x"}}}
			_, err := exec.Execute(context.Background(), cfg, ExecAction{
				Name: "read", Kind: ActionRead, Target: "/tmp/x",
				Payload: argvPayload(t, "/bin/echo", "hi"),
			})
			if !errors.Is(err, ErrSandboxUnavailable) {
				t.Fatalf("Execute(tool missing) = %v, want ErrSandboxUnavailable", err)
			}
		})
	}
}

// TestOSSandboxRefusesEmptyArgv proves the backend never guesses a command: an
// action with no decodable argv is refused.
func TestOSSandboxRefusesEmptyArgv(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	exec := newTestExecutor("darwin", runner)
	cfg := ExecConfig{WorkflowID: "wf", Version: 1, FS: []FSCap{{Path: "/tmp/x"}}}
	cases := map[string][]byte{
		"nil payload":   nil,
		"empty array":   []byte(`[]`),
		"blank argv0":   []byte(`["   "]`),
		"not json argv": []byte(`not-json`),
	}
	for name, payload := range cases {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := exec.Execute(context.Background(), cfg, ExecAction{
				Name: "x", Kind: ActionRead, Target: "/tmp/x", Payload: payload,
			})
			if !errors.Is(err, ErrEmptyArgv) {
				t.Fatalf("Execute(%s) = %v, want ErrEmptyArgv", name, err)
			}
		})
	}
}

// TestOSSandboxRespectsContextCancellation confirms a cancelled context refuses
// before any work.
func TestOSSandboxRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	exec := newTestExecutor("darwin", runner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := exec.Execute(ctx, ExecConfig{}, ExecAction{
		Name: "noop", Kind: ActionRead, Payload: argvPayload(t, "/bin/echo"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(cancelled ctx) = %v, want context.Canceled", err)
	}
}

// TestOSSandboxWrapsDarwinArgv proves the darwin wrapper is sandbox-exec -p
// <profile> <inner argv>, and that the profile is the one generated for cfg.
func TestOSSandboxWrapsDarwinArgv(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{out: []byte("ok"), code: 0}
	exec := newTestExecutor("darwin", runner)
	cfg := ExecConfig{WorkflowID: "wf", Version: 1, FS: []FSCap{{Path: "/tmp/data"}}}
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "read", Kind: ActionRead, Target: "/tmp/data",
		Payload: argvPayload(t, "/bin/cat", "/tmp/data/x"),
	})
	if err != nil {
		t.Fatalf("Execute = %v, want nil", err)
	}
	if res == nil || string(res.Output) != "ok" {
		t.Fatalf("unexpected result %+v", res)
	}
	got := runner.gotArgv
	if len(got) < 5 || got[0] != sandboxExecPath || got[1] != "-p" {
		t.Fatalf("wrapped argv = %v, want sandbox-exec -p <profile> ...", got)
	}
	// The inner command must be appended verbatim after the profile.
	if got[3] != "/bin/cat" || got[4] != "/tmp/data/x" {
		t.Fatalf("inner argv not appended verbatim: %v", got)
	}
	if !strings.Contains(got[2], "(deny default)") || !strings.Contains(got[2], "(deny network*)") {
		t.Fatalf("profile missing deny-default/deny-network: %q", got[2])
	}
}

// TestOSSandboxWrapsLinuxArgv proves the linux wrapper is bwrap <hardening args>
// <inner argv>, with the namespace + no-new-privs hardening present.
func TestOSSandboxWrapsLinuxArgv(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{out: []byte("ok"), code: 0}
	exec := newTestExecutor("linux", runner)
	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		ApprovalGranted: true, // a write needs approval to pass the mutating gate
		FS:              []FSCap{{Path: "/tmp/data", Write: true}},
	}
	_, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "write", Kind: ActionInternalWrite, Target: "/tmp/data",
		Payload: argvPayload(t, "/bin/touch", "/tmp/data/x"),
	})
	if err != nil {
		t.Fatalf("Execute = %v, want nil", err)
	}
	got := runner.gotArgv
	if len(got) == 0 || filepath.Base(got[0]) != bwrapBinary {
		t.Fatalf("wrapped argv[0] = %v, want bwrap", got)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"--unshare-all", "--unshare-net", "--die-with-parent", "--new-session", "--no-new-privs"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("bwrap args missing %q: %v", want, got)
		}
	}
	// The inner command must be the tail.
	if got[len(got)-2] != "/bin/touch" || got[len(got)-1] != "/tmp/data/x" {
		t.Fatalf("inner argv not appended at tail: %v", got)
	}
}

// TestSeatbeltProfileDenyDefault proves the generated macOS profile is
// deny-by-default for filesystem and network, and only allow-lists the configured
// read/write paths plus the system read base.
func TestSeatbeltProfileDenyDefault(t *testing.T) {
	t.Parallel()
	cfg := ExecConfig{
		FS: []FSCap{
			{Path: "/tmp/readonly"},
			{Path: "/tmp/writable", Write: true},
		},
	}
	p := seatbeltProfile(cfg)
	mustContain := []string{
		"(version 1)",
		"(deny default)",
		"(deny network*)",
		"(allow file-read*",
		"(allow file-write*",
	}
	for _, s := range mustContain {
		if !strings.Contains(p, s) {
			t.Errorf("profile missing %q\n%s", s, p)
		}
	}
	// The writable path must appear under write; the read-only path must NOT.
	writeClause := p[strings.Index(p, "(allow file-write*"):]
	if !strings.Contains(writeClause, canonicalPath("/tmp/writable")) {
		t.Errorf("writable path not in write clause:\n%s", writeClause)
	}
	if strings.Contains(writeClause, canonicalPath("/tmp/readonly")) {
		t.Errorf("read-only path leaked into write clause:\n%s", writeClause)
	}
}

// TestSeatbeltProfileNoWriteClauseWhenNoWrites proves that with zero writable
// entries the profile emits no file-write clause at all (deny-default governs
// every write).
func TestSeatbeltProfileNoWriteClauseWhenNoWrites(t *testing.T) {
	t.Parallel()
	cfg := ExecConfig{FS: []FSCap{{Path: "/tmp/readonly"}}}
	p := seatbeltProfile(cfg)
	if strings.Contains(p, "(allow file-write*") {
		t.Fatalf("profile should have no write clause:\n%s", p)
	}
}

// TestSeatbeltProfileEscapesPaths proves a crafted path cannot break out of the
// quoted Seatbelt string and inject a directive. The escape must neutralise the
// quote so the injected (allow file-write* ...) never becomes a live clause.
func TestSeatbeltProfileEscapesPaths(t *testing.T) {
	t.Parallel()
	evil := `/tmp/a") (allow file-write* (subpath "/`
	cfg := ExecConfig{FS: []FSCap{{Path: evil}}} // read-only entry: no Write flag
	p := seatbeltProfile(cfg)

	// The security property: the crafted text appears ONLY inside a quoted,
	// escaped read-subpath token, never as a live directive. There is no Write=true
	// entry, so a real (allow file-write* ...) CLAUSE — i.e. one at the start of a
	// line — must not exist. An unescaped quote would have closed the read string
	// and started exactly such a clause.
	for _, line := range strings.Split(p, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "(allow file-write*") {
			t.Fatalf("path injection opened a live write clause:\n%s", p)
		}
	}
	// The breakout text may appear, but ONLY in its ESCAPED form (the quote
	// preceded by a backslash). Scan every occurrence of the quote-paren breakout
	// sequence and require the byte before the quote to be a backslash — i.e. there
	// is no UNESCAPED `") (allow file-write` that would terminate the read string
	// and start a live directive.
	needle := `") (allow file-write`
	for off := 0; ; {
		i := strings.Index(p[off:], needle)
		if i < 0 {
			break
		}
		abs := off + i
		if abs == 0 || p[abs-1] != '\\' {
			t.Fatalf("UNESCAPED quote/paren breakout present:\n%s", p)
		}
		off = abs + 1
	}
	// Positive proof the escaper ran: the embedded quote is rendered as \".
	if !strings.Contains(p, `\"`) {
		t.Fatalf("escaped quote not found; escaping did not run:\n%s", p)
	}
}

// TestSeatbeltProfileDeterministic proves the profile is byte-stable regardless
// of FS entry order, so audit/replay is reproducible.
func TestSeatbeltProfileDeterministic(t *testing.T) {
	t.Parallel()
	a := ExecConfig{FS: []FSCap{{Path: "/tmp/b", Write: true}, {Path: "/tmp/a"}}}
	b := ExecConfig{FS: []FSCap{{Path: "/tmp/a"}, {Path: "/tmp/b", Write: true}}}
	if seatbeltProfile(a) != seatbeltProfile(b) {
		t.Fatalf("profile is order-sensitive; want deterministic output")
	}
}

// TestBwrapArgsHardening proves the bwrap argv carries the namespace + privilege
// hardening and binds read vs write entries correctly.
func TestBwrapArgsHardening(t *testing.T) {
	t.Parallel()
	cfg := ExecConfig{FS: []FSCap{
		{Path: "/data/ro"},
		{Path: "/data/rw", Write: true},
	}}
	args := bwrapArgs(cfg)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--unshare-all", "--unshare-net", "--die-with-parent",
		"--new-session", "--no-new-privs",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("bwrap args missing %q: %v", want, args)
		}
	}
	// Writable path: --bind. Read-only path: --ro-bind.
	if !containsPair(args, "--bind", canonicalPath("/data/rw")) {
		t.Errorf("writable path not bound rw: %v", args)
	}
	if !containsPair(args, "--ro-bind", canonicalPath("/data/ro")) {
		t.Errorf("read-only path not bound ro: %v", args)
	}
	// A writable path must NOT also be ro-bound (rw supersedes).
	if containsPair(args, "--ro-bind", canonicalPath("/data/rw")) {
		t.Errorf("writable path double-bound ro: %v", args)
	}
}

func containsPair(args []string, flag, path string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == path {
			return true
		}
	}
	return false
}

// TestCanonicalPath proves symlinked allow-list entries are resolved to the real
// path the kernel enforces, including a not-yet-existing write target whose
// parent is a symlink. This is load-bearing: on macOS /tmp is a symlink to
// /private/tmp, so an un-canonicalized entry silently fails the boundary.
func TestCanonicalPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	// An existing symlinked dir resolves to its target.
	if got := canonicalPath(link); got != canonicalPath(real) {
		t.Errorf("canonicalPath(symlink) = %q, want %q", got, canonicalPath(real))
	}
	// A not-yet-existing child of a symlinked dir resolves through the parent.
	child := filepath.Join(link, "newfile")
	wantChild := filepath.Join(canonicalPath(real), "newfile")
	if got := canonicalPath(child); got != wantChild {
		t.Errorf("canonicalPath(symlinked-parent child) = %q, want %q", got, wantChild)
	}
}

// TestExecRunnerReportsExitCode proves execRunner distinguishes a process that
// ran and exited non-zero (ExitCode, no Go error) from one killed by the cap
// (Go error, negative exit). Skips where /bin/sh is unavailable.
func TestExecRunnerReportsExitCode(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	r := execRunner{}
	out, code, err := r.run(context.Background(), []string{"/bin/sh", "-c", "echo hi; exit 7"})
	if err != nil {
		t.Fatalf("run = %v, want nil (non-zero exit is not a Go error)", err)
	}
	if code != 7 {
		t.Fatalf("exit code = %d, want 7", code)
	}
	if !strings.Contains(string(out), "hi") {
		t.Fatalf("output = %q, want to contain hi", string(out))
	}
}
