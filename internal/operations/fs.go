package operations

import (
	"io/fs"
	"os"
	"path/filepath"
)

// fallbackFS is consulted by the template loaders when a filesystem read
// rooted at repoRoot fails (or when repoRoot is empty). Binaries built
// with `//go:embed all:templates/…` wire this in via SetFallbackFS so
// installs without a repo checkout (e.g. `npx wuphf`, `curl | bash`)
// still see the shipped operations and employee blueprints.
//
// When both the filesystem and the fallback have the requested file, the
// filesystem wins. This preserves the existing override pattern — a user
// who adds their own `templates/operations/<id>/blueprint.yaml` to their
// checkout still takes precedence over the embedded shipped blueprint
// with the same id.
var fallbackFS fs.FS

// SetFallbackFS registers the embedded templates FS. Safe to call more
// than once; last writer wins. Passing nil clears the fallback.
func SetFallbackFS(f fs.FS) { fallbackFS = f }

// readTemplateFile reads rel (a slash-separated path rooted at
// "templates/…") from the filesystem at repoRoot if present, otherwise
// from the fallback FS. Returns fs.ErrNotExist when neither holds the
// file.
func readTemplateFile(repoRoot, rel string) ([]byte, error) {
	if repoRoot != "" {
		raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err == nil {
			return raw, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if fallbackFS != nil {
		raw, err := fs.ReadFile(fallbackFS, filepath.ToSlash(rel))
		if err == nil {
			return raw, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fs.ErrNotExist
}

// listTemplateDirs returns the subdirectory names of rel (a
// slash-separated path like "templates/operations"), preferring the
// filesystem at repoRoot and falling back to the embedded FS. Returns
// (nil, nil) when neither location holds the directory — callers treat
// that as "no blueprints", consistent with the pre-embed behavior. When
// the directory exists but contains no subdirectories, returns an empty
// (non-nil) slice.
func listTemplateDirs(repoRoot, rel string) ([]string, error) {
	if repoRoot != "" {
		root := filepath.Join(repoRoot, rel)
		entries, err := os.ReadDir(root)
		if err == nil {
			return filterDirNames(entries), nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if fallbackFS != nil {
		entries, err := fs.ReadDir(fallbackFS, filepath.ToSlash(rel))
		if err == nil {
			return filterDirNames(entries), nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, nil
}

func filterDirNames(entries []fs.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		out = append(out, entry.Name())
	}
	return out
}
