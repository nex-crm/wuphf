// Package workspaces contains Phase-0 audit regression tests for the
// multi-workspace feature. These tests enforce that every os.UserHomeDir() and
// os.Getenv("HOME") call in cmd/ and internal/ (outside the carved-out
// paths) is either in the explicit allowlist or is the RuntimeHomeDir
// definition itself. Any NEW hit that isn't in the allowlist fails the build,
// forcing authors to consciously decide: migrate to config.RuntimeHomeDir() or
// add a carve-out comment and expand the allowlist.
//
// grep-tag: PHASE0_HOMEDIR_AUDIT
package workspaces

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// allowedFiles is the exhaustive set of source files (relative to repo root)
// that are permitted to call os.UserHomeDir() or os.Getenv("HOME") directly.
// Each entry must have a "user-global; intentionally NOT under WUPHF_RUNTIME_HOME"
// comment at the call site, or be the RuntimeHomeDir definition itself.
//
// To add a new carve-out: add the file here AND add the comment in the source.
// To migrate a site: remove it from here AND replace the call with config.RuntimeHomeDir().
var allowedFiles = map[string]string{
	// RuntimeHomeDir definition — this IS the function being migrated TO.
	"internal/config/config.go": "RuntimeHomeDir definition + codex config layering + OpenClaw identity carve-out",

	// Codex auth paths — user-global, subprocess inherits real HOME for tool resolution.
	"internal/team/headless_codex.go": "codex HOME passthrough + headlessCodexHomeDir (auth) + headlessCodexGlobalHomeDir",

	// Opencode — real HOME needed for base config read (auth.json) + HOME env passthrough.
	"internal/team/headless_opencode.go": "opencode HOME env passthrough + base config path read from real user HOME",

	// gbrain — user-global MCP subprocess, real HOME for subprocess auth.
	"internal/team/memory_backend.go": "gbrainMCPEnv + gbrainMCPEnvVars — gbrain is user-global",

	// npm install detection — walks up from user real HOME, not WUPHF state.
	// broker_upgrade.go is the refactored split of broker.go; same rationale.
	"internal/team/broker.go":         "detectLocalInstall — npm dependency walk from user HOME",
	"internal/team/broker_upgrade.go": "detectLocalInstall — npm dependency walk from user HOME (split from broker.go)",

	// Codex runner carve-outs — HOME passthrough to subprocess + codex auth path + global home resolver.
	"internal/team/headless_codex_runner.go": "HOME passthrough + headlessCodexHomeDir (~/.codex auth) + headlessCodexGlobalHomeDir",

	// Opencode MCP config — base opencode.json is user-global (read-only); agent config writes use RuntimeHomeDir.
	"internal/team/headless_opencode_mcp.go": "base opencode config read from real user HOME (user-global); agent config write uses RuntimeHomeDir",

	// OpenClaw probe utility — device-bound identity credentials.
	"cmd/wuphf-oc-probe/main.go": "OpenClaw identity is device-bound credentials, not workspace state",

	// Cross-workspace registry root — ~/.wuphf-spaces/ is the SHARED orchestration
	// directory (registry.json, tokens/, <name>/.wuphf/, .backups/). It must live
	// at the user's REAL home, not under any single workspace's WUPHF_RUNTIME_HOME,
	// or sibling brokers cannot find each other.
	"internal/workspaces/registry.go":    "spacesDir — ~/.wuphf-spaces is shared cross-workspace, lives at real HOME",
	"internal/workspaces/migration.go":   "MigrateToSymmetric operates on legacy ~/.wuphf at real HOME, not WUPHF_RUNTIME_HOME",
	"internal/workspaces/doctor_fix.go":  "symlinkPaths — ~/.wuphf compatibility symlink lives at real HOME",
	"internal/workspaces/paths.go":       "realHomeDir — cross-workspace token + symlink paths must live at real HOME",
	"internal/team/broker_workspaces.go": "workspaceTokenDir — same shared spaces directory rationale",
	"cmd/wuphf/workspaces_adapter.go":    "listTrashEntries — ~/.wuphf-spaces/.backups, shared cross-workspace root",
	"cmd/wuphf/main.go":                  "WUPHF_GLOBAL_HOME captures real HOME before any WUPHF_RUNTIME_HOME override",
}

// repoRoot returns the absolute path to the repository root by walking up from
// this test file's location until go.mod is found.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod — are tests running inside the repo?")
		}
		dir = parent
	}
}

// TestPhase0HomeDirAudit greps cmd/ and internal/ for os.UserHomeDir() and
// os.Getenv("HOME") calls (excluding test files, provider/, gbrain/,
// action/one) and asserts every hit is in the allowedFiles map.
//
// grep-tag: PHASE0_HOMEDIR_AUDIT
func TestPhase0HomeDirAudit(t *testing.T) {
	root := repoRoot(t)

	// Run the same grep the ledger specifies.
	cmd := exec.Command("grep", "-rn",
		`os\.UserHomeDir()\|os\.Getenv("HOME")`,
		filepath.Join(root, "cmd"),
		filepath.Join(root, "internal"),
	)
	out, err := cmd.Output()
	// grep exits 1 when no matches — that would be ideal but unexpected; treat
	// exit 1 with empty output as "no hits" (pass). Any other error is fatal.
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && len(out) == 0 {
			return // no hits — nothing to check
		}
		t.Fatalf("grep failed: %v\noutput: %s", err, out)
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()

		// Strip the repo-root prefix so we get a repo-relative path.
		relLine := strings.TrimPrefix(line, root+"/")
		relLine = strings.TrimPrefix(relLine, root+string(filepath.Separator))

		// Extract the file path (everything before the first colon) BEFORE
		// applying file-path filters so the carve-out and _test checks
		// don't false-positive on source lines that happen to contain
		// "provider/" or "_test." substrings.
		parts := strings.SplitN(relLine, ":", 2)
		if len(parts) < 2 {
			continue
		}
		hitFile := filepath.ToSlash(parts[0])

		// Skip _test files (the ledger's grep does `grep -v _test`; replicate here).
		if strings.HasSuffix(hitFile, "_test.go") {
			continue
		}
		// Skip carved-out packages — their internal calls are expected.
		// Match either the package directory (e.g. internal/action/one/foo.go)
		// or a single sibling file (e.g. internal/action/one.go).
		if strings.Contains(hitFile, "/provider/") ||
			strings.Contains(hitFile, "/gbrain/") ||
			strings.Contains(hitFile, "/action/one/") ||
			strings.HasSuffix(hitFile, "/action/one.go") {
			continue
		}

		// Check against the allowlist using exact-key lookup so unrelated
		// files with the same trailing path can't sneak past the audit.
		_, matched := allowedFiles[hitFile]
		if !matched {
			t.Errorf("PHASE0 AUDIT FAILURE: unexpected os.UserHomeDir()/os.Getenv(\"HOME\") call in %q\n"+
				"  Full grep line: %s\n"+
				"  Action required: either migrate to config.RuntimeHomeDir() OR add a\n"+
				"  'user-global; intentionally NOT under WUPHF_RUNTIME_HOME' comment\n"+
				"  and add the file to the allowedFiles map in internal/workspaces/phase0_test.go",
				hitFile, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
}
