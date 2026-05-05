package team

// broker_wiki_maintenance.go wires the wiki maintenance assistant into the
// broker HTTP layer.
//
// Routes:
//   POST /wiki/maintenance/suggest — body {action, path}; returns MaintenanceSuggestion JSON.
//
// The handler does not auto-write. Suggestions are computed and returned;
// the user accepts a suggestion through the existing /wiki/write-human path
// (the same conflict-detection / expected_sha flow as the editor).

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type maintenanceSuggestRequest struct {
	Action MaintenanceAction `json:"action"`
	Path   string            `json:"path"`
}

func (b *Broker) handleWikiMaintenanceSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, "wiki backend unavailable", http.StatusServiceUnavailable)
		return
	}
	var req maintenanceSuggestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if req.Action == "" {
		http.Error(w, "action is required", http.StatusBadRequest)
		return
	}

	idx := b.WikiIndex()
	prov := &brokerLintProvider{}
	lint := NewLint(idx, worker, prov)
	assistant := NewMaintenanceAssistant(worker, idx, lint)

	suggestion, err := assistant.Suggest(r.Context(), req.Action, req.Path)
	if err != nil {
		log.Printf("wiki maintenance: suggest %s for %s: %v", req.Action, req.Path, err)
		http.Error(w, fmt.Sprintf("suggest failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(suggestion)
}
