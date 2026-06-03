package workspaces

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nex-crm/wuphf/internal/config"
)

// EnsureCacheBackupExclusions repairs existing workspace cache directories so
// disposable .wuphf/cache data does not participate in Time Machine backups.
func EnsureCacheBackupExclusions() error {
	reg, err := Read()
	if err != nil {
		if errors.Is(err, ErrRegistryNotFound) {
			return nil
		}
		return err
	}
	var errs []error
	for _, ws := range reg.Workspaces {
		if ws == nil || ws.RuntimeHome == "" {
			continue
		}
		cacheDir := config.CacheDirForRuntimeHome(ws.RuntimeHome)
		if cacheDir == "" {
			continue
		}
		info, err := os.Stat(cacheDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("stat cache dir %s: %w", cacheDir, err))
			continue
		}
		if !info.IsDir() {
			continue
		}
		if err := config.MarkPathExcludedFromBackup(filepath.Clean(cacheDir)); err != nil {
			errs = append(errs, fmt.Errorf("exclude cache dir %s from backup: %w", cacheDir, err))
		}
	}
	return errors.Join(errs...)
}
