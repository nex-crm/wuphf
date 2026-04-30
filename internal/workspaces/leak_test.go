// Package workspaces — integration leak test.
//
// TestRuntimeHomeIsolation is the Phase-0 regression safety net: it spawns a
// real wuphf binary (must be pre-built as ./wuphf-test-bin relative to repo
// root, or the test skips) with WUPHF_RUNTIME_HOME pointing at a temp
// directory, waits for the broker health endpoint to respond, then tears
// down and asserts that no files under $HOME/.wuphf/ were created or modified
// during the run.
//
// To run locally:
//
//	go build -o wuphf-test-bin ./cmd/wuphf && \
//	  go test ./internal/workspaces/ -run TestRuntimeHomeIsolation -v -timeout 60s
//
// The test is skipped in normal `go test ./...` runs unless WUPHF_TEST_BIN is
// set (pointing at the pre-built binary) or the file ./wuphf-test-bin exists
// at the repo root.
//
// grep-tag: PHASE0_HOMEDIR_AUDIT
package workspaces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	// leakTestBrokerPort is an offset port that should not collide with dev
	// (7899) or prod (7890) instances. Change if you have something on 7920.
	leakTestBrokerPort = 7920
	leakTestWebPort    = 7921
)

// hashSizeFallbackThreshold is the per-file size above which we skip the
// SHA-256 hash and fall back to size-only fingerprinting. Documented at the
// caller: the leak test's whole job is to detect *new state writes*, not
// catch every byte that flips inside a multi-megabyte log. Hashing 100MB log
// files on every test run blows the test's time budget for no real signal —
// a leak test that takes 30s to fingerprint scratch logs never gets run.
const hashSizeFallbackThreshold int64 = 10 * 1024 * 1024 // 10 MiB

// fileFingerprint captures everything we need to detect mutation on a path
// regardless of whether the byte count changed. `Hash` is the SHA-256 hex
// digest of file contents (when ≤ threshold and readable), or empty when
// we used the size-only fallback. `SymlinkTarget` is set for symlinks
// instead of `Hash` so a relink to the same name is still detected.
type fileFingerprint struct {
	Size          int64
	Hash          string // sha256 hex; "" when size-only fallback or symlink
	SymlinkTarget string // set for symlinks instead of Hash
}

// snapshotDir returns a map of relative-path→fingerprint for every regular
// file (and symlink) under dir. Returns an empty map (not an error) when dir
// does not exist.
//
// Skip rules (preserved from the original size-based version):
//   - Directories: walked into but not fingerprinted (mtime-only changes are
//     uninteresting; we care about contents).
//   - Permission errors / disappearing files: tolerated silently. The leak
//     check only cares about files that exist in BOTH snapshots; transient
//     temp files that vanish mid-walk are not signal.
//   - Files larger than hashSizeFallbackThreshold (10 MiB): fall back to
//     size-only fingerprinting to keep the test fast on large logs. A
//     same-size mutation in a 10MB+ file still escapes detection by design;
//     the assumption is that real WUPHF state writes are small registry /
//     config files, not 10MB+ binary blobs.
//
// Symlinks are recorded by their target (not followed) so a relink that
// preserves the link path but points at a different file is detected.
func snapshotDir(t *testing.T, dir string) map[string]fileFingerprint {
	t.Helper()
	m := make(map[string]fileFingerprint)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return m
	}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Tolerate permission errors on system dirs.
			return nil
		}
		if info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}

		// Symlinks: record target only. Lstat since filepath.Walk's Lstat-aware
		// behaviour means info already reflects the link (not its target) on
		// the platforms we care about.
		if info.Mode()&os.ModeSymlink != 0 {
			target, lerr := os.Readlink(path)
			if lerr != nil {
				// Symlink disappeared between walk and read — skip rather than
				// fail the test (mirrors the temp-file tolerance for regular
				// files below).
				return nil
			}
			m[rel] = fileFingerprint{Size: info.Size(), SymlinkTarget: target}
			return nil
		}

		// Skip non-regular files (devices, sockets, named pipes). Their
		// "contents" aren't a fixed byte stream, so hashing them is meaningless.
		if !info.Mode().IsRegular() {
			return nil
		}

		fp := fileFingerprint{Size: info.Size()}
		if info.Size() > hashSizeFallbackThreshold {
			// Size-only fallback for large files (see threshold doc above).
			m[rel] = fp
			return nil
		}

		hash, herr := hashFileContents(path)
		if herr != nil {
			// File disappeared mid-snapshot (temp file, racy state writer).
			// Skip rather than fail — the diff still catches new/deleted paths.
			if os.IsNotExist(herr) {
				return nil
			}
			// Permission error or transient I/O error: log and fall back to
			// size-only so we still detect *additions* of this path even if
			// we can't read it.
			t.Logf("snapshotDir(%q): hash %q failed (size-only fallback): %v", dir, rel, herr)
			m[rel] = fp
			return nil
		}
		fp.Hash = hash
		m[rel] = fp
		return nil
	})
	if err != nil {
		t.Logf("snapshotDir(%q) walk error (non-fatal): %v", dir, err)
	}
	return m
}

