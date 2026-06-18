package team

// custom_app_build.go — server-side publish build for App Builder apps.
//
// The App Builder agent submits SOURCE (a Vite/React/TS project), not a trusted
// bundle. The HOST owns the wire contract (the postMessage bridge + read policy)
// and the build, so a generated app can never ship a tampered bridge or an
// unverified bundle:
//
//  1. The agent's copies of the PROTECTED host-contract files are discarded and
//     replaced with the CANONICAL bytes from the embedded scaffold. The host owns
//     these — an app whose agent rewrote wuphf-bridge.ts still publishes with the
//     lean, robust canonical bridge.
//  2. The bundle is built server-side (`bun install` then `bun run build`) from
//     that source. The resulting dist/index.html is what gets stored — the
//     agent-submitted html is never trusted.
//  3. A build failure (tsc/vite error) does NOT publish: it returns a caller
//     error carrying the build output tail so register_app surfaces why.
//
// The bun-exec plumbing mirrors custom_app_dev.go (configureHeadlessProcess,
// bounded output) so the live preview and the publish build behave identically.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/templates"
)

const (
	// customAppBuildOutDir is where `bun run build` (vite + singlefile) writes the
	// sealed bundle. The entry within it is customAppEntry ("index.html").
	customAppBuildOutDir = "dist"
	// customAppBuildTimeout bounds the whole install+build. A warm bun cache
	// resolves install in a second or two and vite builds in ~1-3s; this is a
	// generous ceiling so a pathological build fails loudly instead of hanging.
	customAppBuildTimeout = 4 * time.Minute
	// customAppBuildLogTailBytes caps how much build output rides back in the
	// error so register_app shows the agent the failing tsc/vite lines without
	// dumping the entire (potentially huge) log.
	customAppBuildLogTailBytes = 4 * 1024
)

// customAppProtectedFiles maps a project-relative path (forward-slash) to its
// canonical source under the embedded scaffold root. On every publish the agent's
// copy at the key is overwritten with the embedded bytes at the value, so the
// host owns the contract regardless of what the agent submitted.
var customAppProtectedFiles = map[string]string{
	"src/wuphf-bridge.ts":    "app-scaffold/src/wuphf-bridge.ts",
	"src/wuphf-inspector.ts": "app-scaffold/src/wuphf-inspector.ts",
	"vite.config.ts":         "app-scaffold/vite.config.ts",
}

// canonicalProtectedFile returns the embedded canonical bytes for a protected
// project path. The bytes come from the SAME embed the scaffolder/dev server use
// (templates.AppScaffold), never the worktree filesystem at runtime.
func canonicalProtectedFile(projectRel string) ([]byte, error) {
	embedded, ok := customAppProtectedFiles[projectRel]
	if !ok {
		return nil, fmt.Errorf("app: %q is not a protected file", projectRel)
	}
	body, err := templates.AppScaffold.ReadFile(embedded)
	if err != nil {
		return nil, fmt.Errorf("app: read canonical %q: %w", projectRel, err)
	}
	return body, nil
}

// overwriteProtectedFiles replaces every protected host-contract file in the
// source map with its canonical embedded bytes (returning a NEW map; the input
// is not mutated). The agent's versions of these files are discarded — the host
// owns them — so a tampered bridge/inspector/config can never reach the build.
// Protected files are written even if the agent omitted them, so the build
// always has the host's contract present.
func overwriteProtectedFiles(files map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(files)+len(customAppProtectedFiles))
	for k, v := range files {
		out[k] = v
	}
	for projectRel := range customAppProtectedFiles {
		body, err := canonicalProtectedFile(projectRel)
		if err != nil {
			return nil, err
		}
		out[projectRel] = string(body)
	}
	return out, nil
}

// snapshotAppSource captures the current APP source under srcRoot (everything
// except the preserved build/install artifacts node_modules/dist/.vite) into a
// temp dir, so a failed publish build can roll the source back to its last good
// state. It returns:
//
//   - restore: clears the non-artifact entries under srcRoot and copies the
//     snapshot back, leaving the preserved artifacts (warm node_modules) in place.
//   - cleanup: removes the temp snapshot dir; safe to defer unconditionally.
//
// Rolling back matters for the security boundary: without it, a deliberately
// build-failing publish would leave the agent's (tampered) source on disk, and
// the live dev preview would hot-reload and RUN it even though the sealed bundle
// stayed canonical. With it, a failed build is a no-op on the running preview.
func snapshotAppSource(srcRoot string) (restore func() error, cleanup func(), err error) {
	snapDir, err := os.MkdirTemp("", "wuphf-app-src-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("app: snapshot source: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(snapDir) }

	if cerr := copySourceTree(srcRoot, snapDir); cerr != nil {
		cleanup()
		return nil, func() {}, cerr
	}

	restore = func() error {
		if rerr := clearSourceExceptArtifacts(srcRoot); rerr != nil {
			return fmt.Errorf("app: restore clear: %w", rerr)
		}
		return copySourceTree(snapDir, srcRoot)
	}
	return restore, cleanup, nil
}

// copySourceTree copies the app source files from src into dst, skipping the
// preserved build/install artifact dirs (node_modules/dist/.vite) at the top
// level — those are large and never part of the rolled-back source. A missing
// src is treated as an empty tree (a first publish has no prior source).
func copySourceTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("app: copy source read: %w", err)
	}
	for _, e := range entries {
		if customAppPreservedSrcDirs[e.Name()] {
			continue
		}
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if err := copyTreeEntry(from, to); err != nil {
			return err
		}
	}
	return nil
}

