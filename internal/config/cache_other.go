//go:build !darwin

package config

func platformExcludePathFromBackup(string) error {
	return nil
}

func backupExclusionCacheKey(path string) string {
	return path
}
