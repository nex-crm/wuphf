package team

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
)

type WebBrokerRestartStatus struct {
	OK  bool   `json:"ok"`
	URL string `json:"url,omitempty"`
}

func (b *Broker) handleWebBrokerRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, err := b.RestartBrokerListener()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
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
