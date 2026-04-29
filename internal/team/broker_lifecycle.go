package team

// broker_lifecycle.go owns the broker-side housekeeping helpers that used
// to sit in launcher.go (PLAN.md §C8): the office PID file (write, clear,
// kill-by-PID), the in-memory + persisted broker-state reset endpoints,
// the stale-broker-port killer used before LaunchWeb starts a fresh
// broker, and the small URL accessors. These are package-level helpers
// (no Launcher receiver) plus a single Launcher.BrokerBaseURL accessor;
// grouping them here keeps launcher.go focused on the orchestrator and
// lets the broker-state tests live alongside their subjects.
//
// No new types or behaviour changes — pure file split.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/config"
)

// killStaleBroker kills any process holding the configured broker port from a previous run.
func killStaleBroker() {
	out, err := exec.CommandContext(context.Background(), "lsof", "-i", fmt.Sprintf(":%d", brokeraddr.ResolvePort()), "-t").Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, pid := range strings.Fields(strings.TrimSpace(string(out))) {
		_ = exec.CommandContext(context.Background(), "kill", "-9", pid).Run()
	}
	time.Sleep(500 * time.Millisecond)
}

func ResetBrokerState() error {
	token := os.Getenv("WUPHF_BROKER_TOKEN")
	if token == "" {
		token = os.Getenv("NEX_BROKER_TOKEN")
	}
	return resetBrokerState(brokerBaseURL(), token)
}

func ClearPersistedBrokerState() error {
	path := defaultBrokerStatePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func officePIDFilePath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return filepath.Join(".wuphf", "team", "office.pid")
	}
	return filepath.Join(home, ".wuphf", "team", "office.pid")
}

func writeOfficePIDFile() error {
	path := officePIDFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func clearOfficePIDFile() error {
	path := officePIDFilePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func killPersistedOfficeProcess() error {
	raw, err := os.ReadFile(officePIDFilePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		// Stale or corrupt PID file: there's no process to kill, just
		// clear the file so the next launcher boots cleanly. Don't
		// surface the parse error — the caller's intent is "shut
		// anything down", and an unparseable PID file means there's
		// nothing to shut down.
		_ = clearOfficePIDFile()
		return nil //nolint:nilerr // intentional: corrupt PID file is a no-op
	}
	if pid == os.Getpid() {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		// On Unix os.FindProcess never errors, but cover the API
		// contract: if it ever does, clear the stale PID file and
		// continue — there's nothing to kill.
		_ = clearOfficePIDFile()
		return nil //nolint:nilerr // intentional: no process to kill, clear PID and move on
	}
	_ = proc.Kill()
	return nil
}

func resetBrokerState(baseURL, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/reset", nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("broker reset failed: %s", resp.Status)
	}
	return nil
}

func brokerBaseURL() string {
	return brokeraddr.ResolveBaseURL()
}

func (l *Launcher) BrokerBaseURL() string {
	if l != nil && l.broker != nil {
		if addr := strings.TrimSpace(l.broker.Addr()); addr != "" {
			return "http://" + addr
		}
	}
	return brokerBaseURL()
}
