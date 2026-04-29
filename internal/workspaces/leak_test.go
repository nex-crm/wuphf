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
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// snapshotDir returns a map of relative-path→size for every file under dir.
// Returns an empty map (not an error) when dir does not exist.
func snapshotDir(t *testing.T, dir string) map[string]int64 {
	t.Helper()
	m := make(map[string]int64)
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
		m[rel] = info.Size()
		return nil
	})
	if err != nil {
		t.Logf("snapshotDir(%q) walk error (non-fatal): %v", dir, err)
	}
	return m
}

// diffSnapshots returns a human-readable description of files that appeared,
// disappeared, or changed size between before and after. Returns "" if equal.
func diffSnapshots(before, after map[string]int64) string {
	var sb strings.Builder
	for path, afterSize := range after {
		if beforeSize, ok := before[path]; !ok {
			fmt.Fprintf(&sb, "  NEW:     %s (%d bytes)\n", path, afterSize)
		} else if beforeSize != afterSize {
			fmt.Fprintf(&sb, "  CHANGED: %s (%d -> %d bytes)\n", path, beforeSize, afterSize)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			fmt.Fprintf(&sb, "  DELETED: %s\n", path)
		}
	}
	return sb.String()
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

// waitForPort polls addr (host:port) until it accepts a TCP connection or
// ctx is done.
func waitForPort(ctx context.Context, addr string) error {
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

	if err := waitForPort(waitCtx, brokerAddr); err != nil {
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
