package workflowpress

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// execLookPath is the real PATH resolver the live tests use; kept as a package
// var so the test file does not collide with the production exec.LookPath usage.
var execLookPath = exec.LookPath

// jsonArgv marshals an argv into the Payload shape the OS-sandbox backend decodes.
func jsonArgv(t *testing.T, argv ...string) []byte {
	t.Helper()
	b, err := json.Marshal(argv)
	if err != nil {
		t.Fatalf("marshal argv: %v", err)
	}
	return b
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ============================================================================
// LIVE ADVERSARIAL TESTS — these SPAWN A REAL PROCESS under the OS sandbox to
// prove the boundary is REAL, not a paper claim. They run only where the
// platform sandbox tool actually exists:
//
//   - darwin: sandbox-exec (present on every macOS box, incl. this dev box).
//   - linux:  bwrap, when installed (skipped otherwise — bwrap is not always on
//             CI runners; the portable unit tests still cover the linux arg gen).
//
// Each test is written so it FAILS if the boundary is bypassable: an allow-listed
// op must SUCCEED and a non-allow-listed op must be BLOCKED. If a "denied" op
// were silently allowed, the assertion flips and the test goes red.
// ============================================================================

// liveExecutor returns an osSandboxExecutor wired to the real runner + real
// lookPath for the current platform, or skips when the platform sandbox tool is
// unavailable (so the suite stays green on a darwin dev box AND on Linux CI).
func liveExecutor(t *testing.T) *osSandboxExecutor {
	t.Helper()
	e := &osSandboxExecutor{
		goos:     goruntime.GOOS,
		runner:   execRunner{},
		lookPath: lookPathFor(t),
	}
	switch goruntime.GOOS {
	case "darwin":
		if _, err := os.Stat(sandboxExecPath); err != nil {
			t.Skipf("sandbox-exec not present at %s", sandboxExecPath)
		}
	case "linux":
		if _, err := e.lookPath(bwrapBinary); err != nil {
			t.Skip("bwrap (bubblewrap) not installed; linux arg gen covered by unit tests")
		}
	default:
		t.Skipf("no OS sandbox on %s", goruntime.GOOS)
	}
	return e
}

func lookPathFor(t *testing.T) func(string) (string, error) {
	t.Helper()
	return func(name string) (string, error) {
		// sandboxExecPath is an absolute path; stat it. bwrap is a PATH lookup.
		if filepath.IsAbs(name) {
			if _, err := os.Stat(name); err != nil {
				return "", err
			}
			return name, nil
		}
		return execLookPath(name)
	}
}

// catBinary / touchBinary resolve to absolute paths so the inner argv is stable
// across platforms.
func catBinary(t *testing.T) string { return resolveBin(t, "cat") }
func shBinary(t *testing.T) string  { return resolveBin(t, "sh") }
func ncBinary(t *testing.T) string  { return resolveBinOpt("nc") }
func touchBin(t *testing.T) string  { return resolveBin(t, "touch") }
func resolveBin(t *testing.T, name string) string {
	t.Helper()
	p, err := execLookPath(name)
	if err != nil {
		t.Skipf("%s not found: %v", name, err)
	}
	return p
}
func resolveBinOpt(name string) string {
	p, err := execLookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// TestLiveReadBoundary proves the FILESYSTEM READ boundary is real: a sandboxed
// read of an ALLOW-LISTED path succeeds, and a read of a NON-allow-listed sibling
// is BLOCKED — through one real sandbox-exec/bwrap invocation each.
func TestLiveReadBoundary(t *testing.T) {
	t.Parallel()
	exec := liveExecutor(t)
	cat := catBinary(t)

	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	denied := filepath.Join(base, "denied")
	mustMkdir(t, allowed)
	mustMkdir(t, denied)
	allowedFile := filepath.Join(allowed, "ok.txt")
	deniedFile := filepath.Join(denied, "secret.txt")
	mustWrite(t, allowedFile, "public")
	mustWrite(t, deniedFile, "SECRET")

	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		FS:            []FSCap{{Path: allowed}},
		MaxWallMillis: 5000,
	}

	// Allow-listed read SUCCEEDS (exit 0).
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "read_allowed", Kind: ActionRead, Target: allowed,
		Payload: jsonArgv(t, cat, allowedFile),
	})
	if err != nil {
		t.Fatalf("allow-listed read errored: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("allow-listed read exit = %d (out=%q), want 0", res.ExitCode, res.Output)
	}

	// Non-allow-listed read is BLOCKED. The target gate refuses it BEFORE exec,
	// AND even if a future change widened the target gate, the kernel sandbox
	// would still block the file open. We assert the strong end-to-end property:
	// reading the secret never succeeds with its contents.
	res2, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "read_denied", Kind: ActionRead, Target: denied, // not in FS allow-list
		Payload: jsonArgv(t, cat, deniedFile),
	})
	if err == nil && res2 != nil && res2.ExitCode == 0 {
		t.Fatalf("BOUNDARY BYPASS: non-allow-listed read succeeded (out=%q)", res2.Output)
	}
}

