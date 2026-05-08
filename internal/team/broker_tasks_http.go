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
		_ = json.NewEncoder(w).Encode(AgentLogEntriesResponse{Task: task, Entries: entries})
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	agentFilter := strings.TrimSpace(r.URL.Query().Get("agent"))

	// When filtering by agent we over-fetch so the limit applies after the
	// filter, not before — otherwise a busy office with many other agents can
	// exhaust the window before the requested agent's runs appear.
	scanLimit := limit
	if agentFilter != "" {
		scanLimit = 500
	}
	tasks, err := agent.ListRecentTasks(root, scanLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if agentFilter != "" {
		filtered := tasks[:0]
		for _, t := range tasks {
			if t.AgentSlug == agentFilter {
				filtered = append(filtered, t)
				if len(filtered) == limit {
					break
				}
			}
		}
		tasks = filtered
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AgentLogTasksResponse{Tasks: tasks})
}

func (b *Broker) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	result, err := b.ListTasks(TaskListRequest{
		StatusFilter: strings.TrimSpace(r.URL.Query().Get("status")),
		MySlug:       strings.TrimSpace(r.URL.Query().Get("my_slug")),
		ViewerSlug:   strings.TrimSpace(r.URL.Query().Get("viewer_slug")),
		Channel:      strings.TrimSpace(r.URL.Query().Get("channel")),
		AllChannels:  strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all_channels")), "true"),
		IncludeDone:  strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_done")), "true"),
	})
	if errors.Is(err, errTaskChannelAccessDenied) {
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
