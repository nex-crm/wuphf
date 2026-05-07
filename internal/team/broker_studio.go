package team

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/operations"
	"github.com/nex-crm/wuphf/internal/provider"
)

// externalRetryAfterPattern parses RFC3339 timestamps embedded in
// workflow-provider error strings of the form "retry after <ts>".
// Workflow runners emit this format when an external action provider
// (One, Composio, etc.) returns a 429 with a structured deadline; the
// broker uses it to decide whether to surface a 429 with Retry-After
// header back to the client. (?i) makes the prefix case-insensitive.
var externalRetryAfterPattern = regexp.MustCompile(`(?i)retry after ([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.+-]+Z?)`)

// stripExternalRetryMarker removes rate-limit marker lines from a task Details
// string so a stale "Retry after <ts>" timestamp cannot re-trigger the
// watchdog resume loop on the next scheduler tick.
//
// The predicate is externalRetryAfterPattern — the same regex the watchdog uses
// in externalWorkflowRetryAfter to detect a re-resume condition. Reusing that
// pattern keeps a single source of truth for marker grammar: if the watchdog
// would not re-resume from a given line, we do not strip it. This narrows the
// strip to lines that carry a parseable RFC3339 retry-after timestamp and
// avoids over-stripping unrelated content (e.g. ticket refs like "SUP-4290",
// version strings like "v4.29", line counts like "completed 4290 items").
//
// Known limitation: when an external provider packs both the rate-limit
// timestamp and an operator note onto a single line (e.g. "429 ... Retry
// after <ts>. Owner: refresh OAuth"), the whole line is dropped together. A
// future refinement could substitute just the marker substring; the loop fix
// does not require that today.
func stripExternalRetryMarker(details string) string {
	if details == "" {
		return details
	}
	lines := strings.Split(details, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if externalRetryAfterPattern.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// externalWorkflowRetryAfter extracts a retry-at timestamp from an
// external workflow provider's error message. Returns (zero, false)
// when err is nil, when the pattern doesn't match, or when the
// matched timestamp is unparseable. Past timestamps clamp to now so
// callers get a positive retry duration.
func externalWorkflowRetryAfter(err error, now time.Time) (time.Time, bool) {
	if err == nil {
		return time.Time{}, false
	}
	matches := externalRetryAfterPattern.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return time.Time{}, false
	}
	retryAt, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(matches[1]))
	if parseErr != nil {
		return time.Time{}, false
	}
	if retryAt.Before(now) {
		return now, true
	}
	return retryAt, true
}

// studioPackageGeneratorFn is the test seam type swapped via
// setStudioPackageGeneratorForTest.
type studioPackageGeneratorFn func(systemPrompt, prompt, cwd string) (string, error)

// studioPackageGenerator routes Studio package generation through the
// install-wide LLM provider so opencode-only or claude-code-only setups
// aren't forced to have `codex` installed.
//
// Lives behind atomic.Pointer because broker handlers can call it from
// goroutines that outlive a test's t.Cleanup (Studio generation is
// fire-and-forget on the broker's HTTP path).
var studioPackageGeneratorOverride atomic.Pointer[studioPackageGeneratorFn]

func studioPackageGenerator(systemPrompt, prompt, cwd string) (string, error) {
	if p := studioPackageGeneratorOverride.Load(); p != nil {
		return (*p)(systemPrompt, prompt, cwd)
	}
	return provider.RunConfiguredOneShot(systemPrompt, prompt, cwd)
}

type studioGeneratedPackage map[string]map[string]any

type studioGeneratedArtifact struct {
	Kind  string         `json:"kind"`
	Title string         `json:"title,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

type studioStubExecution struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	WorkflowKey  string         `json:"workflow_key"`
	Status       string         `json:"status"`
	Mode         string         `json:"mode"`
	Integrations []string       `json:"integrations,omitempty"`
	Summary      string         `json:"summary"`
	Input        map[string]any `json:"input,omitempty"`
	Output       map[string]any `json:"output,omitempty"`
}

func decodeStudioGeneratedPackage(raw string, requiredArtifacts []string) (studioGeneratedPackage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty codex response")
	}
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
			lines = lines[1:]
			if last := len(lines) - 1; last >= 0 && strings.HasPrefix(strings.TrimSpace(lines[last]), "```") {
				lines = lines[:last]
			}
			trimmed = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		trimmed = trimmed[start : end+1]
	}
	var pkg studioGeneratedPackage
	if err := json.Unmarshal([]byte(trimmed), &pkg); err != nil {
		return nil, err
	}
	for _, artifactID := range requiredArtifacts {
		if len(pkg[artifactID]) == 0 {
			return nil, fmt.Errorf("missing required artifact %q", artifactID)
		}
	}
	return pkg, nil
}