// hashFileContents returns the SHA-256 hex digest of a file's contents using
// a streaming reader (no full-file load) so 10MB-threshold files don't spike
// memory. Returns the underlying error untouched so callers can use
// os.IsNotExist for the disappearing-file race.
func hashFileContents(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // test helper, path comes from filepath.Walk under a t.TempDir() / $HOME tree
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// diffSnapshots returns a human-readable description of files that appeared,
// disappeared, or whose fingerprint changed between before and after. Returns
// "" if equal. Sorted by path so failure messages are deterministic.
func diffSnapshots(before, after map[string]fileFingerprint) string {
	var added, removed, changed []string

	for path, afterFP := range after {
		beforeFP, ok := before[path]
		if !ok {
			added = append(added, fmt.Sprintf("  NEW:     %s (%d bytes, hash=%s)", path, afterFP.Size, shortHash(afterFP.Hash)))
			continue
		}
		if !fingerprintsEqual(beforeFP, afterFP) {
			changed = append(changed, fmt.Sprintf("  CHANGED: %s (%s -> %s)", path,
				describeFingerprint(beforeFP), describeFingerprint(afterFP)))
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			removed = append(removed, fmt.Sprintf("  DELETED: %s", path))
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	var sb strings.Builder
	for _, line := range added {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	for _, line := range changed {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	for _, line := range removed {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// fingerprintsEqual treats two fingerprints as equivalent only when every
// recorded field matches. A size-only fallback (Hash=="") matches another
// size-only fingerprint with the same size — which is the documented
// limitation of files >10MiB.
func fingerprintsEqual(a, b fileFingerprint) bool {
	if a.Size != b.Size {
		return false
	}
	if a.SymlinkTarget != b.SymlinkTarget {
		return false
	}
	return a.Hash == b.Hash
}

// describeFingerprint formats a fingerprint for the diff message. Keeps the
// same `<bytes>` context the original size-only renderer had, plus a short
// hash prefix so the failure message can be read at a glance.
func describeFingerprint(fp fileFingerprint) string {
	if fp.SymlinkTarget != "" {
		return fmt.Sprintf("symlink→%s", fp.SymlinkTarget)
	}
	if fp.Hash == "" {
		return fmt.Sprintf("%d bytes (size-only)", fp.Size)
	}
	return fmt.Sprintf("%d bytes, hash=%s", fp.Size, shortHash(fp.Hash))
}

// shortHash truncates a SHA-256 hex digest to its first 12 chars so failure
// messages stay readable. Full hash is reproducible from the file content,
// so we don't need to print the whole thing in test logs.
func shortHash(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

// wuphfTestBinary returns the path to the wuphf binary for integration tests,
// or "" if neither WUPHF_TEST_BIN nor ./wuphf-test-bin exist.
func wuphfTestBinary(root string) string {
	if v := strings.TrimSpace(os.Getenv("WUPHF_TEST_BIN")); v != "" {
		return v
	}
	candidate := filepath.Join(root, "wuphf-test-bin")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// waitForTCPAddr polls addr (host:port) until it accepts a TCP connection or
// ctx is done. Distinct from spawn.go's waitForPort (HTTP HEAD on int port);
// this test path only needs to confirm the broker is bound to a loopback
// socket, not whether HTTP routing is up yet.
func waitForTCPAddr(ctx context.Context, addr string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// TestRuntimeHomeIsolation is the Phase-0 regression safety net.
//
// It spawns a real wuphf binary with WUPHF_RUNTIME_HOME=<tmpdir>, waits for
// the broker health endpoint, tears down, and asserts that no files under
// $HOME/.wuphf/ were created or modified during the run.
func TestRuntimeHomeIsolation(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := repoRoot(t)
	_ = thisFile

	bin := wuphfTestBinary(root)
	if bin == "" {
		t.Skip("wuphf binary not found — build with: go build -o wuphf-test-bin ./cmd/wuphf\n" +
			"or set WUPHF_TEST_BIN=/path/to/wuphf")
	}

	// Determine real user HOME for the leak check.
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	realWuphfDir := filepath.Join(realHome, ".wuphf")

	// Snapshot the real ~/.wuphf BEFORE starting the binary.
	before := snapshotDir(t, realWuphfDir)

	// Isolated runtime home — entirely in a tmp dir.
	runtimeHome := t.TempDir()

	// Build the command.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"--broker-port", fmt.Sprintf("%d", leakTestBrokerPort),
		"--web-port", fmt.Sprintf("%d", leakTestWebPort),
	)

	// Override HOME to an empty sub-dir so LLM CLI auth lookups don't find
	// real credentials (safest for a test binary run). WUPHF_RUNTIME_HOME
	// carries all WUPHF state; WUPHF_GLOBAL_HOME is kept as real HOME so
	// headlessCodexGlobalHomeDir still resolves correctly in code paths that
	// need it — but no WUPHF state should write there.
	fakeHome := filepath.Join(runtimeHome, "fake-home")
	if err := os.MkdirAll(fakeHome, 0o755); err != nil {
		t.Fatalf("mkdir fake home: %v", err)
	}
	cmd.Env = append(os.Environ(),
		"WUPHF_RUNTIME_HOME="+runtimeHome,
		"HOME="+fakeHome,
		"WUPHF_GLOBAL_HOME="+realHome,
	)

	// Capture combined output for diagnostics on failure.
	var outBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start wuphf binary: %v", err)
	}

	// Wait for the broker HTTP port to be available.
	brokerAddr := fmt.Sprintf("127.0.0.1:%d", leakTestBrokerPort)
	waitCtx, waitCancel := context.WithTimeout(ctx, 20*time.Second)
	defer waitCancel()

	if err := waitForTCPAddr(waitCtx, brokerAddr); err != nil {
		// Kill before logging output so the test doesn't hang.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("broker port %d never came up (timeout 20s): %v\nprocess output:\n%s",
			leakTestBrokerPort, err, outBuf.String())
	}

	// Hit the health endpoint to confirm the broker is actually serving.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/api/health", leakTestBrokerPort)
	resp, herr := http.Get(healthURL) //nolint:noctx // short-lived test
	if herr != nil {
		t.Logf("health check failed (non-fatal, port was open): %v", herr)
	} else {
		_ = resp.Body.Close()
		t.Logf("broker health: HTTP %d", resp.StatusCode)
	}

	// Graceful shutdown.
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}

	// Snapshot AFTER.
	after := snapshotDir(t, realWuphfDir)

	// Assert no leak.
	diff := diffSnapshots(before, after)
	if diff != "" {
		t.Errorf("PHASE0 LEAK DETECTED: $HOME/.wuphf/ was modified during isolated run.\n"+
			"WUPHF_RUNTIME_HOME was %q but state leaked to real home %q.\n\n"+
			"Changed files:\n%s\n"+
			"Process output:\n%s",
			runtimeHome, realWuphfDir, diff, outBuf.String())
	}

	// Assert runtime home was actually written to (sanity check that the
	// binary is actually using WUPHF_RUNTIME_HOME and not falling back).
	if _, err := os.Stat(filepath.Join(runtimeHome, ".wuphf")); os.IsNotExist(err) {
		t.Logf("WARNING: no .wuphf dir found under runtimeHome %q — binary may not have fully initialized (check process output)", runtimeHome)
		t.Logf("process output:\n%s", outBuf.String())
		// Treat as a soft warning, not a hard failure, because the binary may
		// exit before onboarding writes the dir (in CI this binary has no valid
		// config and may exit quickly). The no-leak assertion above is the hard gate.
	}
}

// TestSnapshotDir_DetectsSameSizeMutation is the regression guard for the
// CodeRabbit #3164366617 finding: the original size-only fingerprint missed
// in-place mutations that preserved byte length. Now that snapshotDir hashes
// content, mutating a file without changing its size MUST surface in the
// diff.
func TestSnapshotDir_DetectsSameSizeMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	original := []byte(`{"workspaces": ["main"]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}

	before := snapshotDir(t, dir)

	// Same length, different bytes — the exact regression class the previous
	// size-only fingerprint missed. Length is stable because we replace
	// "main" with "demo" (both 4 chars).
	mutated := []byte(`{"workspaces": ["demo"]}`)
	if len(mutated) != len(original) {
		t.Fatalf("test setup bug: mutation must preserve length, got %d vs %d", len(mutated), len(original))
	}
	if err := os.WriteFile(path, mutated, 0o600); err != nil {
		t.Fatalf("write mutated: %v", err)
	}

	after := snapshotDir(t, dir)
	diff := diffSnapshots(before, after)
	if diff == "" {
		t.Fatal("expected diff for same-size mutation, got empty (size-only fingerprint regression)")
	}
	if !strings.Contains(diff, "registry.json") {
		t.Fatalf("expected registry.json in diff, got %q", diff)
	}
	if !strings.Contains(diff, "CHANGED:") {
		t.Fatalf("expected CHANGED: marker in diff, got %q", diff)
	}
}

// TestSnapshotDir_NoFalsePositiveOnIdenticalContent guards the other
// direction: re-writing the exact same bytes (same content, same hash)
// must produce no diff. Catches a regression where snapshotDir hashed
// mtime or path metadata instead of contents.
func TestSnapshotDir_NoFalsePositiveOnIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	contents := []byte(`{"hello": "world"}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	before := snapshotDir(t, dir)

	// Touch mtime by re-writing identical bytes.
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	after := snapshotDir(t, dir)
	if diff := diffSnapshots(before, after); diff != "" {
		t.Fatalf("expected no diff for identical content, got: %s", diff)
	}
}

// TestSnapshotDir_NewAndDeletedPathsSurface confirms the existing add/remove
// detection still works after the fingerprint refactor.
func TestSnapshotDir_NewAndDeletedPathsSurface(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(gone, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old: %v", err)
	}

	before := snapshotDir(t, dir)

	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove old: %v", err)
	}
	added := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(added, []byte("new"), 0o600); err != nil {
		t.Fatalf("write new: %v", err)
	}

	after := snapshotDir(t, dir)
	diff := diffSnapshots(before, after)
	if !strings.Contains(diff, "NEW:") || !strings.Contains(diff, "new.txt") {
		t.Errorf("expected NEW: new.txt in diff, got %q", diff)
	}
	if !strings.Contains(diff, "DELETED:") || !strings.Contains(diff, "old.txt") {
		t.Errorf("expected DELETED: old.txt in diff, got %q", diff)
	}
}

// TestSnapshotDir_LargeFileFallsBackToSize covers the documented fallback:
// files above hashSizeFallbackThreshold are fingerprinted by size only, so
// a same-size mutation in a large log is intentionally NOT detected. This
// test pins the contract — if someone changes the threshold or forgets the
// fallback, this test surfaces the behavior change.
func TestSnapshotDir_LargeFileFallsBackToSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-file fallback test in -short mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	// Write hashSizeFallbackThreshold + 1 byte so the file is just over the
	// threshold. Use a sparse-friendly byte pattern to keep the test fast.
	size := hashSizeFallbackThreshold + 1
	original := make([]byte, size)
	for i := range original {
		original[i] = 'A'
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write big: %v", err)
	}

	snap := snapshotDir(t, dir)
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	fp, ok := snap[rel]
	if !ok {
		t.Fatalf("expected big.log in snapshot, missing")
	}
	if fp.Hash != "" {
		t.Errorf("expected size-only fallback (empty Hash) for >%d-byte file, got hash=%s",
			hashSizeFallbackThreshold, fp.Hash)
	}
	if fp.Size != size {
		t.Errorf("expected size=%d, got %d", size, fp.Size)
	}
}
