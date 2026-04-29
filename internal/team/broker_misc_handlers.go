package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/nex"
)

func (b *Broker) handleHealth(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	mode := b.sessionMode
	agent := b.oneOnOneAgent
	focus := b.focusMode
	provider := b.runtimeProvider
	b.mu.Unlock()
	if strings.TrimSpace(provider) == "" {
		provider = config.ResolveLLMProvider("")
	}
	memoryStatus := ResolveMemoryBackendStatus()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                "ok",
		"session_mode":          mode,
		"one_on_one_agent":      agent,
		"focus_mode":            focus,
		"provider":              provider,
		"provider_model":        resolveProviderModel(provider),
		"memory_backend":        memoryStatus.SelectedKind,
		"memory_backend_active": memoryStatus.ActiveKind,
		"memory_backend_ready":  memoryStatus.ActiveKind != config.MemoryBackendNone,
		"nex_connected":         memoryStatus.ActiveKind == config.MemoryBackendNex && nex.Connected(),
		"build":                 buildinfo.Current(),
	})
}

// resolveProviderModel returns the effective model id for the active LLM
// provider so the web UI's status bar can show, e.g.
// "opencode · ollama/qwen2.5-coder:1.5b". Returns "" when the provider has
// no resolvable model (claude-code uses the CLI's bundled default unless the
// user overrides via --model; we don't parse that out here).
func resolveProviderModel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		// Empty cwd keeps the home-dir config lookup but skips the
		// workspace-relative walk — Broker doesn't know which workspace the
		// caller is in, and the status bar is a coarse indicator anyway.
		return config.ResolveCodexModel("")
	case "opencode":
		return config.ResolveOpencodeModel()
	default:
		return ""
	}
}

func (b *Broker) handleSessionMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mode, agent := b.SessionModeState()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_mode":     mode,
			"one_on_one_agent": agent,
		})
	case http.MethodPost:
		var body struct {
			Mode  string `json:"mode"`
			Agent string `json:"agent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := b.SetSessionMode(body.Mode, body.Agent); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		mode, agent := b.SessionModeState()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_mode":     mode,
			"one_on_one_agent": agent,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleFocusMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"focus_mode": b.FocusModeEnabled(),
		})
	case http.MethodPost:
		var body struct {
			FocusMode bool `json:"focus_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := b.SetFocusMode(body.FocusMode); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"focus_mode": b.FocusModeEnabled(),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.Reset()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (b *Broker) handleResetDM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Agent   string `json:"agent"`
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	agent := strings.TrimSpace(body.Agent)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	b.mu.Lock()
	// Keep only messages that are NOT direct exchanges between human and agent
	filtered := make([]channelMessage, 0, len(b.messages))
	removed := 0
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) != channel {
			filtered = append(filtered, msg)
			continue
		}
		// Remove if: human->agent or agent->human (direct messages only)
		isHuman := msg.From == "you" || msg.From == "human"
		isAgent := msg.From == agent
		if isHuman || isAgent {
			// Check if it's a direct message (not a delegation to others)
			if isAgent && len(msg.Tagged) > 0 {
				taggedHuman := false
				for _, t := range msg.Tagged {
					if t == "you" || t == "human" {
						taggedHuman = true
						break
					}
				}
				if !taggedHuman {
					// Agent message to other agents — keep it
					filtered = append(filtered, msg)
					continue
				}
			}
			removed++
			continue
		}
		filtered = append(filtered, msg)
	}
	b.messages = filtered
	_ = b.saveLocked()
	b.mu.Unlock()

	// Respawn the agent's Claude Code session to clear its context
	go respawnAgentPane(agent)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "removed": removed})
}

// respawnAgentPane restarts an agent's Claude Code session in its tmux pane.
func respawnAgentPane(slug string) {
	manifest := company.DefaultManifest()
	loaded, err := company.LoadManifest()
	if err == nil && len(loaded.Members) > 0 {
		manifest = loaded
	}

	for i, agent := range manifest.Members {
		if agent.Slug == slug {
			paneIdx := i + 1 // pane 0 is channel view
			target := fmt.Sprintf("wuphf-team:team.%d", paneIdx)
			// Send Ctrl+C to interrupt, then exit to terminate
			ctx := context.Background()
			_ = exec.CommandContext(ctx, "tmux", "-L", "wuphf", "send-keys", "-t", target, "C-c", "").Run()
			time.Sleep(500 * time.Millisecond)
			_ = exec.CommandContext(ctx, "tmux", "-L", "wuphf", "send-keys", "-t", target, "C-c", "").Run()
			time.Sleep(500 * time.Millisecond)
			// Respawn the pane with a fresh claude session
			_ = exec.CommandContext(ctx, "tmux", "-L", "wuphf", "respawn-pane", "-k", "-t", target).Run()
			return
		}
	}
}

func (b *Broker) handleSignals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	signals := make([]officeSignalRecord, len(b.signals))
	copy(signals, b.signals)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"signals": signals})
}

func (b *Broker) handleDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	decisions := make([]officeDecisionRecord, len(b.decisions))
	copy(decisions, b.decisions)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"decisions": decisions})
}

func (b *Broker) handleWatchdogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	alerts := make([]watchdogAlert, len(b.watchdogs))
	copy(alerts, b.watchdogs)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"watchdogs": alerts})
}

func (b *Broker) handleActions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		actions := make([]officeActionLog, len(b.actions))
		copy(actions, b.actions)
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"actions": actions})
	case http.MethodPost:
		var body struct {
			Kind       string   `json:"kind"`
			Source     string   `json:"source"`
			Channel    string   `json:"channel"`
			Actor      string   `json:"actor"`
			Summary    string   `json:"summary"`
			RelatedID  string   `json:"related_id"`
			SignalIDs  []string `json:"signal_ids"`
			DecisionID string   `json:"decision_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Kind) == "" || strings.TrimSpace(body.Summary) == "" {
			http.Error(w, "kind and summary required", http.StatusBadRequest)
			return
		}
		if err := b.RecordAction(
			body.Kind,
			body.Source,
			body.Channel,
			body.Actor,
			body.Summary,
			body.RelatedID,
			body.SignalIDs,
			body.DecisionID,
		); err != nil {
			http.Error(w, "failed to persist action", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(b.QueueSnapshot())
}

func (b *Broker) handleTelegramGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	groups := make([]map[string]any, 0)
	for chatID, title := range b.seenTelegramGroups {
		groups = append(groups, map[string]any{"chat_id": chatID, "title": title})
	}
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups})
}