func buildStudioFollowUpStubExecutions(runTitle string, offers []any, pkg studioGeneratedPackage) []studioStubExecution {
	offerNames := extractStudioOfferNames(offers)
	artifactIDs := studioPackageArtifactIDs(pkg)
	primaryArtifactID, primaryArtifact := studioPrimaryPackageArtifact(pkg, artifactIDs)
	primarySummary := firstStudioString(
		primaryArtifact["summary"],
		primaryArtifact["objective"],
		primaryArtifact["title"],
		primaryArtifact["name"],
		runTitle,
	)
	return []studioStubExecution{
		{
			ID:           fmt.Sprintf("followup-review-%d", time.Now().UTC().UnixNano()),
			Provider:     "one",
			WorkflowKey:  "artifact-review-sync",
			Status:       "success",
			Mode:         "dry_run",
			Integrations: []string{"artifact-review"},
			Summary:      fmt.Sprintf("Prepared a review sync payload for %s.", runTitle),
			Input: map[string]any{
				"run_title":        runTitle,
				"artifact_ids":     artifactIDs,
				"primary_artifact": primaryArtifactID,
			},
			Output: map[string]any{
				"destination":  "review queue",
				"draft_status": "ready_for_review",
			},
		},
		{
			ID:           fmt.Sprintf("followup-offers-%d", time.Now().UTC().UnixNano()+1),
			Provider:     "one",
			WorkflowKey:  "offer-alignment-check",
			Status:       "success",
			Mode:         "dry_run",
			Integrations: []string{"offer-alignment"},
			Summary:      fmt.Sprintf("Prepared offer alignment notes for %s.", runTitle),
			Input: map[string]any{
				"run_title":    runTitle,
				"offer_names":  offerNames,
				"artifact_ids": artifactIDs,
			},
			Output: map[string]any{
				"destination":  "offer queue",
				"draft_status": "ready_for_review",
			},
		},
		{
			ID:           fmt.Sprintf("followup-approval-%d", time.Now().UTC().UnixNano()+2),
			Provider:     "one",
			WorkflowKey:  "approval-gate-review",
			Status:       "success",
			Mode:         "dry_run",
			Integrations: []string{"approval-gates"},
			Summary:      fmt.Sprintf("Prepared approval gates for %s.", runTitle),
			Input: map[string]any{
				"run_title":        runTitle,
				"primary_artifact": primaryArtifactID,
				"primary_summary":  primarySummary,
			},
			Output: map[string]any{
				"destination":  "approval queue",
				"draft_status": "ready_for_review",
			},
		},
	}
}

func studioDefaultArtifactDefinitions() []operations.ArtifactType {
	return []operations.ArtifactType{
		{ID: "objective_brief", Name: "Objective brief", Description: "Problem statement, constraints, and desired outcome for one run."},
		{ID: "execution_packet", Name: "Execution packet", Description: "Checklist, dependencies, outputs, and handoff details for one run."},
		{ID: "approval_checklist", Name: "Approval checklist", Description: "Review gates and required human approvals before live action."},
	}
}

func studioNormalizeArtifactDefinitions(defs []operations.ArtifactType) []operations.ArtifactType {
	normalized := make([]operations.ArtifactType, 0, len(defs))
	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		def.ID = strings.TrimSpace(def.ID)
		if def.ID == "" {
			continue
		}
		if _, ok := seen[def.ID]; ok {
			continue
		}
		seen[def.ID] = struct{}{}
		normalized = append(normalized, def)
	}
	if len(normalized) == 0 {
		return studioDefaultArtifactDefinitions()
	}
	return normalized
}

func studioArtifactIDs(defs []operations.ArtifactType) []string {
	ids := make([]string, 0, len(defs))
	for _, def := range defs {
		ids = append(ids, def.ID)
	}
	return ids
}

func buildStudioGeneratedArtifacts(runTitle string, pkg studioGeneratedPackage, defs []operations.ArtifactType) []studioGeneratedArtifact {
	artifacts := make([]studioGeneratedArtifact, 0, len(defs))
	for _, def := range studioNormalizeArtifactDefinitions(defs) {
		artifacts = append(artifacts, studioGeneratedArtifact{
			Kind:  def.ID,
			Title: runTitle,
			Data:  pkg[def.ID],
		})
	}
	return artifacts
}

