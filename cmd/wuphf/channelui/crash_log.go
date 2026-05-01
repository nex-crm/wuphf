package channelui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// AppendChannelCrashLog appends an RFC3339-stamped crash entry to
// the channel crash log at ChannelCrashLogPath, creating the
// containing directory (mode 0o700) and the log file (mode 0o600)
// when missing. Used by the bubble-tea panic recovery hook.
func AppendChannelCrashLog(details string) error {
	path := ChannelCrashLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "\n[%s]\n%s\n", time.Now().Format(time.RFC3339), details); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ChannelCrashLogPath returns the absolute path to the channel
// crash log: ~/.wuphf/logs/channel-crash.log when a home directory
// is available, otherwise a working-directory fallback so the
// log is still capturable in restricted environments.
func ChannelCrashLogPath() string {
	if home := config.RuntimeHomeDir(); home != "" {
		return filepath.Join(home, ".wuphf", "logs", "channel-crash.log")
	}
	return ".wuphf-channel-crash.log"
}
