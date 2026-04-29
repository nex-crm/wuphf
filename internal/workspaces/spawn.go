//go:build darwin || linux

package workspaces

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/nex-crm/wuphf/internal/runtimebin"
)

const (
	spawnStartTimeout  = 30 * time.Second
	spawnPollInterval  = 200 * time.Millisecond
	spawnBrokerLogName = "broker.log"
)

// spawnFn is the function used to launch a broker subprocess. Overridden in
// tests to record calls without actually spawning.
var spawnFn = defaultSpawn

// Spawn starts a sibling broker process for the workspace identified by name,
// using brokerPort and webPort. The child runs in a detached process group
// (Setpgid: true) so it survives its parent's exit. Stdout and stderr are
// redirected to <runtimeHome>/.wuphf/logs/broker.log.
//
// Spawn returns when the child's broker port accepts HTTP HEAD requests, or
// when spawnStartTimeout elapses (error).
func Spawn(name string, runtimeHome string, brokerPort, webPort int) error {
	return spawnFn(name, runtimeHome, brokerPort, webPort)
}

func defaultSpawn(name string, runtimeHome string, brokerPort, webPort int) error {
	binary, err := resolveBinary()
	if err != nil {
		return fmt.Errorf("workspaces: spawn %q: resolve binary: %w", name, err)
	}

	logDir := filepath.Join(runtimeHome, ".wuphf", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("workspaces: spawn %q: mkdir logs: %w", name, err)
	}

	logPath := filepath.Join(logDir, spawnBrokerLogName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("workspaces: spawn %q: open log %s: %w", name, logPath, err)
	}
	defer logFile.Close()

	cmd := exec.Command(binary,
		"--broker-port", strconv.Itoa(brokerPort),
		"--web-port", strconv.Itoa(webPort),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	// Inherit the current environment but override workspace-specific vars.
	// HOME is explicitly preserved so LLM CLI auth (codex, claude, opencode)
	// continues to read from the real user home — NOT from runtimeHome.
	env := os.Environ()
	env = appendOrReplace(env, "WUPHF_RUNTIME_HOME", runtimeHome)
	env = appendOrReplace(env, "WUPHF_BROKER_PORT", strconv.Itoa(brokerPort))
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("workspaces: spawn %q: start process: %w", name, err)
	}

	// Write PID file for reconciliation.
	pidPath := filepath.Join(runtimeHome, ".wuphf", "broker.pid")
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)

	// Wait for the broker to bind its port.
	ctx, cancel := context.WithTimeout(context.Background(), spawnStartTimeout)
	defer cancel()
	if err := waitForPort(ctx, brokerPort); err != nil {
		return fmt.Errorf("workspaces: spawn %q: broker did not bind port %d within %s: %w",
			name, brokerPort, spawnStartTimeout, err)
	}
	return nil
}

// waitForPort polls brokerPort with HTTP HEAD until it responds or ctx expires.
func waitForPort(ctx context.Context, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	client := &http.Client{Timeout: spawnPollInterval}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := client.Head(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(spawnPollInterval):
		}
	}
}

// resolveBinary returns the path to the running wuphf binary using
// os.Executable, falling back to runtimebin.LookPath("wuphf").
func resolveBinary() (string, error) {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe, nil
	}
	return runtimebin.LookPath("wuphf")
}

// appendOrReplace returns a copy of env with key=val set. If key already
// exists in env its value is replaced; otherwise the pair is appended.
func appendOrReplace(env []string, key, val string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	found := false
	for _, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			out = append(out, prefix+val)
			found = true
		} else {
			out = append(out, e)
		}
	}
	if !found {
		out = append(out, prefix+val)
	}
	return out
}