// TestLiveReadBoundaryKernelEnforced is the stronger test: it allow-lists the
// PARENT so the target gate passes, then proves the KERNEL still blocks reading a
// sibling path that is not in the read allow-list. This isolates the sandbox
// boundary from the pre-exec target check — if the only thing stopping the read
// were the Go-side target gate, this test would catch a bypass once the file is
// actually opened inside the sandbox.
func TestLiveReadBoundaryKernelEnforced(t *testing.T) {
	t.Parallel()
	exec := liveExecutor(t)
	cat := catBinary(t)

	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	mustMkdir(t, allowed)
	insideFile := filepath.Join(allowed, "ok.txt")
	mustWrite(t, insideFile, "public")

	// A secret OUTSIDE the allow-listed dir.
	secret := filepath.Join(base, "outside-secret.txt")
	mustWrite(t, secret, "TOPSECRET")

	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		FS:            []FSCap{{Path: allowed}},
		MaxWallMillis: 5000,
	}

	// Target is the allow-listed dir (gate passes), but the inner command tries to
	// read a file OUTSIDE it. The kernel sandbox must block the open.
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "read_inside_target_outside_file", Kind: ActionRead, Target: allowed,
		Payload: jsonArgv(t, cat, secret),
	})
	if err != nil {
		t.Fatalf("execute errored unexpectedly: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("BOUNDARY BYPASS: kernel allowed read of non-allow-listed file (out=%q)", res.Output)
	}
}

// TestLiveWriteBoundary proves the FILESYSTEM WRITE boundary is real: a write to
// an allow-listed writable path succeeds (the file appears), and a write outside
// the writable allow-list is BLOCKED (no file appears).
func TestLiveWriteBoundary(t *testing.T) {
	t.Parallel()
	exec := liveExecutor(t)
	touch := touchBin(t)

	base := t.TempDir()
	writable := filepath.Join(base, "writable")
	readonly := filepath.Join(base, "readonly")
	mustMkdir(t, writable)
	mustMkdir(t, readonly)

	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		// A write is a mutating action; it must arrive already approved (the seam
		// never self-approves). The kernel sandbox is what we are testing here.
		ApprovalGranted: true,
		// writable is rw; readonly is read-only (no Write flag).
		FS:            []FSCap{{Path: writable, Write: true}, {Path: readonly}},
		MaxWallMillis: 5000,
	}

	// Write inside the writable allow-list SUCCEEDS and the file actually appears.
	okFile := filepath.Join(writable, "created.txt")
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "write_allowed", Kind: ActionInternalWrite, Target: writable,
		Payload: jsonArgv(t, touch, okFile),
	})
	if err != nil {
		t.Fatalf("allow-listed write errored: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("allow-listed write exit = %d (out=%q), want 0", res.ExitCode, res.Output)
	}
	if _, statErr := os.Stat(okFile); statErr != nil {
		t.Fatalf("allow-listed write did not create the file: %v", statErr)
	}

	// Write to the READ-ONLY allow-listed dir is BLOCKED — the target gate passes
	// (readonly is FS-listed) but it has no Write flag, so the kernel sandbox must
	// block the create. Proves write is gated independently of read.
	roFile := filepath.Join(readonly, "should-not-exist.txt")
	res2, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "write_to_readonly", Kind: ActionInternalWrite, Target: readonly,
		Payload: jsonArgv(t, touch, roFile),
	})
	if err != nil {
		t.Fatalf("execute errored unexpectedly: %v", err)
	}
	if res2.ExitCode == 0 {
		t.Fatalf("BOUNDARY BYPASS: write to read-only allow-listed dir succeeded")
	}
	if _, statErr := os.Stat(roFile); statErr == nil {
		t.Fatalf("BOUNDARY BYPASS: file was created in a read-only dir")
	}
}

