package team

// broker_learning.go wires the typed team learnings surface onto the broker.
//
// Route map:
//
//	POST /learning/record   — append one validated learning
//	GET  /learning/search   — search/dedup scoped learnings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (b *Broker) TeamLearningLog() *LearningLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.teamLearningLog
}

func (b *Broker) SetTeamLearningLog(log *LearningLog) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.teamLearningLog = log
}

func (b *Broker) ensureTeamLearningLog() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.wikiWorker == nil || b.teamLearningLog != nil {
		return
	}
	b.teamLearningLog = NewLearningLog(b.wikiWorker)
}

// handleLearningRecord is POST /learning/record.
func (b *Broker) handleLearningRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.ensureTeamLearningLog()
	log := b.TeamLearningLog()
	if log == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team learnings backend is not active"})
		return
	}
	var body LearningRecord
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(body.CreatedBy) == "" {
		body.CreatedBy = strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
	}
	if strings.TrimSpace(body.CreatedBy) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "created_by or X-WUPHF-Agent header is required"})
		return
	}
	rec, err := log.Append(r.Context(), body)
	if err != nil {
		writeJSON(w, learningAppendHTTPStatus(err), map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"learning":   rec,
		"jsonl_path": TeamLearningsJSONLPath,
		"wiki_path":  TeamLearningsPagePath,
	})
}

func learningAppendHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrInvalidLearning):
		return http.StatusBadRequest
	case errors.Is(err, ErrLearningLogNotRunning), errors.Is(err, ErrWorkerStopped), errors.Is(err, ErrQueueSaturated):
		return http.StatusServiceUnavailable
	case strings.Contains(err.Error(), "timed out"):
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// handleLearningSearch is GET /learning/search.
func (b *Broker) handleLearningSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.ensureTeamLearningLog()
	log := b.TeamLearningLog()
	if log == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team learnings backend is not active"})
		return
	}
	q := r.URL.Query()
	filters := LearningSearchFilters{
		Query:        q.Get("query"),
		Scope:        q.Get("scope"),
		Type:         LearningType(q.Get("type")),
		Source:       LearningSource(q.Get("source")),
		PlaybookSlug: q.Get("playbook_slug"),
		File:         q.Get("file"),
	}
	if raw := strings.TrimSpace(q.Get("trusted")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("trusted must be true or false; got %q", raw)})
			return
		}
		filters.Trusted = &v
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("limit must be an integer; got %q", raw)})
			return
		}
		filters.Limit = n
	}
	if filters.Type != "" && !isValidLearningType(filters.Type) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown type %q", filters.Type)})
		return
	}
	if filters.Source != "" && !isValidLearningSource(filters.Source) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unknown source %q", filters.Source)})
		return
	}
	results, err := log.Search(filters)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []LearningSearchResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"learnings": results,
		"count":     len(results),
		"wiki_path": TeamLearningsPagePath,
	})
}
