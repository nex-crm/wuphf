package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

var tmutilAddExclusion = func(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/tmutil", "addexclusion", path)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("tmutil addexclusion %q: %w", path, ctx.Err())
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("tmutil addexclusion %q: %w: %s", path, err, msg)
		}
		return fmt.Errorf("tmutil addexclusion %q: %w", path, err)
	}
	return nil
}

func platformExcludePathFromBackup(path string) error {
	return tmutilAddExclusion(path)
}

func backupExclusionCacheKey(path string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return path
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return path
	}
	return fmt.Sprintf("darwin:%d:%d", stat.Dev, stat.Ino)
}
