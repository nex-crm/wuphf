package team

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
)

func (b *Broker) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetTasks(w, r)
	case http.MethodPost:
		b.handlePostTask(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	root := b.agentLogRoot
	b.mu.Unlock()
	if root == "" {
		root = agent.DefaultTaskLogRoot()
	}

	task := strings.TrimSpace(r.URL.Query().Get("task"))
	if task != "" {
		// Guard against path traversal — the task id is a single directory name.
		if strings.Contains(task, "..") || strings.ContainsAny(task, `/\`) {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		entries, err := agent.ReadTaskLog(root, task)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task":    task,
			"entries": entries,
		})
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	tasks, err := agent.ListRecentTasks(root, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
}

func (b *Broker) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	mySlug := strings.TrimSpace(r.URL.Query().Get("my_slug"))
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	allChannels := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all_channels")), "true")
	if channel == "" && !allChannels {
		channel = "general"
	}
	includeDone := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_done")), "true")

	b.mu.Lock()
	if !allChannels && !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	result := make([]teamTask, 0, len(b.tasks))
	// allChannels=true must NOT bypass channel authorization. Without this
	// per-task check, an authenticated viewer could enumerate every task in
	// every channel — including private ones they aren't a member of —
	// just by passing all_channels=true. Apply the same access predicate
	// to each candidate channel before letting the task into the response.
	allChannelsCache := make(map[string]bool)
	channelAllowed := func(slug string) bool {
		if !allChannels {
			return true
		}
		if v, ok := allChannelsCache[slug]; ok {
			return v
		}
		v := b.canAccessChannelLocked(viewerSlug, slug)
		allChannelsCache[slug] = v
		return v
	}
	for _, task := range b.tasks {
		taskChannel := normalizeChannelSlug(task.Channel)
		if !allChannels && taskChannel != channel {
			continue
		}
		if !channelAllowed(taskChannel) {
			continue
		}
		if task.Status == "done" && !includeDone && statusFilter == "" {
			continue
		}
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if mySlug != "" && task.Owner != "" && task.Owner != mySlug {
			continue
		}
		result = append(result, task)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "tasks": result})
}