// copyTreeEntry recursively copies a file or directory, preserving the 0o600/
// 0o700 perms the store writes elsewhere.
func copyTreeEntry(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return fmt.Errorf("app: copy stat %q: %w", from, err)
	}
	if info.IsDir() {
		if err := os.MkdirAll(to, 0o700); err != nil {
			return fmt.Errorf("app: copy mkdir %q: %w", to, err)
		}
		entries, err := os.ReadDir(from)
		if err != nil {
			return fmt.Errorf("app: copy readdir %q: %w", from, err)
		}
		for _, e := range entries {
			if err := copyTreeEntry(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	body, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("app: copy read %q: %w", from, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return fmt.Errorf("app: copy mkdir %q: %w", filepath.Dir(to), err)
	}
	if err := writeFileAtomic(to, body, 0o600); err != nil {
		return fmt.Errorf("app: copy write %q: %w", to, err)
	}
	return nil
}

// buildAppBundle builds the agent's source into a single-file dist/index.html
// and returns its bytes. It is the HOST-owned build: the protected files were
// already overwritten with canonical bytes by the caller, so this trusts the
// tree on disk under srcDir. install/build run with bun, bounded by a context
// timeout, with stdout+stderr captured so a failure carries the tail.
//
// srcDir is the persisted app source directory (…/<id>/src). Building in place
// reuses its warm node_modules cache (preserved across publishes), so a republish
// install is cache-fast. The output lands at srcDir/dist/index.html.
func buildAppBundle(srcDir string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), customAppBuildTimeout)
	defer cancel()

	if out, err := runAppBuildStep(ctx, srcDir, "install"); err != nil {
		return nil, buildStepError("install", out, err)
	}
	if out, err := runAppBuildStep(ctx, srcDir, "run", "build"); err != nil {
		return nil, buildStepError("build", out, err)
	}

	distEntry := filepath.Join(srcDir, customAppBuildOutDir, customAppEntry)
	body, err := os.ReadFile(distEntry)
	if err != nil {
		return nil, newCustomAppCallerError(
			"app: build produced no %s/%s — check the build output",
			customAppBuildOutDir, customAppEntry,
		)
	}
	if strings.TrimSpace(string(body)) == "" {
		return nil, newCustomAppCallerError("app: built %s/%s is empty", customAppBuildOutDir, customAppEntry)
	}
	return body, nil
}

// runAppBuildStep runs one `bun <args...>` invocation in srcDir with stdout +
// stderr folded into a single capped buffer, returning that output. It mirrors
// custom_app_dev.go's exec discipline: a process group (configureHeadlessProcess)
// so a timeout kills the whole tree, and a bounded buffer so a runaway build log
// can't exhaust memory.
func runAppBuildStep(ctx context.Context, srcDir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bun", args...)
	cmd.Dir = srcDir
	configureHeadlessProcess(cmd)
	var buf cappedBuffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	// On timeout, kill the whole process group (CommandContext only signals the
	// immediate child); bun spawns workers that would otherwise linger.
	if ctx.Err() != nil {
		terminateHeadlessProcess(cmd)
	}
	return buf.Bytes(), err
}

// buildStepError wraps a failed build step as a caller error carrying the tail of
// the build output, so register_app shows the agent the failing tsc/vite lines
// rather than an opaque exit code.
func buildStepError(step string, out []byte, runErr error) error {
	tail := strings.TrimSpace(string(tailBytes(out, customAppBuildLogTailBytes)))
	reason := runErr.Error()
	if errors.Is(runErr, context.DeadlineExceeded) {
		reason = fmt.Sprintf("timed out after %s", customAppBuildTimeout)
	}
	if tail == "" {
		return newCustomAppCallerError("app: %s failed (%s)", step, reason)
	}
	return newCustomAppCallerError("app: %s failed (%s)\n%s", step, reason, tail)
}

// tailBytes returns the last n bytes of b (all of b when shorter).
func tailBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}

// cappedBuffer is an io.Writer that retains only the last cappedBufferMax bytes
// written — enough headroom over the reported tail (customAppBuildLogTailBytes)
// that the tail is never truncated mid-line, while still bounding memory so a
// runaway build log can't exhaust it.
type cappedBuffer struct {
	buf bytes.Buffer
}

// cappedBufferMax is the in-memory ceiling: generous over the reported tail so
// the tail is never truncated mid-line, but still bounded.
const cappedBufferMax = 64 * 1024

func (c *cappedBuffer) Write(p []byte) (int, error) {
	n, err := c.buf.Write(p)
	if c.buf.Len() > cappedBufferMax {
		// Keep only the last cappedBufferMax bytes. Reset + re-write the same
		// buffer (a bytes.Buffer must not be copied by value after first use).
		b := c.buf.Bytes()
		keep := make([]byte, cappedBufferMax)
		copy(keep, b[len(b)-cappedBufferMax:])
		c.buf.Reset()
		c.buf.Write(keep)
	}
	return n, err
}

func (c *cappedBuffer) Bytes() []byte { return c.buf.Bytes() }

// ensure cappedBuffer satisfies io.Writer.
var _ io.Writer = (*cappedBuffer)(nil)
