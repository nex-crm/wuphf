package nex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeFakeNexCLI creates a shell script that simulates nex-cli on a temp
// PATH. The script echoes args + a canned response so tests can assert both
// detection and shell-out plumbing without touching the real binary.
func writeFakeNexCLI(t *testing.T, dir, name, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake shell script requires a POSIX shell")
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake nex-cli: %v", err)
	}
}

// withIsolatedPATH points PATH at a dedicated tmp dir and restores it on cleanup.
// Returns the tmp dir so the test can drop fake binaries into it.
func withIsolatedPATH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	return dir
}

func TestIsInstalled_Missing(t *testing.T) {
	withIsolatedPATH(t)
	t.Setenv("WUPHF_NO_NEX", "")
	if IsInstalled() {
		t.Fatal("expected nex-cli to be missing on an empty PATH")
	}
}

func TestIsInstalled_Present(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", "echo installed")
	if !IsInstalled() {
		t.Fatal("expected nex-cli to be detected")
	}
}

func TestConnected_DisabledBeatsInstalled(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", "echo installed")
	t.Setenv("WUPHF_NO_NEX", "1")
	if Connected() {
		t.Fatal("--no-nex must take precedence over detection")
	}
}

func TestConnected_Happy(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", "echo installed")
	t.Setenv("WUPHF_NO_NEX", "")
	if !Connected() {
		t.Fatal("expected Connected() when nex-cli is installed and --no-nex is off")
	}
}

func TestRun_ReturnsStdout(t *testing.T) {
	dir := withIsolatedPATH(t)
	// Assert nex-cli is invoked as `--cmd "<joined args>"`: $1 is --cmd,
	// $2 is the joined command string.
	writeFakeNexCLI(t, dir, "nex-cli", `printf '%s|%s' "$1" "$2"`)
	t.Setenv("WUPHF_NO_NEX", "")
	out, err := Run(context.Background(), "recall", "acme")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if out != "--cmd|recall acme" {
		t.Fatalf("Run: unexpected stdout %q", out)
	}
}

func TestRun_QuotesWhitespaceArgs(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", `printf '%s' "$2"`)
	t.Setenv("WUPHF_NO_NEX", "")
	out, err := Run(context.Background(), "recall", "acme q3 renewal")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if out != `recall "acme q3 renewal"` {
		t.Fatalf("Run: expected quoted multi-word arg, got %q", out)
	}
}

func TestRun_MissingBinary(t *testing.T) {
	withIsolatedPATH(t)
	t.Setenv("WUPHF_NO_NEX", "")
	_, err := Run(context.Background(), "recall", "foo")
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("Run: expected ErrNotInstalled, got %v", err)
	}
}

func TestRun_Disabled(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", "echo ok")
	t.Setenv("WUPHF_NO_NEX", "1")
	_, err := Run(context.Background(), "recall", "foo")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("Run: expected ErrDisabled, got %v", err)
	}
}

func TestRecall_ShellsOut(t *testing.T) {
	dir := withIsolatedPATH(t)
	// $2 is the full --cmd string: subcommand followed by the (quoted) query.
	writeFakeNexCLI(t, dir, "nex-cli", `printf '%s' "$2"`)
	t.Setenv("WUPHF_NO_NEX", "")
	out, err := Recall(context.Background(), "acme q3 renewal")
	if err != nil {
		t.Fatalf("Recall: unexpected error: %v", err)
	}
	if out != `recall "acme q3 renewal"` {
		t.Fatalf("Recall: expected quoted query in --cmd string, got %q", out)
	}
}

func TestRegister_PassesEmail(t *testing.T) {
	dir := withIsolatedPATH(t)
	// $2 is the --cmd string: `setup <email>` (single token, no quoting).
	writeFakeNexCLI(t, dir, "nex-cli", `printf '%s' "$2"`)
	t.Setenv("WUPHF_NO_NEX", "")
	out, err := Register(context.Background(), "founder@example.com")
	if err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}
	if out != "setup founder@example.com" {
		t.Fatalf("Register: expected `setup <email>` --cmd string, got %q", out)
	}
}

func TestRegister_RejectsEmpty(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", "echo ok")
	t.Setenv("WUPHF_NO_NEX", "")
	if _, err := Register(context.Background(), "  "); err == nil {
		t.Fatal("Register: expected error on blank email")
	}
}

func TestRun_Timeout(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", "sleep 2")
	t.Setenv("WUPHF_NO_NEX", "")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := Run(ctx, "slow"); err == nil {
		t.Fatal("Run: expected timeout error")
	}
}

// TestRegister_AgainstRealisticNexCLI simulates nex-cli 0.1.7's real
// behavior: refuse non-interactive invocation unless --cmd/--script is used,
// and recognize `setup <email>` (not `register`) as the onboarding entry
// point. This is the exact failure mode described in issue #102 — if the
// fix regresses, this test breaks.
func TestRegister_AgainstRealisticNexCLI(t *testing.T) {
	dir := withIsolatedPATH(t)
	writeFakeNexCLI(t, dir, "nex-cli", `
if [ "$1" != "--cmd" ] && [ "$1" != "--script" ]; then
  echo "Error: nex requires an interactive terminal. Use --cmd or --script for non-interactive mode." >&2
  exit 1
fi
case "$2" in
  "setup "*) printf 'registered %s\n' "${2#setup }" ;;
  "register"*) echo "Error: unknown command 'register'" >&2; exit 2 ;;
  *) echo "Error: unknown command" >&2; exit 2 ;;
esac
`)
	t.Setenv("WUPHF_NO_NEX", "")
	out, err := Register(context.Background(), "founder@example.com")
	if err != nil {
		t.Fatalf("Register: unexpected error against realistic nex-cli: %v", err)
	}
	if out != "registered founder@example.com" {
		t.Fatalf("Register: expected successful setup, got %q", out)
	}
}
