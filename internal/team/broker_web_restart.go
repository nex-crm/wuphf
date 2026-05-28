package team

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
)

type WebBrokerRestartStatus struct {
	OK  bool   `json:"ok"`
	URL string `json:"url,omitempty"`
}

// reExecHookMu guards the package-level re-exec hook + delay so tests can
// swap them without racing the handler goroutine that reads them.
var (
	reExecHookMu sync.RWMutex

	// reExecBrokerProcess replaces the current process image with a fresh
	// exec of the same binary path so a newly-installed version (from
	// `npm install -g wuphf@latest`) actually takes over. Implemented
	// per-platform in broker_reexec_*.go.
	reExecBrokerProcess = platformReExecBroker

	// brokerReExecDelay gives the HTTP response time to flush to the
	// client before the process image is replaced.
	brokerReExecDelay = 200 * time.Millisecond
)

// loadReExecHook captures the current hook + delay under the read lock so the
// handler goroutine uses a coherent pair even if a test swaps them mid-flight.
func loadReExecHook() (func() error, time.Duration) {
	reExecHookMu.RLock()
	defer reExecHookMu.RUnlock()
	return reExecBrokerProcess, brokerReExecDelay
}

func (b *Broker) handleWebBrokerRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b == nil {
		http.Error(w, "broker unavailable", http.StatusServiceUnavailable)
		return
	}
	// Refuse to schedule a re-exec on a broker that is already shutting
	// down — preserves the legacy behaviour of TestWebBrokerRestartRejectsAfterStop
	// and avoids racing the lifecycle teardown.
	if b.stopCh != nil {
		select {
		case <-b.stopCh:
			http.Error(w, "broker is shutting down", http.StatusServiceUnavailable)
			return
		default:
		}
	}
	if b.lifecycleCtx != nil {
		select {
		case <-b.lifecycleCtx.Done():
			http.Error(w, "broker is shutting down", http.StatusServiceUnavailable)
			return
		default:
		}
	}

	status := WebBrokerRestartStatus{
		OK:  true,
		URL: "http://" + b.Addr(),
	}
	// Confirm to the client first; the actual process replacement happens on
	// a short delay so the response reaches the browser before the binary is
	// replaced. The frontend's SSE client reconnects automatically once the
	// new binary is listening on the same port.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(status)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	reExec, delay := loadReExecHook()
	go func() {
		time.Sleep(delay)
		// syscall.Exec only returns on failure. If it succeeds, the new
		// binary takes over the process and this goroutine never resumes.
		// On Windows (or any platform where re-exec isn't supported) we
		// fall back to the in-process listener restart so the user at
		// least sees a reconnect, even though the version won't change
		// until they re-launch the CLI manually.
		if err := reExec(); err != nil {
			log.Printf("broker re-exec failed (%v); falling back to listener restart", err)
			if _, restartErr := b.RestartBrokerListener(); restartErr != nil {
				log.Printf("broker listener restart fallback failed: %v", restartErr)
			}
		}
	}()
}

func (b *Broker) RestartBrokerListener() (WebBrokerRestartStatus, error) {
	if b == nil {
		return WebBrokerRestartStatus{}, fmt.Errorf("broker unavailable")
	}
	b.brokerRestartMu.Lock()
	defer b.brokerRestartMu.Unlock()

	if b.stopCh != nil {
		select {
		case <-b.stopCh:
			return WebBrokerRestartStatus{}, fmt.Errorf("broker is shutting down")
		default:
		}
	}
	if b.lifecycleCtx != nil {
		select {
		case <-b.lifecycleCtx.Done():
			return WebBrokerRestartStatus{}, fmt.Errorf("broker is shutting down")
		default:
		}
	}

	if b.server != nil {
		_ = b.server.Close()
	}
	if b.listener != nil {
		_ = b.listener.Close()
	}
	if err := b.StartOnPort(b.restartBrokerPort()); err != nil {
		return WebBrokerRestartStatus{}, fmt.Errorf("restart broker listener: %w", err)
	}
	return WebBrokerRestartStatus{
		OK:  true,
		URL: "http://" + b.Addr(),
	}, nil
}

func (b *Broker) restartBrokerPort() int {
	if b != nil {
		if _, portRaw, err := net.SplitHostPort(b.Addr()); err == nil {
			if port, convErr := strconv.Atoi(portRaw); convErr == nil && port > 0 {
				return port
			}
		}
	}
	return brokeraddr.ResolvePort()
}