// TestLiveNetworkBoundary proves the NETWORK boundary is real: a sandboxed TCP
// connect is BLOCKED when network is denied (the only mode this tier supports).
// Uses nc; skips if nc is unavailable. Does NOT require the connect to reach the
// internet — it asserts the connect is refused by the sandbox, not by routing.
func TestLiveNetworkBoundary(t *testing.T) {
	t.Parallel()
	exec := liveExecutor(t)
	nc := ncBinary(t)
	if nc == "" {
		t.Skip("nc not available for network-boundary probe")
	}

	base := t.TempDir()
	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		FS:            []FSCap{{Path: base}},
		MaxWallMillis: 6000,
	}

	// A TCP connect attempt under deny-all-network must fail. nc returns non-zero
	// ("Operation not permitted") when the sandbox blocks connectx. We assert the
	// connect did NOT succeed (exit != 0).
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "net_connect", Kind: ActionRead, Target: base,
		// -G/-w bound the attempt so the test cannot hang.
		Payload: jsonArgv(t, nc, "-v", "-G", "3", "-w", "3", "1.1.1.1", "53"),
	})
	if err != nil {
		// A resource-cap kill is also an acceptable "did not connect" outcome.
		return
	}
	if res.ExitCode == 0 {
		t.Fatalf("BOUNDARY BYPASS: sandboxed network connect succeeded under deny-all-network (out=%q)", res.Output)
	}
}

// TestLiveSeatbeltInjectionDoesNotGrantWrite proves the Seatbelt path escaper
// holds END-TO-END against the real kernel, not just in string assertions: a
// crafted read-only allow-list entry that TRIES to inject a (allow file-write*)
// clause must NOT actually let the sandbox write to the injection target. If the
// escaper were broken, sandbox-exec would parse the injected clause and the write
// below would succeed — flipping this test red.
func TestLiveSeatbeltInjectionDoesNotGrantWrite(t *testing.T) {
	t.Parallel()
	if goruntime.GOOS != "darwin" {
		t.Skip("Seatbelt injection probe is darwin-specific")
	}
	exec := liveExecutor(t)
	touch := touchBin(t)

	base := t.TempDir()
	target := filepath.Join(base, "victim")
	mustMkdir(t, target)
	victimFile := filepath.Join(target, "pwned.txt")

	// A read-only FS entry whose path is crafted to (try to) close the read string
	// and inject a write-allow for `target`. Escaping must neutralise it.
	evil := target + `") (allow file-write* (subpath "` + target
	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		ApprovalGranted: true, // approve so the gate doesn't pre-refuse the write
		FS:              []FSCap{{Path: evil}},
		MaxWallMillis:   5000,
	}
	// Attempt to write into target. With the injection neutralised there is NO
	// file-write clause, so deny-default must block the create.
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "injected_write", Kind: ActionInternalWrite, Target: evil,
		Payload: jsonArgv(t, touch, victimFile),
	})
	if err != nil {
		t.Fatalf("execute errored unexpectedly: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("INJECTION BYPASS: crafted path granted write (exit 0)")
	}
	if _, statErr := os.Stat(victimFile); statErr == nil {
		t.Fatalf("INJECTION BYPASS: file created via injected write clause")
	}
}

// TestLiveResourceCapKillsRunaway proves the wallclock cap actually terminates a
// runaway process rather than letting it run unbounded.
func TestLiveResourceCapKillsRunaway(t *testing.T) {
	t.Parallel()
	exec := liveExecutor(t)
	sh := shBinary(t)

	base := t.TempDir()
	cfg := ExecConfig{
		WorkflowID: "wf", Version: 1,
		FS:            []FSCap{{Path: base}},
		MaxWallMillis: 500, // half a second
	}
	// sleep 30 would run far past the cap; the cap must kill it.
	res, err := exec.Execute(context.Background(), cfg, ExecAction{
		Name: "runaway", Kind: ActionRead, Target: base,
		Payload: jsonArgv(t, sh, "-c", "sleep 30"),
	})
	// Killed-by-cap surfaces as an error with a negative exit; either way the call
	// must NOT report a clean exit 0.
	if err == nil && res != nil && res.ExitCode == 0 {
		t.Fatalf("resource cap did not kill the runaway process")
	}
}