func extractStudioOfferNames(offers []any) []string {
	names := make([]string, 0, len(offers))
	for _, item := range offers {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// Skip when "name" is missing or null — fmt.Sprintf("%v", nil)
		// returns the literal string "<nil>" which would otherwise leak
		// into the generated follow-up payload.
		raw, present := record["name"]
		if !present || raw == nil {
			continue
		}
		name := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func extractStudioStringSlice(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprintf("%v", item))
		if text == "" || text == "<nil>" {
			continue
		}
		values = append(values, text)
	}
	return values
}

func firstStudioString(values ...any) string {
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

// studioRequestMaxBodyBytes caps Studio HTTP request bodies. 1 MiB
// comfortably fits a workspace + run + offer payload; anything larger
// is either a bug, a malformed paste from the UI, or a hostile input.
const studioRequestMaxBodyBytes = 1 << 20

func (b *Broker) handleStudioGeneratePackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, studioRequestMaxBodyBytes)
	defer r.Body.Close()

	var body struct {
		Channel   string                    `json:"channel"`
		Actor     string                    `json:"actor"`
		Workspace map[string]any            `json:"workspace"`
		Run       map[string]any            `json:"run"`
		Offers    []any                     `json:"offers"`
		Artifacts []operations.ArtifactType `json:"artifacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = "human"
	}
	runTitle := strings.TrimSpace(fmt.Sprintf("%v", body.Run["title"]))
	if runTitle == "" {
		http.Error(w, "run.title required", http.StatusBadRequest)
		return
	}

	artifactDefs := studioNormalizeArtifactDefinitions(body.Artifacts)
	systemPrompt := strings.TrimSpace(`You generate structured operation artifacts for a reusable workflow.
Return valid JSON only. No markdown fences. No prose outside JSON.
The top-level object must contain exactly:
` + strings.Join(func() []string {
		items := make([]string, 0, len(artifactDefs))
		for _, def := range artifactDefs {
			items = append(items, "- "+def.ID)
		}
		return items
	}(), "\n"))

	promptPayload, _ := json.Marshal(map[string]any{
		"workspace": body.Workspace,
		"run":       body.Run,
		"offers":    body.Offers,
		"artifacts": artifactDefs,
	})
	prompt := strings.TrimSpace(`Turn this run into a production-ready artifact bundle for the active operation.

Rules:
- Keep claims concrete and production-safe.
- Use short, scannable fields.
- For each requested artifact, use the provided id, name, and description to shape the object.
- Prefer compact objects with fields like summary, goals, checklist, dependencies, outputs, risks, approvals, notes, links, or tags when they fit the artifact purpose.
- Only return the requested artifact ids as top-level keys.

Input JSON:
` + string(promptPayload))

	raw, err := studioPackageGenerator(systemPrompt, prompt, "")
	if err != nil {
		http.Error(w, "package generation failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	pkg, err := decodeStudioGeneratedPackage(raw, studioArtifactIDs(artifactDefs))
	if err != nil {
		http.Error(w, "invalid codex package output: "+err.Error(), http.StatusBadGateway)
		return
	}
	stubExecutions := buildStudioFollowUpStubExecutions(runTitle, body.Offers, pkg)
	artifacts := buildStudioGeneratedArtifacts(runTitle, pkg, artifactDefs)
	summary := truncateSummary("Generated operation artifacts for "+runTitle, 140)
	if err := b.RecordAction("studio_package_generated", "studio", channel, actor, summary, runTitle, nil, ""); err != nil {
		http.Error(w, "failed to persist action", http.StatusInternalServerError)
		return
	}
	for _, execution := range stubExecutions {
		if err := b.RecordAction("studio_followup_stub_executed", "studio", channel, actor, truncateSummary(execution.Summary, 140), runTitle, nil, ""); err != nil {
			http.Error(w, "failed to persist follow-up stub action", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":              true,
		"package":         pkg,
		"artifacts":       artifacts,
		"stub_executions": stubExecutions,
	})
}

func studioPackageArtifactIDs(pkg studioGeneratedPackage) []string {
	ids := make([]string, 0, len(pkg))
	for id := range pkg {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func studioPrimaryPackageArtifact(pkg studioGeneratedPackage, artifactIDs []string) (string, map[string]any) {
	for _, id := range artifactIDs {
		if item, ok := pkg[id]; ok && len(item) > 0 {
			return id, item
		}
	}
	for id, item := range pkg {
		if len(item) > 0 {
			return strings.TrimSpace(id), item
		}
	}
	return "", map[string]any{}
}

func normalizeStudioWorkflowDefinition(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err != nil {
			return nil, err
		}
		trimmed = []byte(strings.TrimSpace(encoded))
	}
	if len(trimmed) == 0 {
		return nil, nil
	}
	// After unwrapping, validate the remainder parses as JSON. Without this
	// check `{"workflow_definition":"\"not json\""}` would be accepted here
	// and only fail much later as a 502 from the provider; anything that
	// isn't a JSON object/array shouldn't have made it past the broker.
	var probe any
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return nil, fmt.Errorf("workflow_definition is not valid JSON: %w", err)
	}
	return trimmed, nil
}

func studioWorkflowHints(definition []byte) (dryRun bool, mock bool, integrations []string) {
	var parsed struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(definition, &parsed); err != nil {
		return false, false, nil
	}
	seen := make(map[string]struct{})
	for _, step := range parsed.Steps {
		if v, ok := step["dry_run"].(bool); ok && v {
			dryRun = true
		}
		if v, ok := step["mock"].(bool); ok && v {
			mock = true
		}
		platform := strings.TrimSpace(fmt.Sprintf("%v", step["platform"]))
		if platform == "" || platform == "<nil>" {
			continue
		}
		if _, exists := seen[platform]; exists {
			continue
		}
		seen[platform] = struct{}{}
		integrations = append(integrations, platform)
	}
	return dryRun, mock, integrations
}

func workflowCreateConflict(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "already exists") ||
		strings.Contains(text, "duplicate") ||
		strings.Contains(text, "conflict")
}

func uniqueStrings(values ...[]string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, group := range values {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func workflowRunModeLabel(dryRun, mock bool) string {
	switch {
	case dryRun && mock:
		return "dry-run + mock"
	case dryRun:
		return "dry-run"
	case mock:
		return "mock"
	default:
		return "live"
	}
}

func mustMarshalStudioJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{"error":"marshal_failed"}`)
	}
	return json.RawMessage(data)
}

