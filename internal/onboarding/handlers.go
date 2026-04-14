package onboarding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// RegisterRoutes attaches all onboarding HTTP handlers to mux.
//
// completeFn is called by HandleComplete when the user finishes onboarding.
// Pass nil to defer wiring — the broker should supply a real implementation
// that seeds the team, posts the first message, and triggers the CEO turn.
//
// Routes registered:
//
//	GET  /onboarding/state
//	POST /onboarding/progress
//	POST /onboarding/complete
//	GET  /onboarding/prereqs
//	POST /onboarding/validate-key
//	GET  /onboarding/templates
//	POST /onboarding/checklist/{id}/done
//	POST /onboarding/checklist/dismiss
func RegisterRoutes(mux *http.ServeMux, completeFn func(task string, skipTask bool) error) {
	mux.HandleFunc("/onboarding/state", HandleState)
	mux.HandleFunc("/onboarding/progress", HandleProgress)
	mux.HandleFunc("/onboarding/complete", makeHandleComplete(completeFn))
	mux.HandleFunc("/onboarding/prereqs", HandlePrereqs)
	mux.HandleFunc("/onboarding/validate-key", HandleValidateKey)
	mux.HandleFunc("/onboarding/templates", HandleTemplates)
	mux.HandleFunc("/onboarding/checklist/dismiss", HandleChecklistDismiss)
	// Pattern must be registered after the more-specific /dismiss route so
	// that /dismiss is not swallowed by the /{id}/done prefix match.
	mux.HandleFunc("/onboarding/checklist/", HandleChecklistDone)
}

// HandleState handles GET /onboarding/state.
// Returns the full onboarding State plus an "onboarded" convenience boolean.
// The frontend wizard reads state.onboarded to decide whether to show itself
// on page load. Without this boolean, a completed user who refreshes the
// page sees the wizard again because the frontend has no simple flag to
// check.
func HandleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s, err := Load()
	if err != nil {
		http.Error(w, "failed to load state", http.StatusInternalServerError)
		return
	}
	payload := map[string]any{
		"version":             s.Version,
		"completed_at":        s.CompletedAt,
		"company_name":        s.CompanyName,
		"completed_steps":     s.CompletedSteps,
		"checklist_dismissed": s.ChecklistDismissed,
		"partial":             s.Partial,
		"checklist":           s.Checklist,
		"onboarded":           s.Onboarded(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

// HandleProgress handles POST /onboarding/progress.
// Body: {"step": string, "answers": map}.
// Merges the answers for the given step into the partial-progress record.
func HandleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Step    string                 `json:"step"`
		Answers map[string]interface{} `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Step == "" {
		http.Error(w, "step required", http.StatusBadRequest)
		return
	}
	if err := SaveProgress(body.Step, body.Answers); err != nil {
		http.Error(w, "failed to save progress", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// makeHandleComplete returns a handler for POST /onboarding/complete that
// closes over completeFn. The broker should supply a non-nil completeFn to
// seed the team and post the first message.
func makeHandleComplete(completeFn func(task string, skipTask bool) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		HandleComplete(w, r, completeFn)
	}
}

// HandleComplete handles POST /onboarding/complete.
// Body: {"task": string, "skip_task": bool}.
//
// Logic:
//  1. Load state; if already completed return 200 {"already_completed": true, "redirect": "/"}.
//  2. If skip_task is false and task is empty, return 400.
//  3. Call completeFn (when non-nil) — the broker wires side-effects here.
//  4. Mark state as complete and persist it.
//  5. Return 200 {"ok": true, "redirect": "/"}.
//
// TODO: broker wires CompleteFunc here
func HandleComplete(w http.ResponseWriter, r *http.Request, completeFn func(task string, skipTask bool) error) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Task     string `json:"task"`
		SkipTask bool   `json:"skip_task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	s, err := Load()
	if err != nil {
		http.Error(w, "failed to load state", http.StatusInternalServerError)
		return
	}

	// Idempotent: already done.
	if s.Onboarded() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"already_completed": true,
			"redirect":          "/",
		})
		return
	}

	// Validate: task is required unless skip_task=true.
	if !body.SkipTask && strings.TrimSpace(body.Task) == "" {
		http.Error(w, "task required", http.StatusBadRequest)
		return
	}

	// TODO: broker wires CompleteFunc here
	if completeFn != nil {
		if err := completeFn(body.Task, body.SkipTask); err != nil {
			http.Error(w, "complete failed", http.StatusInternalServerError)
			return
		}
	}

	// Build the completed payload — prepare the response before writing disk.
	companyName := ""
	if s.Partial != nil {
		if welcome, ok := s.Partial.Answers["welcome"]; ok {
			if cn, ok := welcome["company_name"].(string); ok {
				companyName = cn
			}
		}
	}
	completeState(s, companyName)

	if err := Save(s); err != nil {
		http.Error(w, "failed to save state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"redirect": "/",
	})
}

// validateProviderKey pings the provider API with a minimal request to verify
// the key. Returns "valid", "invalid", "unreachable", or "format_error".
func validateProviderKey(provider, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "format_error"
	}
	switch provider {
	case "anthropic":
		if !strings.HasPrefix(key, "sk-ant-") || len(key) < 20 {
			return "format_error"
		}
		return pingAnthropic(key)
	case "openai":
		if !strings.HasPrefix(key, "sk-") || len(key) < 20 {
			return "format_error"
		}
		return pingOpenAI(key)
	case "gemini":
		if len(key) < 10 {
			return "format_error"
		}
		// Gemini format varies; accept if non-empty and reasonable length.
		return "valid"
	default:
		return "format_error"
	}
}

func pingAnthropic(key string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	body := strings.NewReader(`{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", body)
	if err != nil {
		return "unreachable"
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "unreachable"
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusBadRequest: // 400 means auth passed, model may complain
		return "valid"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "invalid"
	default:
		return fmt.Sprintf("unreachable:%d", resp.StatusCode)
	}
}

func pingOpenAI(key string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/models", nil)
	if err != nil {
		return "unreachable"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return "unreachable"
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return "valid"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "invalid"
	default:
		return "unreachable"
	}
}

// HandleChecklistDone handles POST /onboarding/checklist/{id}/done.
// Parses the item ID from the URL path and marks it done.
func HandleChecklistDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /onboarding/checklist/{id}/done
	// Strip prefix and suffix to extract id.
	path := strings.TrimPrefix(r.URL.Path, "/onboarding/checklist/")
	path = strings.TrimSuffix(path, "/done")
	id := strings.TrimSpace(path)
	if id == "" || id == "dismiss" {
		http.Error(w, "item id required", http.StatusBadRequest)
		return
	}
	if err := MarkChecklistItem(id, true); err != nil {
		http.Error(w, "failed to update checklist", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandlePrereqs handles GET /onboarding/prereqs.
// Returns JSON array of PrereqResult for node, git, and claude CLI.
func HandlePrereqs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	results := CheckAll()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// HandleValidateKey handles POST /onboarding/validate-key.
// Body: {"provider": string, "key": string}.
// Returns {"status": "valid"|"invalid"|"unreachable"|"format_error"}.
func HandleValidateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	status := validateProviderKey(body.Provider, body.Key)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

// HandleTemplates handles GET /onboarding/templates.
// Returns JSON array of TaskTemplate for the default starter pack.
func HandleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DefaultTemplates())
}

// HandleChecklistDismiss handles POST /onboarding/checklist/dismiss.
// Sets ChecklistDismissed=true so the UI stops showing the checklist.
func HandleChecklistDismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := DismissChecklist(); err != nil {
		http.Error(w, "failed to dismiss checklist", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
