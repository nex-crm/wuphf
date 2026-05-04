package team

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// handleBrokerRestart handles POST /broker/restart.
//
// It responds immediately with 202 Accepted, then spawns a fresh copy of the
// binary with the same arguments and exits the current process. The new process
// starts up cleanly, binds to the same port, and the browser's existing SSE
// EventSource reconnects automatically.
//
// The restart is soft-destructive: in-flight agent turns are abandoned. Persisted
// broker-state.json is not modified, so the roster, channels, and skills survive.
//
// The handler is intentionally not loopback-restricted (unlike /admin/pause) so
// the web UI can invoke it directly without the user needing shell access.
func (b *Broker) handleBrokerRestart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "restart accepted; broker will restart shortly",
	})

	go func() {
		// Let the response flush before tearing down.
		time.Sleep(adminPauseSelfShutdownDelay)
		b.execRestart()
	}()
}

// execRestart spawns a replacement process then exits the current one.
// If spawning fails the current broker keeps running — no downtime on error.
func (b *Broker) execRestart() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("broker/restart: get executable: %v — keeping current process alive", err)
		return
	}
	cmd := exec.CommandContext(context.Background(), exe, os.Args[1:]...) //nolint:gosec // intentional self-restart
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if startErr := cmd.Start(); startErr != nil {
		log.Printf("broker/restart: spawn replacement: %v — keeping current process alive", startErr)
		return
	}
	b.adminPauseExit(0)
}
