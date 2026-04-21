package team

// scanner_walk.go ports walk-dir.ts to Go. See scanner.go for the package-
// level overview.
//
// Key divergences from the TypeScript source:
//   - Symlinks are never followed (file or directory). nex-cli inherits
//     Node's readdirSync which does follow; that is unsafe for WUPHF where
//     scan roots are arbitrary user directories and a symlink to /etc
//     could land secrets in the wiki.
//   - Permission-denied subdirs are silently skipped rather than erroring.

import (
	"os"
	"path/filepath"
	"strings"
)

// SkipDirs mirrors walk-dir.ts SKIP_DIRS exactly. Kept in sync by
// convention; change only with a parallel update in nex-cli.
var SkipDirs = map[string]struct{}{
	"node_modules": {},
	".git":         {},
	"dist":         {},
	"build":        {},
	".next":        {},
	"__pycache__":  {},
	"vendor":       {},
	".venv":        {},
	".claude":      {},
	"coverage":     {},
	".turbo":       {},
	".cache":       {},
	".nyc_output":  {},
}

// maxScanDepth caps recursive descent. Matches the nex-cli default.
const maxScanDepth = 20

// WalkOptions drive WalkDir.
type WalkOptions struct {
	Extensions map[string]struct{}
	MaxDepth   int
	IgnoreDirs map[string]struct{}
}

// DiscoveredFile is one candidate surfaced by WalkDir.
type DiscoveredFile struct {
	AbsolutePath string
	RelativePath string
	Info         os.FileInfo
}

// WalkDir discovers candidate files under dir, honouring SKIP_DIRS, the
// extension allowlist, and depth cap. Symlinks are NOT followed.
func WalkDir(dir, cwd string, opts WalkOptions) []DiscoveredFile {
	if opts.IgnoreDirs == nil {
		opts.IgnoreDirs = SkipDirs
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = maxScanDepth
	}
	return walkDirRec(dir, cwd, opts, 0)
}

func walkDirRec(dir, cwd string, opts WalkOptions, depth int) []DiscoveredFile {
	if depth > opts.MaxDepth {
		return nil
	}
	// os.ReadDir honours Lstat semantics: symlinks show up as symlinks, not
	// as the types they target. Exactly what we want — never recurse into
	// symlinked directories, never read through file symlinks.
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Permission-denied or transient — skip silently. Matches TS.
		return nil
	}
	var results []DiscoveredFile
	for _, entry := range entries {
		name := entry.Name()
		if _, skip := opts.IgnoreDirs[name]; skip {
			continue
		}
		full := filepath.Join(dir, name)
		mode := entry.Type()
		if mode&os.ModeSymlink != 0 {
			continue // skip all symlinks
		}
		if entry.IsDir() {
			results = append(results, walkDirRec(full, cwd, opts, depth+1)...)
			continue
		}
		if !mode.IsRegular() {
			continue // devices, sockets, pipes
		}
		ext := strings.ToLower(filepath.Ext(name))
		if _, ok := opts.Extensions[ext]; !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // raced with deletion
		}
		rel, err := filepath.Rel(cwd, full)
		if err != nil {
			rel = full
		}
		results = append(results, DiscoveredFile{
			AbsolutePath: full,
			RelativePath: rel,
			Info:         info,
		})
	}
	return results
}