func executeStudioWorkflowStub(workflowKey string, definition []byte, inputs map[string]any, dryRun, mock bool) (action.WorkflowExecuteResult, error) {
	var parsed struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(definition, &parsed); err != nil {
		return action.WorkflowExecuteResult{}, err
	}
	now := time.Now().UTC()
	runID := fmt.Sprintf("studiowf_%d", now.UnixNano())
	stepLogs := make(map[string]json.RawMessage, len(parsed.Steps))
	events := make([]json.RawMessage, 0, len(parsed.Steps)+2)
	events = append(events, mustMarshalStudioJSON(map[string]any{
		"event":        "workflow_started",
		"provider":     "studio_stub",
		"workflow_key": workflowKey,
		"run_id":       runID,
	}))
	status := "success"
	for i, step := range parsed.Steps {
		stepID := strings.TrimSpace(fmt.Sprintf("%v", step["id"]))
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", i+1)
		}
		stepType := strings.TrimSpace(fmt.Sprintf("%v", step["kind"]))
		if stepType == "" || stepType == "<nil>" {
			stepType = strings.TrimSpace(fmt.Sprintf("%v", step["type"]))
		}
		if stepType == "" || stepType == "<nil>" {
			stepType = "action"
		}
		stepStatus := "completed"
		if dryRun {
			stepStatus = "planned"
		}
		if mock {
			stepStatus = "mocked"
		}
		payload := map[string]any{
			"id":       stepID,
			"type":     stepType,
			"status":   stepStatus,
			"platform": strings.TrimSpace(fmt.Sprintf("%v", step["platform"])),
			"action":   strings.TrimSpace(fmt.Sprintf("%v", step["action"])),
			"inputs":   inputs,
		}
		stepLogs[stepID] = mustMarshalStudioJSON(payload)
		events = append(events, mustMarshalStudioJSON(map[string]any{
			"event":   "workflow_step_completed",
			"step_id": stepID,
			"type":    stepType,
			"status":  stepStatus,
		}))
	}
	events = append(events, mustMarshalStudioJSON(map[string]any{
		"event":  "workflow_finished",
		"run_id": runID,
		"status": status,
	}))
	return action.WorkflowExecuteResult{
		RunID:  runID,
		Status: status,
		Steps:  stepLogs,
		Events: events,
	}, nil
}

