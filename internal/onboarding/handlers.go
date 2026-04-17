package onboarding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/operations"
)

// RegisterRoutes attaches all onboarding HTTP handlers to mux.
//
// completeFn is called by HandleComplete when the user finishes onboarding.
// Pass nil to defer wiring — the broker should supply a real implementation
// that seeds the team, posts the first message, and triggers the CEO turn.
//
// packSlug is a legacy selection identifier. HandleTemplates uses it to
// return operation-appropriate first-task suggestions and falls back to the
// generic compatibility templates when no blueprint-specific set exists.
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
func RegisterRoutes(mux *http.ServeMux, completeFn func(task string, skipTask bool) error, packSlug string) {
	mux.HandleFunc("/onboarding/state", HandleState)
	mux.HandleFunc("/onboarding/progress", HandleProgress)
	mux.HandleFunc("/onboarding/complete", makeHandleComplete(completeFn))
	mux.HandleFunc("/onboarding/prereqs", HandlePrereqs)
	mux.HandleFunc("/onboarding/validate-key", HandleValidateKey)
	mux.HandleFunc("/onboarding/templates", makeHandleTemplates(packSlug))
	mux.HandleFunc("/onboarding/blueprints", HandleBlueprints)
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
		"step":                onboardingStateStep(s),
		"completed_steps":     s.CompletedSteps,
		"checklist_dismissed": s.ChecklistDismissed,
		"partial":             s.Partial,
		"checklist":           s.Checklist,
		"onboarded":           s.Onboarded(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// HandleProgress handles POST /onboarding/progress.
// Body: {"step": string, "answers": map}.
// Merges the answers for the given step into the partial-progress record.
func HandleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	step := strings.TrimSpace(anyString(body["step"]))
	if step == "" {
		http.Error(w, "step required", http.StatusBadRequest)
		return
	}
	answers := anyMap(body["answers"])
	if len(answers) == 0 {
		answers = legacyProgressAnswers(body)
	}
	if err := SaveProgress(step, answers); err != nil {
		http.Error(w, "failed to save progress", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
	companyName := onboardingPartialCompanyName(s.Partial)
	completeState(s, companyName)

	if err := Save(s); err != nil {
		http.Error(w, "failed to save state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"redirect": "/",
	})
}

func onboardingStateStep(s *State) string {
	if s == nil || s.Partial == nil {
		return ""
	}
	return strings.TrimSpace(s.Partial.Step)
}

func anyString(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func anyMap(value interface{}) map[string]interface{} {
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	return m
}

func legacyProgressAnswers(body map[string]interface{}) map[string]interface{} {
	answers := make(map[string]interface{})
	for key, value := range body {
		switch key {
		case "step", "answers":
			continue
		default:
			answers[key] = value
		}
	}
	return answers
}

func onboardingPartialCompanyName(partial *PartialProgress) string {
	if partial == nil || partial.Answers == nil {
		return ""
	}
	// "identity" is the current wizard step name; "welcome" and "setup"
	// remain for back-compat with sessions saved before the wizard restructure.
	for _, step := range []string{"identity", "welcome", "setup"} {
		answers := partial.Answers[step]
		for _, key := range []string{"company_name", "company"} {
			if value, ok := answers[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
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
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
	_ = json.NewEncoder(w).Encode(results)
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
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

// makeHandleTemplates returns a handler for GET /onboarding/templates that
// closes over the active selection so the first-task suggestions match the
// operation the user is actually launching.
func makeHandleTemplates(packSlug string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		HandleTemplates(w, r, packSlug)
	}
}

// HandleTemplates handles GET /onboarding/templates.
// Returns JSON array of TaskTemplate for the given selection. An empty
// selection falls back to the generic compatibility templates.
func HandleTemplates(w http.ResponseWriter, r *http.Request, packSlug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TemplatesForSelection("", packSlug))
}

// blueprintSummary is the wizard-facing shape returned by HandleBlueprints.
// Keep the field names in sync with BlueprintTemplate in
// web/src/components/onboarding/Wizard.tsx.
type blueprintSummary struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	Emoji       string                  `json:"emoji,omitempty"`
	Agents      []blueprintAgentSummary `json:"agents,omitempty"`
	Tasks       []blueprintTaskSummary  `json:"tasks,omitempty"`
}

type blueprintAgentSummary struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Role    string `json:"role,omitempty"`
	Emoji   string `json:"emoji,omitempty"`
	Checked bool   `json:"checked"`
}

type blueprintTaskSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

// HandleBlueprints handles GET /onboarding/blueprints.
// Returns {"templates": [...]} in the shape the Wizard expects for its
// blueprint picker. An empty list is a valid response (fresh clone without
// templates/operations/ gets the "From scratch" card only).
func HandleBlueprints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	repoRoot := resolveTemplatesRepoRoot("")
	summaries := []blueprintSummary{}
	if repoRoot != "" {
		blueprints, err := operations.ListBlueprints(repoRoot)
		if err == nil {
			for _, bp := range blueprints {
				summaries = append(summaries, summarizeBlueprint(bp))
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"templates": summaries})
}

func summarizeBlueprint(bp operations.Blueprint) blueprintSummary {
	s := blueprintSummary{
		ID:          bp.ID,
		Name:        bp.Name,
		Description: bp.Description,
	}
	for _, a := range bp.Starter.Agents {
		s.Agents = append(s.Agents, blueprintAgentSummary{
			Slug:    a.Slug,
			Name:    a.Name,
			Role:    a.Role,
			Emoji:   a.Emoji,
			Checked: a.Checked,
		})
	}
	for _, t := range bp.Starter.Tasks {
		title := strings.TrimSpace(t.Title)
		if title == "" {
			continue
		}
		s.Tasks = append(s.Tasks, blueprintTaskSummary{
			ID:          onboardingTemplateID(title),
			Name:        title,
			Description: strings.TrimSpace(t.Details),
		})
	}
	return s
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
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
