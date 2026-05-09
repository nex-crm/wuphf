package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
)

// CompleteFunc is the side-effect hook invoked by HandleComplete when the
// user finishes onboarding. The broker supplies a real implementation that
// seeds the team from the picked blueprint (or synthesizes one when blueprintID
// is empty), honors the selectedAgents filter from the wizard, and posts the
// kickoff task. blueprintID is empty for the "from scratch" path.
// selectedAgents is nil when no filtering is requested (internal synthesis
// callers) and may be an empty slice when the wizard user unchecked every
// agent. companyName is the company or project name captured in the identity
// step; it is empty when the user skipped or left the field blank.
type CompleteFunc func(task string, skipTask bool, blueprintID string, selectedAgents []string, companyName string) error

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
// wikiRoot is the absolute path to the wiki directory used by the scan
// handler to write extracted company context articles.
//
// authMiddleware wraps each handler. Pass the broker's requireAuth so local
// processes and cross-origin callers cannot POST /onboarding/complete (which
// seeds the team and fires the first CEO turn) without the broker token.
// Pass a nil middleware only in tests — RegisterRoutes substitutes a passthrough.
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
//	POST /onboarding/scan
//	POST /onboarding/upload-context
func RegisterRoutes(mux *http.ServeMux, completeFn CompleteFunc, packSlug string, authMiddleware func(http.HandlerFunc) http.HandlerFunc, wikiRoot string) {
	if authMiddleware == nil {
		authMiddleware = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	mux.HandleFunc("/onboarding/state", authMiddleware(HandleState))
	mux.HandleFunc("/onboarding/progress", authMiddleware(HandleProgress))
	mux.HandleFunc("/onboarding/complete", authMiddleware(makeHandleComplete(completeFn)))
	mux.HandleFunc("/onboarding/prereqs", authMiddleware(HandlePrereqs))
	mux.HandleFunc("/onboarding/validate-key", authMiddleware(HandleValidateKey))
	mux.HandleFunc("/onboarding/templates", authMiddleware(makeHandleTemplates(packSlug)))
	mux.HandleFunc("/onboarding/blueprints", authMiddleware(HandleBlueprints))
	mux.HandleFunc("/onboarding/checklist/dismiss", authMiddleware(HandleChecklistDismiss))
	// Pattern must be registered after the more-specific /dismiss route so
	// that /dismiss is not swallowed by the /{id}/done prefix match.
	mux.HandleFunc("/onboarding/checklist/", authMiddleware(HandleChecklistDone))
	mux.HandleFunc("/onboarding/scan", authMiddleware(makeHandleScan(wikiRoot)))
	mux.HandleFunc("/onboarding/upload-context", authMiddleware(handleUploadContext))
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
func makeHandleComplete(completeFn CompleteFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		HandleComplete(w, r, completeFn)
	}
}

// HandleComplete handles POST /onboarding/complete.
// Body: {"task": string, "skip_task": bool, "blueprint": string, "agents": []string}.
// The blueprint and agents fields are forwarded to completeFn so the broker
// can seed the team that the wizard actually picked. A legacy client that
// omits them is treated as "from scratch" (blueprint empty, agents nil).
//
// Logic:
//  1. Load state; if already completed return 200 {"already_completed": true, "redirect": "/"}.
//  2. If skip_task is false and task is empty, return 400.
//  3. Call completeFn (when non-nil) — the broker wires side-effects here.
//     If completeFn returns an error (e.g. LoadBlueprint failed), return 500
//     with the error message so the wizard can surface it.
//  4. Mark state as complete and persist it.
//  5. Return 200 {"ok": true, "redirect": "/"}.
func HandleComplete(w http.ResponseWriter, r *http.Request, completeFn CompleteFunc) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Task          string   `json:"task"`
		SkipTask      bool     `json:"skip_task"`
		Blueprint     string   `json:"blueprint"`
		Agents        []string `json:"agents"`
		Website       string   `json:"website"`
		ScanCompleted bool     `json:"scan_completed"`
		OwnerName     string   `json:"owner_name"`
		OwnerRole     string   `json:"owner_role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	website := strings.TrimSpace(body.Website)
	ownerName := strings.TrimSpace(body.OwnerName)
	ownerRole := strings.TrimSpace(body.OwnerRole)

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

	// Extract company name before calling completeFn so the broker can sync it
	// to the workspace registry as part of its side effects.
	companyName := onboardingPartialCompanyName(s.Partial)

	if completeFn != nil {
		if err := completeFn(body.Task, body.SkipTask, strings.TrimSpace(body.Blueprint), body.Agents, companyName); err != nil {
			// Log the full error server-side but return an opaque response to
			// the client. completeFn may wrap filesystem paths, yaml parse
			// messages, or other internals that should not leak to HTTP
			// callers on a locally-bound broker.
			log.Printf("onboarding: complete failed: %v", err)
			http.Error(w, "complete failed", http.StatusInternalServerError)
			return
		}
	}

	// Persist company/owner fields only after validation and completeFn succeed
	// so duplicate or invalid requests don't overwrite config state.
	if website != "" || ownerName != "" || ownerRole != "" || !body.ScanCompleted {
		if cfg, err := config.Load(); err == nil {
			if website != "" {
				cfg.CompanyWebsite = website
			}
			if ownerName != "" {
				cfg.OwnerName = ownerName
			}
			if ownerRole != "" {
				cfg.OwnerRole = ownerRole
			}
			if website != "" || ownerName != "" || ownerRole != "" {
				cfg.PendingCompanySeed = !body.ScanCompleted
			}
			if err := config.Save(cfg); err != nil {
				log.Printf("onboarding: complete: failed to persist company fields: %v", err)
			}
		}
	}

	// Build the completed payload — prepare the response before writing disk.
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
func validateProviderKey(ctx context.Context, provider, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "format_error"
	}
	switch provider {
	case "anthropic":
		if !strings.HasPrefix(key, "sk-ant-") || len(key) < 20 {
			return "format_error"
		}
		return pingAnthropic(ctx, key)
	case "openai":
		if !strings.HasPrefix(key, "sk-") || len(key) < 20 {
			return "format_error"
		}
		return pingOpenAI(ctx, key)
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

func pingAnthropic(ctx context.Context, key string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	body := strings.NewReader(`{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", body)
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

func pingOpenAI(ctx context.Context, key string) string {
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/v1/models", nil)
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
	status := validateProviderKey(r.Context(), body.Provider, body.Key)
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
// web/src/components/onboarding/wizard/types.ts. The pack-library
// fields (outcome, category, channels, skills, wiki_scaffold,
// first_tasks, requirements, estimated_setup_minutes,
// example_artifacts) are additive — older clients that only consume
// id/name/description/agents/tasks keep working.
type blueprintSummary struct {
	ID                    string                       `json:"id"`
	Name                  string                       `json:"name"`
	Description           string                       `json:"description,omitempty"`
	Emoji                 string                       `json:"emoji,omitempty"`
	Outcome               string                       `json:"outcome,omitempty"`
	Category              string                       `json:"category,omitempty"`
	EstimatedSetupMinutes int                          `json:"estimated_setup_minutes,omitempty"`
	Agents                []blueprintAgentSummary      `json:"agents,omitempty"`
	Channels              []blueprintChannelSummary    `json:"channels,omitempty"`
	Tasks                 []blueprintTaskSummary       `json:"tasks,omitempty"`
	Skills                []blueprintSkillSummary      `json:"skills,omitempty"`
	WikiScaffold          []blueprintWikiScaffoldEntry `json:"wiki_scaffold,omitempty"`
	FirstTasks            []blueprintFirstTaskSummary  `json:"first_tasks,omitempty"`
	Requirements          []blueprintRequirementEntry  `json:"requirements,omitempty"`
	ExampleArtifacts      []blueprintExampleSummary    `json:"example_artifacts,omitempty"`
}

type blueprintChannelSummary struct {
	Slug    string `json:"slug"`
	Name    string `json:"name,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}

type blueprintSkillSummary struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose,omitempty"`
}

type blueprintWikiScaffoldEntry struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
}

type blueprintFirstTaskSummary struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Prompt         string `json:"prompt,omitempty"`
	ExpectedOutput string `json:"expected_output,omitempty"`
}

type blueprintRequirementEntry struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type blueprintExampleSummary struct {
	Kind  string `json:"kind,omitempty"`
	Title string `json:"title"`
}

type blueprintAgentSummary struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Role    string `json:"role,omitempty"`
	Emoji   string `json:"emoji,omitempty"`
	Checked bool   `json:"checked"`
	// BuiltIn marks the blueprint's lead agent (type: lead or built_in:
	// true in the yaml). The wizard uses this to prevent the user from
	// unchecking the lead in the Team step — downstream broker guards
	// also refuse to disable or remove a BuiltIn member.
	BuiltIn bool `json:"built_in,omitempty"`
}

type blueprintTaskSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

// HandleBlueprints handles GET /onboarding/blueprints.
// Returns {"templates": [...]} in the shape the Wizard expects for its
// blueprint picker. Passes "" to ListBlueprints when the filesystem walk
// finds no repo — the loader falls back to the binary's embedded
// templates (wired in the root wuphf package's init), so installs without
// a checkout still see the shipped blueprints.
func HandleBlueprints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	summaries := []blueprintSummary{}
	blueprints, err := operations.ListBlueprints(resolveTemplatesRepoRoot(""))
	if err == nil {
		for _, bp := range blueprints {
			summaries = append(summaries, summarizeBlueprint(bp))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"templates": summaries})
}

func summarizeBlueprint(bp operations.Blueprint) blueprintSummary {
	s := blueprintSummary{
		ID:                    bp.ID,
		Name:                  bp.Name,
		Description:           bp.Description,
		Outcome:               bp.Outcome,
		Category:              bp.Category,
		EstimatedSetupMinutes: bp.EstimatedSetupMinutes,
	}
	leadSlug := strings.TrimSpace(bp.Starter.LeadSlug)
	for _, a := range bp.Starter.Agents {
		// Mark the lead as BuiltIn so the wizard's Team step can disable
		// its checkbox. We trust three signals from the blueprint yaml:
		// explicit built_in, type=lead, or slug matching starter.lead_slug.
		builtIn := a.BuiltIn || strings.EqualFold(strings.TrimSpace(a.Type), "lead") || (leadSlug != "" && a.Slug == leadSlug)
		s.Agents = append(s.Agents, blueprintAgentSummary{
			Slug:    a.Slug,
			Name:    a.Name,
			Role:    a.Role,
			Emoji:   a.Emoji,
			Checked: a.Checked,
			BuiltIn: builtIn,
		})
	}
	for _, c := range bp.Starter.Channels {
		slug := strings.TrimSpace(c.Slug)
		if slug == "" || strings.Contains(slug, "{{") {
			continue
		}
		s.Channels = append(s.Channels, blueprintChannelSummary{
			Slug:    slug,
			Name:    strings.TrimSpace(c.Name),
			Purpose: strings.TrimSpace(c.Description),
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
	for _, ft := range bp.FirstTasks {
		id := strings.TrimSpace(ft.ID)
		title := strings.TrimSpace(ft.Title)
		if id == "" || title == "" {
			continue
		}
		s.FirstTasks = append(s.FirstTasks, blueprintFirstTaskSummary{
			ID:             id,
			Title:          title,
			Prompt:         strings.TrimSpace(ft.Prompt),
			ExpectedOutput: strings.TrimSpace(ft.ExpectedOutput),
		})
	}
	for _, sk := range bp.Skills {
		name := strings.TrimSpace(sk.Name)
		if name == "" {
			continue
		}
		s.Skills = append(s.Skills, blueprintSkillSummary{
			Name:    name,
			Purpose: strings.TrimSpace(sk.Purpose),
		})
	}
	for _, req := range bp.Requirements {
		name := strings.TrimSpace(req.Name)
		if name == "" {
			continue
		}
		s.Requirements = append(s.Requirements, blueprintRequirementEntry{
			Kind:     strings.TrimSpace(req.Kind),
			Name:     name,
			Required: req.Required,
			Detail:   strings.TrimSpace(req.Detail),
		})
	}
	if bp.WikiSchema != nil {
		for _, item := range bp.WikiSchema.Bootstrap {
			path := strings.TrimSpace(item.Path)
			if path == "" {
				continue
			}
			s.WikiScaffold = append(s.WikiScaffold, blueprintWikiScaffoldEntry{
				Path:  path,
				Title: strings.TrimSpace(item.Title),
			})
		}
	}
	for _, ex := range bp.ExampleArtifacts {
		title := strings.TrimSpace(ex.Title)
		if title == "" {
			continue
		}
		s.ExampleArtifacts = append(s.ExampleArtifacts, blueprintExampleSummary{
			Kind:  strings.TrimSpace(ex.Kind),
			Title: title,
		})
	}
	return s
}

// OSScanRequest is the request body for POST /onboarding/scan.
type OSScanRequest struct {
	WebsiteURL string   `json:"website_url"`
	FilePaths  []string `json:"file_paths"`
	OwnerName  string   `json:"owner_name"`
	OwnerRole  string   `json:"owner_role"`
}

// OSScanResponse is the response body for POST /onboarding/scan.
type OSScanResponse struct {
	Facts           []string `json:"facts"`
	ArticlesWritten []string `json:"articles_written"`
	Warnings        []string `json:"warnings,omitempty"`
}

// makeHandleScan returns a handler for POST /onboarding/scan that runs the
// company context seeding pipeline and writes wiki articles.
func makeHandleScan(wikiRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req OSScanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		// Security: file path prefix guard — only files staged under the
		// wuphf-upload temp prefix are permitted. EvalSymlinks resolves any
		// symlinks so a link inside the upload dir cannot escape to an
		// arbitrary path. Resolve os.TempDir() itself so the prefix matches
		// on platforms (e.g. macOS) where /tmp is a symlink to /private/tmp.
		resolvedTmpDir, tmpErr := filepath.EvalSymlinks(os.TempDir())
		if tmpErr != nil {
			resolvedTmpDir = os.TempDir()
		}
		uploadPrefix := filepath.Join(resolvedTmpDir, "wuphf-upload-")
		for _, p := range req.FilePaths {
			resolved, symlinkErr := filepath.EvalSymlinks(p)
			if symlinkErr != nil {
				http.Error(w, "invalid file path", http.StatusBadRequest)
				return
			}
			if !strings.HasPrefix(resolved, uploadPrefix) {
				http.Error(w, "invalid file path", http.StatusBadRequest)
				return
			}
		}
		// Clean up uploaded temp dirs after the scan regardless of outcome.
		defer func() {
			seen := make(map[string]struct{})
			for _, p := range req.FilePaths {
				dir := filepath.Dir(p)
				if _, ok := seen[dir]; !ok {
					seen[dir] = struct{}{}
					_ = os.RemoveAll(dir)
				}
			}
		}()
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		input := operations.CompanySeedInput{
			WebsiteURL: req.WebsiteURL,
			FilePaths:  req.FilePaths,
			OwnerName:  req.OwnerName,
			OwnerRole:  req.OwnerRole,
			Completer:  cliCompleter{},
			WikiRoot:   wikiRoot,
		}
		result, err := operations.SeedCompanyContext(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				log.Printf("onboarding: scan timeout for %q", req.WebsiteURL)
				http.Error(w, "scan timeout", http.StatusRequestTimeout)
				return
			}
			log.Printf("onboarding: scan failed: %v", err)
			http.Error(w, "scan failed", http.StatusInternalServerError)
			return
		}
		resp := OSScanResponse{
			Facts:           result.Facts,
			ArticlesWritten: result.ArticlesWritten,
			Warnings:        result.Warnings,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// handleUploadContext handles POST /onboarding/upload-context.
// Accepts a multipart form with a "files" field and saves each uploaded file
// to a temp directory so /onboarding/scan can reference it by path.
func handleUploadContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "parse form failed", http.StatusBadRequest)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "no files uploaded", http.StatusBadRequest)
		return
	}
	const maxFileCount = 20
	if len(files) > maxFileCount {
		http.Error(w, "too many files", http.StatusBadRequest)
		return
	}
	var paths []string
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			continue
		}
		// Create temp dir with wuphf-upload- prefix so the scan handler's
		// path prefix guard accepts it.
		dir, err := os.MkdirTemp("", "wuphf-upload-")
		if err != nil {
			f.Close()
			continue
		}
		dst := filepath.Join(dir, filepath.Base(fh.Filename))
		out, err := os.Create(dst)
		if err != nil {
			f.Close()
			_ = os.RemoveAll(dir)
			continue
		}
		if _, err := io.Copy(out, f); err != nil {
			_ = out.Close()
			f.Close()
			_ = os.RemoveAll(dir)
			continue
		}
		_ = out.Close()
		f.Close()
		paths = append(paths, dst)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string][]string{"paths": paths})
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
