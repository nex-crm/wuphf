package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var excludedBackupPaths sync.Map

// RuntimeCacheDir returns the cache directory under the active WUPHF runtime
// home. Cache contents are disposable and should not be included in backups.
func RuntimeCacheDir() string {
	return CacheDirForRuntimeHome(RuntimeHomeDir())
}

// CacheDirForRuntimeHome returns the .wuphf cache directory for runtimeHome.
func CacheDirForRuntimeHome(runtimeHome string) string {
	runtimeHome = strings.TrimSpace(runtimeHome)
	if runtimeHome == "" {
		return ""
	}
	return filepath.Join(runtimeHome, ".wuphf", "cache")
}

// EnsureCacheDir creates dir and marks it as excluded from user backups on
// platforms that support that metadata. Backup exclusion is best-effort so a
// permissions issue cannot break the command that needed the cache.
func EnsureCacheDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = MarkPathExcludedFromBackup(dir)
	return nil
}

// EnsureRuntimeCacheDir creates the active runtime cache directory.
func EnsureRuntimeCacheDir() (string, error) {
	dir := RuntimeCacheDir()
	return dir, EnsureCacheDir(dir)
}

// MarkPathExcludedFromBackup marks an existing cache path as excluded from
// user backups. Missing paths are ignored so startup can repair old installs
// without creating cache directories that are not needed yet.
func MarkPathExcludedFromBackup(path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	paths := []string{path}
	if realPath, err := filepath.EvalSymlinks(path); err == nil && realPath != path {
		paths = append(paths, realPath)
	}
	for _, candidate := range paths {
		candidate = filepath.Clean(candidate)
		cacheKey := backupExclusionCacheKey(candidate)
		if _, loaded := excludedBackupPaths.Load(cacheKey); loaded {
			continue
		}
		if err := platformExcludePathFromBackup(candidate); err != nil {
			// Backup metadata is best-effort. A tmutil timeout, permission error,
			// or unsupported volume must not break the command that needed cache.
			continue
		}
		excludedBackupPaths.Store(cacheKey, struct{}{})
	}
	return nil
}
