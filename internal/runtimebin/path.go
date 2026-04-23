package runtimebin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	lookPathFn    = exec.LookPath
	userHomeFn    = os.UserHomeDir
	statFn        = os.Stat
	getenvFn      = os.Getenv
	setenvFn      = os.Setenv
	evalSymlinkFn = filepath.EvalSymlinks
)

// LookPath resolves a CLI binary from PATH plus common user/package-manager
// bin directories. macOS app/npm launches can inherit a minimal PATH that omits
// Homebrew or user bins, so onboarding should not report installed CLIs as
// missing just because the parent process skipped shell startup files.
func LookPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty executable name")
	}
	if path, err := lookPathFn(name); err == nil {
		return path, nil
	}
	if filepath.Base(name) != name {
		return "", exec.ErrNotFound
	}
	for _, dir := range fallbackDirs() {
		for _, candidate := range executableCandidates(dir, name) {
			if isExecutable(candidate) {
				ensureDirOnPATH(filepath.Dir(candidate))
				return candidate, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

func fallbackDirs() []string {
	var dirs []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		dirs = append(dirs, dir)
	}

	for _, dir := range filepath.SplitList(getenvFn("WUPHF_CLI_PATHS")) {
		add(dir)
	}

	if home, err := userHomeFn(); err == nil && strings.TrimSpace(home) != "" {
		add(filepath.Join(home, ".opencode", "bin"))
		add(filepath.Join(home, ".local", "bin"))
		add(filepath.Join(home, ".bun", "bin"))
		add(filepath.Join(home, "Library", "pnpm"))
		add(filepath.Join(home, ".npm-global", "bin"))
		add(filepath.Join(home, ".deno", "bin"))
		add(filepath.Join(home, ".cargo", "bin"))
	}

	add("/opt/homebrew/bin")
	add("/usr/local/bin")
	add("/home/linuxbrew/.linuxbrew/bin")

	seen := make(map[string]bool, len(dirs))
	out := dirs[:0]
	for _, dir := range dirs {
		clean := filepath.Clean(dir)
		if seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, clean)
	}
	return out
}

func executableCandidates(dir, name string) []string {
	candidates := []string{filepath.Join(dir, name)}
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		exts := filepath.SplitList(getenvFn("PATHEXT"))
		if len(exts) == 0 {
			exts = []string{".exe", ".cmd", ".bat"}
		}
		for _, ext := range exts {
			ext = strings.TrimSpace(ext)
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			candidates = append(candidates, filepath.Join(dir, name+ext))
		}
	}
	return candidates
}

func isExecutable(path string) bool {
	info, err := statFn(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func ensureDirOnPATH(dir string) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" {
		return
	}
	current := getenvFn("PATH")
	for _, existing := range filepath.SplitList(current) {
		if sameDir(existing, dir) {
			return
		}
	}
	next := dir
	if current != "" {
		next += string(os.PathListSeparator) + current
	}
	_ = setenvFn("PATH", next)
}

func sameDir(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aReal, aErr := evalSymlinkFn(a)
	bReal, bErr := evalSymlinkFn(b)
	if aErr == nil && bErr == nil {
		return filepath.Clean(aReal) == filepath.Clean(bReal)
	}
	return false
}