func (b *Broker) recordStudioWorkflowExecution(channel, actor, skillName, workflowKey, providerName, title, status string, when time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var skill *teamSkill
	if strings.TrimSpace(skillName) != "" {
		skill = b.findSkillByNameLocked(skillName)
	}
	if skill == nil && strings.TrimSpace(workflowKey) != "" {
		skill = b.findSkillByWorkflowKeyLocked(workflowKey)
	}
	if skill != nil {
		skill.UsageCount++
		skill.LastExecutionStatus = strings.TrimSpace(status)
		skill.LastExecutionAt = when.UTC().Format(time.RFC3339)
		skill.UpdatedAt = when.UTC().Format(time.RFC3339)
		if strings.TrimSpace(title) == "" {
			title = skill.Title
		}
	}
	if strings.TrimSpace(title) == "" {
		title = workflowKey
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   channel,
		Kind:      "skill_invocation",
		Title:     title,
		Content:   fmt.Sprintf("Workflow %q executed via %s (%s)", workflowKey, providerName, status),
		Timestamp: when.UTC().Format(time.RFC3339),
	})
	return b.saveLocked()
}

func (b *Broker) handleStudioRunWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, studioRequestMaxBodyBytes)
	defer r.Body.Close()

	var body struct {
		Channel            string          `json:"channel"`
		Actor              string          `json:"actor"`
		SkillName          string          `json:"skill_name"`
		WorkflowKey        string          `json:"workflow_key"`
		WorkflowProvider   string          `json:"workflow_provider"`
		WorkflowDefinition json.RawMessage `json:"workflow_definition"`
		Inputs             map[string]any  `json:"inputs"`
		DryRun             *bool           `json:"dry_run"`
		Mock               *bool           `json:"mock"`
		AllowBash          bool            `json:"allow_bash"`
		Integrations       []string        `json:"integrations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = "human"
	}

	var (
		skillName          = strings.TrimSpace(body.SkillName)
		workflowKey        = strings.TrimSpace(body.WorkflowKey)
		workflowProvider   = strings.TrimSpace(body.WorkflowProvider)
		workflowDefinition []byte
		title              string
	)
	definition, err := normalizeStudioWorkflowDefinition(body.WorkflowDefinition)
	if err != nil {
		http.Error(w, "invalid workflow_definition: "+err.Error(), http.StatusBadRequest)
		return
	}
	workflowDefinition = definition

	b.mu.Lock()
	if skillName != "" || workflowKey != "" {
		var skill *teamSkill
		if skillName != "" {
			skill = b.findSkillByNameLocked(skillName)
		}
		if skill == nil && workflowKey != "" {
			skill = b.findSkillByWorkflowKeyLocked(workflowKey)
		}
		if skill != nil {
			if skillName == "" {
				skillName = strings.TrimSpace(skill.Name)
			}
			if workflowKey == "" {
				workflowKey = strings.TrimSpace(skill.WorkflowKey)
			}
			if workflowProvider == "" {
				workflowProvider = strings.TrimSpace(skill.WorkflowProvider)
			}
			if len(workflowDefinition) == 0 {
				workflowDefinition = []byte(strings.TrimSpace(skill.WorkflowDefinition))
			}
			title = strings.TrimSpace(skill.Title)
		}
	}
	b.mu.Unlock()

	if workflowKey == "" {
		http.Error(w, "workflow_key required", http.StatusBadRequest)
		return
	}
	if workflowProvider == "" {
		workflowProvider = "one"
	}
	if len(workflowDefinition) == 0 {
		http.Error(w, "workflow_definition required", http.StatusBadRequest)
		return
	}

	inferredDryRun, inferredMock, inferredIntegrations := studioWorkflowHints(workflowDefinition)
	dryRun := inferredDryRun
	if body.DryRun != nil {
		dryRun = *body.DryRun
	}
	mock := inferredMock
	if body.Mock != nil {
		mock = *body.Mock
	}
	integrations := uniqueStrings(body.Integrations, inferredIntegrations)

	providerLabel := workflowProvider
	registry := action.NewRegistryFromEnv()
	prov, err := registry.ProviderNamed(workflowProvider, action.CapabilityWorkflowExecute)
	var execution action.WorkflowExecuteResult
	if err != nil {
		if dryRun || mock {
			execution, err = executeStudioWorkflowStub(workflowKey, workflowDefinition, body.Inputs, dryRun, mock)
			if err != nil {
				http.Error(w, "workflow stub execution failed: "+err.Error(), http.StatusBadGateway)
				return
			}
		} else {
			http.Error(w, "workflow provider unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
	} else {
		providerLabel = prov.Name()
		if prov.Supports(action.CapabilityWorkflowCreate) {
			if _, err := prov.CreateWorkflow(r.Context(), action.WorkflowCreateRequest{
				Key:        workflowKey,
				Definition: workflowDefinition,
			}); err != nil && !workflowCreateConflict(err) {
				if dryRun || mock {
					execution, err = executeStudioWorkflowStub(workflowKey, workflowDefinition, body.Inputs, dryRun, mock)
					if err != nil {
						http.Error(w, "workflow stub execution failed: "+err.Error(), http.StatusBadGateway)
						return
					}
				} else {
					http.Error(w, "workflow registration failed: "+err.Error(), http.StatusBadGateway)
					return
				}
			}
		}
		if execution.RunID == "" {
			execution, err = prov.ExecuteWorkflow(r.Context(), action.WorkflowExecuteRequest{
				KeyOrPath: workflowKey,
				Inputs:    body.Inputs,
				DryRun:    dryRun,
				Mock:      mock,
				AllowBash: body.AllowBash,
			})
			if err != nil {
				if dryRun || mock {
					execution, err = executeStudioWorkflowStub(workflowKey, workflowDefinition, body.Inputs, dryRun, mock)
					if err != nil {
						http.Error(w, "workflow stub execution failed: "+err.Error(), http.StatusBadGateway)
						return
					}
				} else {
					now := time.Now().UTC()
					mode := workflowRunModeLabel(dryRun, mock)
					retryAt, rateLimited := externalWorkflowRetryAfter(err, now)
					failKind := "external_workflow_failed"
					failStatus := "failed"
					failSummary := truncateSummary(fmt.Sprintf("Studio workflow %s failed via %s (%s)", workflowKey, titleCaser.String(providerLabel), mode), 140)
					if rateLimited {
						failKind = "external_workflow_rate_limited"
						failStatus = "rate_limited"
						failSummary = truncateSummary(fmt.Sprintf("Studio workflow %s rate-limited via %s (%s)", workflowKey, titleCaser.String(providerLabel), mode), 140)
						retryDelay := time.Until(retryAt)
						if retryDelay < time.Second {
							retryDelay = time.Second
						}
						w.Header().Set("Retry-After", strconv.Itoa(int((retryDelay+time.Second-1)/time.Second)))
					}
					_ = b.RecordAction(failKind, providerLabel, channel, actor, failSummary, workflowKey, nil, "")
					_ = b.UpdateSkillExecutionByWorkflowKey(workflowKey, failStatus, now)
					if rateLimited {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusTooManyRequests)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"ok":           false,
							"workflow_key": workflowKey,
							"provider":     providerLabel,
							"status":       "rate_limited",
							"error":        err.Error(),
							"retry_after":  retryAt.UTC().Format(time.RFC3339Nano),
						})
						return
					}
					http.Error(w, "workflow execution failed: "+err.Error(), http.StatusBadGateway)
					return
				}
			}
		}
	}
	now := time.Now().UTC()
	mode := workflowRunModeLabel(dryRun, mock)
	status := strings.TrimSpace(execution.Status)
	if status == "" {
		status = "completed"
	}
	summary := truncateSummary(fmt.Sprintf("Studio workflow %s ran via %s (%s)", workflowKey, titleCaser.String(providerLabel), mode), 140)
	if err := b.RecordAction("external_workflow_executed", providerLabel, channel, actor, summary, workflowKey, nil, ""); err != nil {
		http.Error(w, "failed to record workflow action", http.StatusInternalServerError)
		return
	}
	if err := b.recordStudioWorkflowExecution(channel, actor, skillName, workflowKey, providerLabel, title, status, now); err != nil {
		http.Error(w, "failed to persist workflow execution", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":           true,
		"skill_name":   skillName,
		"workflow_key": workflowKey,
		"provider":     providerLabel,
		"mode":         mode,
		"status":       status,
		"integrations": integrations,
		"execution": map[string]any{
			"run_id":   execution.RunID,
			"log_file": execution.LogFile,
			"status":   status,
			"steps":    execution.Steps,
			"events":   execution.Events,
		},
	})
}
