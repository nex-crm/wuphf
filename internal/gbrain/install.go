package gbrain

// install.go orchestrates gbrain's OFFICIAL install path (INSTALL_FOR_AGENTS.md)
// on demand, so the user can enable semantic memory entirely from the UI:
//
//	curl -fsSL https://bun.sh/install | bash      # Bun (gbrain's runtime)
//	export PATH="$HOME/.bun/bin:$PATH"
//	bun install -g github:garrytan/gbrain          # gbrain, latest from source
//	gbrain apply-migrations --yes                  # recover blocked postinstall
//
// It is idempotent (a no-op when gbrain is already on PATH), best-effort (the
// caller treats failure as "stay on the keyword/markdown fallback"), and streams
// human-readable progress so the onboarding UI can show it. Running a remote
// install script is a real action; it fires ONLY on an explicit user opt-in.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	gbrainRepoSpec = "github:garrytan/gbrain"
	bunInstallURL  = "https://bun.sh/install"
)

// Seams (package-level so tests can drive the orchestration without a real
// network install). Production wires the real implementations.
var (
	isInstalledFn = IsInstalled
	resolveBunFn  = resolveBun
	runStreamFn   = runStream
)

// bunBinDir is where `bun install -g` places binaries (and where the bun
// installer drops `bun` itself).
func bunBinDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".bun", "bin")
	}
	return ""
}

// resolveBun returns a runnable bun path (PATH first, then ~/.bun/bin), or "".
func resolveBun() string {
	if p, err := exec.LookPath("bun"); err == nil {
		return p
	}
	if dir := bunBinDir(); dir != "" {
		cand := filepath.Join(dir, "bun")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}

// EnsureInstalled installs gbrain via its official path when absent. progress
// (nil-safe) receives step headlines and streamed command output. On success it
// pins WUPHF_GBRAIN_COMMAND at the freshly installed binary so the rest of the
// process finds gbrain without a shell restart.
func EnsureInstalled(ctx context.Context, progress func(string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	if isInstalledFn() {
		progress("gbrain already installed")
		return nil
	}

	bun := resolveBunFn()
	if bun == "" {
		progress("Installing Bun (gbrain's runtime)...")
		// The official bootstrap: curl -fsSL https://bun.sh/install | bash.
		if err := runStreamFn(ctx, nil, progress, "bash", "-c", "curl -fsSL "+bunInstallURL+" | bash"); err != nil {
			return fmt.Errorf("install bun: %w", err)
		}
		if bun = resolveBunFn(); bun == "" {
			return fmt.Errorf("bun not found after install (expected under %s)", bunBinDir())
		}
	}

	progress("Installing gbrain (latest)...")
	// Put ~/.bun/bin first so the global install lands there and a subsequent
	// gbrain lookup resolves it.
	env := os.Environ()
	if dir := bunBinDir(); dir != "" {
		env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	if err := runStreamFn(ctx, env, progress, bun, "install", "-g", gbrainRepoSpec); err != nil {
		return fmt.Errorf("bun install gbrain: %w", err)
	}

	// Pin the installed binary for this process (BinaryPath checks
	// WUPHF_GBRAIN_COMMAND first), so we do not depend on a PATH restart.
	if dir := bunBinDir(); dir != "" {
		gb := filepath.Join(dir, "gbrain")
		if st, err := os.Stat(gb); err == nil && !st.IsDir() {
			_ = os.Setenv("WUPHF_GBRAIN_COMMAND", gb)
		}
	}
	if !isInstalledFn() {
		return fmt.Errorf("gbrain not runnable after install")
	}

	// Bun occasionally blocks the top-level postinstall on global installs, so
	// schema migrations may not have run. Recover them best-effort (issue #218).
	progress("Applying gbrain migrations...")
	if gb := BinaryPath(); gb != "" {
		if err := runStreamFn(ctx, nil, progress, gb, "apply-migrations", "--yes", "--non-interactive"); err != nil {
			progress("migration recovery skipped: " + err.Error())
		}
	}

	progress("gbrain installed")
	return nil
}

// runStream runs name+args (with env, nil = inherit) and forwards each combined
// stdout/stderr line to onLine. A reader goroutine drains the pipe for the whole
// run, so there is no read-before-Wait deadlock.
func runStream(ctx context.Context, env []string, onLine func(string), name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	done := make(chan struct{})
	go func() {
		scanLines(pr, onLine)
		close(done)
	}()
	err := cmd.Run()
	_ = pw.Close() // signal EOF to the scanner
	<-done
	return err
}

func scanLines(r io.Reader, onLine func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := strings.TrimRight(sc.Text(), "\r"); strings.TrimSpace(line) != "" {
			onLine(line)
		}
	}
}
