package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// detectPathShadows returns other wuphf executables found on pathEnv besides
// selfExec. Paths are returned in PATH order and de-duplicated by real path
// (so a symlink pointing at the running binary is correctly ignored).
//
// Candidates that resolve to a sibling file in the same directory as the
// running binary are ignored too — this is the npm install layout, where
// the PATH entry symlinks at `wuphf.js` (a launcher) but the native binary
// is `wuphf` in the same node_modules/wuphf/bin dir. They are one install,
// not a shadow.
//
// Extracted for tests — os.Executable() and os.Getenv() are injected by the
// caller so unit tests can drive deterministic PATH layouts.
func detectPathShadows(selfExec, pathEnv string) []string {
	if selfExec == "" {
		return nil
	}
	selfReal, err := filepath.EvalSymlinks(selfExec)
	if err != nil {
		selfReal = selfExec
	}
	selfRealDir := filepath.Dir(selfReal)
	exe := "wuphf"
	if runtime.GOOS == "windows" {
		exe = "wuphf.exe"
	}
	seen := map[string]bool{selfReal: true}
	var shadows []string
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, exe)
		info, err := os.Stat(cand)
		if err != nil || info.IsDir() {
			continue
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			continue
		}
		real, err := filepath.EvalSymlinks(cand)
		if err != nil {
			real = cand
		}
		if seen[real] {
			continue
		}
		// Sibling file in the running binary's own directory — same install,
		// not a shadow. Covers the npm layout (wuphf + wuphf.js in the same
		// node_modules bin dir).
		if filepath.Dir(real) == selfRealDir {
			seen[real] = true
			continue
		}
		seen[real] = true
		shadows = append(shadows, cand)
	}
	return shadows
}

// warnPathShadow writes a one-time warning to w when other wuphf executables
// are on PATH besides the currently running binary. The classic trap: a
// hand-built copy in ~/.local/bin silently shadows a fresh npm install, so
// upgrades appear not to take effect.
func warnPathShadow(w io.Writer) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	shadows := detectPathShadows(self, os.Getenv("PATH"))
	if len(shadows) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w, "wuphf: warning: other wuphf binaries are on PATH and may shadow this one:")
	for _, s := range shadows {
		_, _ = fmt.Fprintf(w, "  %s\n", s)
	}
	_, _ = fmt.Fprintf(w, "  running: %s\n", self)
	_, _ = fmt.Fprintln(w, "  If `which wuphf` picks a different path, upgrades to this binary will have no effect until the other copy is removed.")
}

// shouldWarnShadow gates the warning so it fires only on interactive launches.
// Script-facing entrypoints (--version, --cmd, piped stdin) and internal
// subprocesses (--channel-view, mcp-team) keep their output clean.
func shouldWarnShadow(showVersion, channelView, cmdFlagSet, piped bool, subcmd string) bool {
	if showVersion || channelView || cmdFlagSet || piped {
		return false
	}
	if subcmd == "mcp-team" {
		return false
	}
	return true
}
