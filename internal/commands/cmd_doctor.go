package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type brokerHealthResponse struct {
	Status              string `json:"status"`
	SessionMode         string `json:"session_mode"`
	FocusMode           bool   `json:"focus_mode"`
	Provider            string `json:"provider"`
	ProviderModel       string `json:"provider_model"`
	MemoryBackend       string `json:"memory_backend"`
	MemoryBackendActive string `json:"memory_backend_active"`
	MemoryBackendReady  bool   `json:"memory_backend_ready"`
	NexConnected        bool   `json:"nex_connected"`
}

func cmdDoctor(ctx *SlashContext, _ string) error {
	health, err := brokerHealth()
	if err != nil {
		ctx.AddMessage("system", fmt.Sprintf("Doctor check failed: %v", err))
		return nil
	}
	provider := health.Provider
	if health.ProviderModel != "" {
		provider += " · " + health.ProviderModel
	}
	lines := []string{
		"Doctor: " + firstNonEmpty(health.Status, "unknown"),
		"Provider: " + firstNonEmpty(provider, "unknown"),
		"Session: " + firstNonEmpty(health.SessionMode, "office"),
		fmt.Sprintf("Focus mode: %t", health.FocusMode),
		"Memory: " + firstNonEmpty(health.MemoryBackendActive, health.MemoryBackend),
		fmt.Sprintf("Memory ready: %t", health.MemoryBackendReady),
		fmt.Sprintf("Nex connected: %t", health.NexConnected),
	}
	ctx.AddMessage("system", strings.Join(lines, "\n"))
	return nil
}

func brokerHealth() (brokerHealthResponse, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, resolveBrokerBaseURL()+"/health", nil)
	if err != nil {
		return brokerHealthResponse{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return brokerHealthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return brokerHealthResponse{}, fmt.Errorf("broker %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var health brokerHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return brokerHealthResponse{}, err
	}
	return health, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
