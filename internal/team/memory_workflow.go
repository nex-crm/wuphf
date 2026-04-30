package team

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type MemoryWorkflowStep string

const (
	MemoryWorkflowStepLookup  MemoryWorkflowStep = "lookup"
	MemoryWorkflowStepCapture MemoryWorkflowStep = "capture"
	MemoryWorkflowStepPromote MemoryWorkflowStep = "promote"
)

const (
	MemoryWorkflowStatusNotRequired = "not_required"
	MemoryWorkflowStatusPending     = "pending"
	MemoryWorkflowStatusSatisfied   = "satisfied"
	MemoryWorkflowStatusOverridden  = "overridden"
)

const (
	MemoryWorkflowStepStatusPending   = "pending"
	MemoryWorkflowStepStatusSatisfied = "satisfied"
)

// ContextCitation is the backend-neutral citation shape used by the context
// harness. It is intentionally broad enough for markdown, Nex, and GBrain
// sources without making any one backend canonical.
type ContextCitation struct {
	Backend     string   `json:"backend,omitempty"`
	Source      string   `json:"source,omitempty"`
	SourceID    string   `json:"source_id,omitempty"`
	Path        string   `json:"path,omitempty"`
	PageID      string   `json:"page_id,omitempty"`
	ChunkID     string   `json:"chunk_id,omitempty"`
	SourceURL   string   `json:"source_url,omitempty"`
	LineStart   int      `json:"line_start,omitempty"`
	LineEnd     int      `json:"line_end,omitempty"`
	Title       string   `json:"title,omitempty"`
	Snippet     string   `json:"snippet,omitempty"`
	Score       *float64 `json:"score,omitempty"`
	Stale       *bool    `json:"stale,omitempty"`
	RetrievedAt string   `json:"retrieved_at,omitempty"`
}

func (c *ContextCitation) UnmarshalJSON(data []byte) error {
	var raw struct {
		Backend     string   `json:"backend"`
		Source      string   `json:"source"`
		Corpus      string   `json:"corpus"`
		SourceID    any      `json:"source_id"`
		Identifier  any      `json:"identifier"`
		Slug        string   `json:"slug"`
		Path        string   `json:"path"`
		PageID      any      `json:"page_id"`
		ChunkID     any      `json:"chunk_id"`
		SourceURL   string   `json:"source_url"`
		Line        int      `json:"line"`
		LineStart   int      `json:"line_start"`
		LineEnd     int      `json:"line_end"`
		Title       string   `json:"title"`
		Snippet     string   `json:"snippet"`
		Score       *float64 `json:"score"`
		Stale       *bool    `json:"stale"`
		RetrievedAt string   `json:"retrieved_at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	lineStart := raw.LineStart
	if lineStart == 0 {
		lineStart = raw.Line
	}
	lineEnd := raw.LineEnd
	if lineEnd == 0 {
		lineEnd = lineStart
	}
	*c = ContextCitation{
		Backend:     strings.TrimSpace(raw.Backend),
		Source:      firstNonEmptyString(strings.TrimSpace(raw.Source), strings.TrimSpace(raw.Corpus)),
		SourceID:    firstNonEmptyString(jsonScalarString(raw.SourceID), jsonScalarString(raw.Identifier), strings.TrimSpace(raw.Slug)),
		Path:        strings.TrimSpace(raw.Path),
		PageID:      jsonScalarString(raw.PageID),
		ChunkID:     jsonScalarString(raw.ChunkID),
		SourceURL:   strings.TrimSpace(raw.SourceURL),
		LineStart:   lineStart,
		LineEnd:     lineEnd,
		Title:       strings.TrimSpace(raw.Title),
		Snippet:     strings.TrimSpace(raw.Snippet),
		Score:       raw.Score,
		Stale:       raw.Stale,
		RetrievedAt: strings.TrimSpace(raw.RetrievedAt),
	}
	return nil
}

type MemoryWorkflowArtifact struct {
	Backend      string `json:"backend,omitempty"`
	Source       string `json:"source,omitempty"`
	Path         string `json:"path,omitempty"`
	PageID       string `json:"page_id,omitempty"`
	PromotionID  string `json:"promotion_id,omitempty"`
	EntityKind   string `json:"entity_kind,omitempty"`
	EntitySlug   string `json:"entity_slug,omitempty"`
	PlaybookSlug string `json:"playbook_slug,omitempty"`
	Title        string `json:"title,omitempty"`
	Snippet      string `json:"snippet,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	State        string `json:"state,omitempty"`
	RecordedAt   string `json:"recorded_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	Missing      bool   `json:"missing,omitempty"`
}

func jsonScalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%.0f", typed)
		}
		return strings.TrimSpace(fmt.Sprintf("%g", typed))
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type MemoryWorkflowStepState struct {
	Required    bool   `json:"required,omitempty"`
	Status      string `json:"status,omitempty"`
	Actor       string `json:"actor,omitempty"`
	Query       string `json:"query,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Count       int    `json:"count,omitempty"`
}

type MemoryWorkflowOverride struct {
	Actor     string `json:"actor"`
	Reason    string `json:"reason"`
	Timestamp string `json:"timestamp"`
}

type MemoryWorkflow struct {
	Required          bool                     `json:"required"`
	Status            string                   `json:"status,omitempty"`
	RequirementReason string                   `json:"requirement_reason,omitempty"`
	RequiredSteps     []MemoryWorkflowStep     `json:"required_steps,omitempty"`
	Lookup            MemoryWorkflowStepState  `json:"lookup,omitempty"`
	Capture           MemoryWorkflowStepState  `json:"capture,omitempty"`
	Promote           MemoryWorkflowStepState  `json:"promote,omitempty"`
	Citations         []ContextCitation        `json:"citations,omitempty"`
	Captures          []MemoryWorkflowArtifact `json:"captures,omitempty"`
	Promotions        []MemoryWorkflowArtifact `json:"promotions,omitempty"`
	Override          *MemoryWorkflowOverride  `json:"override,omitempty"`
	PartialErrors     []string                 `json:"partial_errors,omitempty"`
	CreatedAt         string                   `json:"created_at,omitempty"`
	UpdatedAt         string                   `json:"updated_at,omitempty"`
	CompletedAt       string                   `json:"completed_at,omitempty"`
}

type memoryWorkflowRequirement struct {
	Required bool
	Steps    []MemoryWorkflowStep
	Reason   string
}

func memoryWorkflowRequirementForTask(task *teamTask) memoryWorkflowRequirement {
	if task == nil {
		return memoryWorkflowRequirement{Reason: "task is nil"}
	}
	taskType := normalizeMemoryWorkflowTaskType(task.TaskType)
	pipelineID := normalizeMemoryWorkflowTaskType(task.PipelineID)
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		task.Channel,
		task.Title,
		task.Details,
		task.TaskType,
		task.PipelineID,
	}, " ")))

	requiredSteps := []MemoryWorkflowStep{
		MemoryWorkflowStepLookup,
		MemoryWorkflowStepCapture,
		MemoryWorkflowStepPromote,
	}
	switch {
	case isProcessResearchTaskType(taskType) || isProcessResearchTaskType(pipelineID):
		return memoryWorkflowRequirement{
			Required: true,
			Steps:    requiredSteps,
			Reason:   "process research requires prior context lookup, durable capture, and promotion",
		}
	case taskType == "research" && researchTaskNeedsPriorContext(text):
		return memoryWorkflowRequirement{
			Required: true,
			Steps:    requiredSteps,
			Reason:   "research task asks for prior organizational context",
		}
	default:
		return memoryWorkflowRequirement{Reason: "task does not require the durable memory workflow"}
	}
}

func normalizeMemoryWorkflowTaskType(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.NewReplacer("-", "_", " ", "_").Replace(raw)
	for strings.Contains(raw, "__") {
		raw = strings.ReplaceAll(raw, "__", "_")
	}
	return strings.Trim(raw, "_")
}

func isProcessResearchTaskType(taskType string) bool {
	switch normalizeMemoryWorkflowTaskType(taskType) {
	case "process_research", "context_research", "memory_research", "knowledge_research":
		return true
	default:
		return false
	}
}

func researchTaskNeedsPriorContext(text string) bool {
	if text == "" {
		return false
	}
	return containsAnyTaskFragment(text,
		"prior context", "previous context", "existing context", "org context",
		"organizational context", "shared context", "context harness",
		"memory", "notebook", "wiki", "gbrain", "nex",
		"past work", "previous work", "prior work", "history", "historical",
		"lesson learned", "lessons learned", "playbook", "canonical",
		"source of truth", "past decision", "prior decision", "previous decision",
		"process research", "process-research", "promotion", "promote",
	)
}

func syncTaskMemoryWorkflow(task *teamTask, timestamp string) {
	if task == nil {
		return
	}
	requirement := memoryWorkflowRequirementForTask(task)
	timestamp = memoryWorkflowTimestamp(timestamp, task)
	if !requirement.Required && task.MemoryWorkflow == nil {
		return
	}
	if task.MemoryWorkflow == nil {
		task.MemoryWorkflow = &MemoryWorkflow{
			CreatedAt: timestamp,
		}
	}
	changed := false
	wf := task.MemoryWorkflow
	if wf.CreatedAt == "" {
		wf.CreatedAt = timestamp
		changed = true
	}
	if wf.Required != requirement.Required {
		wf.Required = requirement.Required
		changed = true
	}
	if wf.RequirementReason != requirement.Reason {
		wf.RequirementReason = requirement.Reason
		changed = true
	}
	if !memoryWorkflowStepsEqual(wf.RequiredSteps, requirement.Steps) {
		wf.RequiredSteps = append([]MemoryWorkflowStep(nil), requirement.Steps...)
		changed = true
	}
	if requirement.Required {
		changed = ensureMemoryWorkflowStepRequirements(wf) || changed
	} else {
		changed = clearMemoryWorkflowStepRequirements(wf) || changed
	}
	changed = refreshMemoryWorkflowStatus(wf, timestamp) || changed
	if changed {
		wf.UpdatedAt = timestamp
	}
}

func ensureMemoryWorkflowForRecording(task *teamTask, timestamp string) *MemoryWorkflow {
	if task == nil {
		return nil
	}
	timestamp = memoryWorkflowTimestamp(timestamp, task)
	if task.MemoryWorkflow == nil {
		task.MemoryWorkflow = &MemoryWorkflow{
			Status:    MemoryWorkflowStatusNotRequired,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		}
	}
	syncTaskMemoryWorkflow(task, timestamp)
	return task.MemoryWorkflow
}

func ensureMemoryWorkflowStepRequirements(wf *MemoryWorkflow) bool {
	if wf == nil {
		return false
	}
	changed := false
	if !wf.Lookup.Required {
		wf.Lookup.Required = true
		changed = true
	}
	if !wf.Capture.Required {
		wf.Capture.Required = true
		changed = true
	}
	if !wf.Promote.Required {
		wf.Promote.Required = true
		changed = true
	}
	if wf.Lookup.Status == "" {
		wf.Lookup.Status = MemoryWorkflowStepStatusPending
		changed = true
	}
	if wf.Capture.Status == "" {
		wf.Capture.Status = MemoryWorkflowStepStatusPending
		changed = true
	}
	if wf.Promote.Status == "" {
		wf.Promote.Status = MemoryWorkflowStepStatusPending
		changed = true
	}
	return changed
}

func clearMemoryWorkflowStepRequirements(wf *MemoryWorkflow) bool {
	if wf == nil {
		return false
	}
	changed := false
	if wf.Lookup.Required {
		wf.Lookup.Required = false
		changed = true
	}
	if wf.Capture.Required {
		wf.Capture.Required = false
		changed = true
	}
	if wf.Promote.Required {
		wf.Promote.Required = false
		changed = true
	}
	return changed
}

func refreshMemoryWorkflowStatus(wf *MemoryWorkflow, timestamp string) bool {
	if wf == nil {
		return false
	}
	oldStatus := wf.Status
	oldCompletedAt := wf.CompletedAt
	oldLookup := wf.Lookup
	oldCapture := wf.Capture
	oldPromote := wf.Promote
	if wf.Override != nil {
		wf.Status = MemoryWorkflowStatusOverridden
		if wf.CompletedAt == "" {
			wf.CompletedAt = wf.Override.Timestamp
		}
		return oldStatus != wf.Status || oldCompletedAt != wf.CompletedAt
	}
	refreshMemoryWorkflowStepStatus(wf, MemoryWorkflowStepLookup)
	refreshMemoryWorkflowStepStatus(wf, MemoryWorkflowStepCapture)
	refreshMemoryWorkflowStepStatus(wf, MemoryWorkflowStepPromote)
	if !wf.Required {
		wf.Status = MemoryWorkflowStatusNotRequired
		wf.CompletedAt = ""
		return oldStatus != wf.Status || oldCompletedAt != wf.CompletedAt
	}
	if memoryWorkflowStepSatisfied(wf.Lookup) &&
		memoryWorkflowStepSatisfied(wf.Capture) &&
		memoryWorkflowStepSatisfied(wf.Promote) {
		wf.Status = MemoryWorkflowStatusSatisfied
		if wf.CompletedAt == "" {
			wf.CompletedAt = timestamp
		}
	} else {
		wf.Status = MemoryWorkflowStatusPending
		wf.CompletedAt = ""
	}
	return oldStatus != wf.Status ||
		oldCompletedAt != wf.CompletedAt ||
		oldLookup != wf.Lookup ||
		oldCapture != wf.Capture ||
		oldPromote != wf.Promote
}

func refreshMemoryWorkflowStepStatus(wf *MemoryWorkflow, step MemoryWorkflowStep) {
	if wf == nil {
		return
	}
	switch step {
	case MemoryWorkflowStepLookup:
		if len(wf.Citations) > 0 {
			wf.Lookup.Status = MemoryWorkflowStepStatusSatisfied
			wf.Lookup.Count = len(wf.Citations)
			return
		}
		wf.Lookup.CompletedAt = ""
		wf.Lookup.Count = 0
		if wf.Lookup.Required {
			wf.Lookup.Status = MemoryWorkflowStepStatusPending
		}
	case MemoryWorkflowStepCapture:
		count := countPresentArtifacts(wf.Captures)
		if count > 0 {
			wf.Capture.Status = MemoryWorkflowStepStatusSatisfied
			wf.Capture.Count = count
			return
		}
		if hasMissingMemoryWorkflowArtifact(wf.Captures) {
			wf.Capture.CompletedAt = ""
			wf.Capture.Status = MemoryWorkflowStepStatusPending
			wf.Capture.Count = 0
			return
		}
		if wf.Capture.CompletedAt != "" {
			wf.Capture.Status = MemoryWorkflowStepStatusSatisfied
			wf.Capture.Count = 0
			return
		}
		if wf.Capture.Required {
			wf.Capture.Status = MemoryWorkflowStepStatusPending
			wf.Capture.Count = 0
		}
	case MemoryWorkflowStepPromote:
		count := countPresentArtifacts(wf.Promotions)
		if count > 0 {
			wf.Promote.Status = MemoryWorkflowStepStatusSatisfied
			wf.Promote.Count = count
			return
		}
		if hasMissingMemoryWorkflowArtifact(wf.Promotions) {
			wf.Promote.CompletedAt = ""
			wf.Promote.Status = MemoryWorkflowStepStatusPending
			wf.Promote.Count = 0
			return
		}
		if wf.Promote.CompletedAt != "" {
			wf.Promote.Status = MemoryWorkflowStepStatusSatisfied
			wf.Promote.Count = 0
			return
		}
		if wf.Promote.Required {
			wf.Promote.Status = MemoryWorkflowStepStatusPending
			wf.Promote.Count = 0
		}
	}
}

func countPresentArtifacts(artifacts []MemoryWorkflowArtifact) int {
	count := 0
	for _, artifact := range artifacts {
		if !artifact.Missing {
			count++
		}
	}
	return count
}

func hasMissingMemoryWorkflowArtifact(artifacts []MemoryWorkflowArtifact) bool {
	for _, artifact := range artifacts {
		if artifact.Missing {
			return true
		}
	}
	return false
}

func memoryWorkflowStepSatisfied(step MemoryWorkflowStepState) bool {
	return step.Status == MemoryWorkflowStepStatusSatisfied
}

func taskMemoryWorkflowReady(task *teamTask) bool {
	if task == nil {
		return true
	}
	syncTaskMemoryWorkflow(task, "")
	wf := task.MemoryWorkflow
	if wf == nil || !wf.Required {
		return true
	}
	return wf.Status == MemoryWorkflowStatusSatisfied || wf.Status == MemoryWorkflowStatusOverridden
}

func missingMemoryWorkflowSteps(task *teamTask) []MemoryWorkflowStep {
	if task == nil || task.MemoryWorkflow == nil || !task.MemoryWorkflow.Required {
		return nil
	}
	wf := task.MemoryWorkflow
	var missing []MemoryWorkflowStep
	if !memoryWorkflowStepSatisfied(wf.Lookup) {
		missing = append(missing, MemoryWorkflowStepLookup)
	}
	if !memoryWorkflowStepSatisfied(wf.Capture) {
		missing = append(missing, MemoryWorkflowStepCapture)
	}
	if !memoryWorkflowStepSatisfied(wf.Promote) {
		missing = append(missing, MemoryWorkflowStepPromote)
	}
	return missing
}

func applyMemoryWorkflowCompletionGate(task *teamTask, actor, reason string, override bool, timestamp string) error {
	if task == nil {
		return nil
	}
	syncTaskMemoryWorkflow(task, timestamp)
	wf := task.MemoryWorkflow
	if wf == nil || !wf.Required {
		return nil
	}
	if taskMemoryWorkflowReady(task) {
		return nil
	}
	if !override {
		missing := missingMemoryWorkflowSteps(task)
		return fmt.Errorf("memory workflow incomplete: task requires context %s before completion; provide memory_workflow_override with a human reason to override", joinMemoryWorkflowSteps(missing))
	}
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if actor == "" {
		return fmt.Errorf("memory workflow override requires actor")
	}
	if reason == "" {
		return fmt.Errorf("memory workflow override requires reason")
	}
	timestamp = memoryWorkflowTimestamp(timestamp, task)
	wf.Override = &MemoryWorkflowOverride{
		Actor:     actor,
		Reason:    reason,
		Timestamp: timestamp,
	}
	wf.Status = MemoryWorkflowStatusOverridden
	wf.CompletedAt = timestamp
	wf.UpdatedAt = timestamp
	return nil
}

func joinMemoryWorkflowSteps(steps []MemoryWorkflowStep) string {
	if len(steps) == 0 {
		return "workflow"
	}
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		parts = append(parts, string(step))
	}
	return strings.Join(parts, ", ")
}

func recordMemoryWorkflowLookup(task *teamTask, actor, query string, citations []ContextCitation, timestamp string) bool {
	wf := ensureMemoryWorkflowForRecording(task, timestamp)
	if wf == nil {
		return false
	}
	timestamp = memoryWorkflowTimestamp(timestamp, task)
	changed := false
	stepChanged := false
	actor = strings.TrimSpace(actor)
	query = strings.TrimSpace(query)
	if wf.Lookup.Actor != actor {
		wf.Lookup.Actor = actor
		stepChanged = true
	}
	if wf.Lookup.Query != query {
		wf.Lookup.Query = query
		stepChanged = true
	}
	if len(citations) > 0 && wf.Lookup.CompletedAt == "" {
		wf.Lookup.CompletedAt = timestamp
		stepChanged = true
	}
	for _, citation := range citations {
		normalized := normalizeContextCitation(citation, timestamp)
		if appendContextCitation(&wf.Citations, normalized) {
			stepChanged = true
		}
	}
	if stepChanged && wf.Lookup.UpdatedAt != timestamp {
		wf.Lookup.UpdatedAt = timestamp
	}
	changed = stepChanged
	changed = refreshMemoryWorkflowStatus(wf, timestamp) || changed
	if changed {
		wf.UpdatedAt = timestamp
	}
	return changed
}

func recordMemoryWorkflowCapture(task *teamTask, actor string, artifact MemoryWorkflowArtifact, timestamp string) bool {
	return recordMemoryWorkflowArtifact(task, actor, artifact, timestamp, MemoryWorkflowStepCapture)
}

func recordMemoryWorkflowPromotion(task *teamTask, actor string, artifact MemoryWorkflowArtifact, timestamp string) bool {
	return recordMemoryWorkflowArtifact(task, actor, artifact, timestamp, MemoryWorkflowStepPromote)
}

func recordMemoryWorkflowArtifact(task *teamTask, actor string, artifact MemoryWorkflowArtifact, timestamp string, step MemoryWorkflowStep) bool {
	wf := ensureMemoryWorkflowForRecording(task, timestamp)
	if wf == nil {
		return false
	}
	timestamp = memoryWorkflowTimestamp(timestamp, task)
	artifact = normalizeMemoryWorkflowArtifact(artifact, timestamp)
	if memoryWorkflowArtifactKey(artifact) == "" {
		return false
	}
	changed := false
	stepChanged := false
	switch step {
	case MemoryWorkflowStepCapture:
		if appendMemoryWorkflowArtifact(&wf.Captures, artifact) {
			stepChanged = true
		}
		if wf.Capture.Actor != strings.TrimSpace(actor) {
			wf.Capture.Actor = strings.TrimSpace(actor)
			stepChanged = true
		}
		if wf.Capture.CompletedAt == "" {
			wf.Capture.CompletedAt = timestamp
			stepChanged = true
		}
		if stepChanged && wf.Capture.UpdatedAt != timestamp {
			wf.Capture.UpdatedAt = timestamp
		}
	case MemoryWorkflowStepPromote:
		if appendMemoryWorkflowArtifact(&wf.Promotions, artifact) {
			stepChanged = true
		}
		if wf.Promote.Actor != strings.TrimSpace(actor) {
			wf.Promote.Actor = strings.TrimSpace(actor)
			stepChanged = true
		}
		if wf.Promote.CompletedAt == "" {
			wf.Promote.CompletedAt = timestamp
			stepChanged = true
		}
		if stepChanged && wf.Promote.UpdatedAt != timestamp {
			wf.Promote.UpdatedAt = timestamp
		}
	}
	changed = stepChanged
	changed = refreshMemoryWorkflowStatus(wf, timestamp) || changed
	if changed {
		wf.UpdatedAt = timestamp
	}
	return changed
}

func appendContextCitation(citations *[]ContextCitation, citation ContextCitation) bool {
	for i := range *citations {
		if contextCitationKey((*citations)[i]) == contextCitationKey(citation) {
			merged := mergeContextCitation((*citations)[i], citation)
			changed := !contextCitationEqual((*citations)[i], merged)
			(*citations)[i] = merged
			return changed
		}
	}
	*citations = append(*citations, citation)
	return true
}

func appendMemoryWorkflowArtifact(artifacts *[]MemoryWorkflowArtifact, artifact MemoryWorkflowArtifact) bool {
	if memoryWorkflowArtifactKey(artifact) == "" {
		return false
	}
	for i := range *artifacts {
		if memoryWorkflowArtifactKey((*artifacts)[i]) == memoryWorkflowArtifactKey(artifact) {
			merged := mergeMemoryWorkflowArtifact((*artifacts)[i], artifact)
			changed := (*artifacts)[i] != merged
			(*artifacts)[i] = merged
			return changed
		}
	}
	*artifacts = append(*artifacts, artifact)
	return true
}

func contextCitationEqual(a, b ContextCitation) bool {
	if a.Backend != b.Backend || a.Source != b.Source || a.SourceID != b.SourceID ||
		a.Path != b.Path || a.PageID != b.PageID || a.ChunkID != b.ChunkID ||
		a.SourceURL != b.SourceURL || a.LineStart != b.LineStart || a.LineEnd != b.LineEnd ||
		a.Title != b.Title || a.Snippet != b.Snippet || a.RetrievedAt != b.RetrievedAt {
		return false
	}
	if (a.Score == nil) != (b.Score == nil) || (a.Stale == nil) != (b.Stale == nil) {
		return false
	}
	if a.Score != nil && b.Score != nil && *a.Score != *b.Score {
		return false
	}
	if a.Stale != nil && b.Stale != nil && *a.Stale != *b.Stale {
		return false
	}
	return true
}

func normalizeContextCitation(citation ContextCitation, timestamp string) ContextCitation {
	citation.Backend = strings.TrimSpace(citation.Backend)
	citation.Source = strings.TrimSpace(citation.Source)
	citation.SourceID = strings.TrimSpace(citation.SourceID)
	citation.Path = strings.TrimSpace(citation.Path)
	citation.PageID = strings.TrimSpace(citation.PageID)
	citation.ChunkID = strings.TrimSpace(citation.ChunkID)
	citation.SourceURL = strings.TrimSpace(citation.SourceURL)
	citation.Title = strings.TrimSpace(citation.Title)
	citation.Snippet = strings.TrimSpace(citation.Snippet)
	if citation.RetrievedAt == "" {
		citation.RetrievedAt = timestamp
	}
	return citation
}

func normalizeMemoryWorkflowArtifact(artifact MemoryWorkflowArtifact, timestamp string) MemoryWorkflowArtifact {
	artifact.Backend = strings.TrimSpace(artifact.Backend)
	artifact.Source = strings.TrimSpace(artifact.Source)
	artifact.Path = strings.TrimSpace(artifact.Path)
	artifact.PageID = strings.TrimSpace(artifact.PageID)
	artifact.PromotionID = strings.TrimSpace(artifact.PromotionID)
	artifact.EntityKind = strings.TrimSpace(artifact.EntityKind)
	artifact.EntitySlug = strings.TrimSpace(artifact.EntitySlug)
	artifact.PlaybookSlug = strings.TrimSpace(artifact.PlaybookSlug)
	artifact.Title = strings.TrimSpace(artifact.Title)
	artifact.Snippet = strings.TrimSpace(artifact.Snippet)
	artifact.CommitSHA = strings.TrimSpace(artifact.CommitSHA)
	artifact.State = strings.TrimSpace(artifact.State)
	if artifact.RecordedAt == "" {
		artifact.RecordedAt = timestamp
	}
	if artifact.UpdatedAt == "" {
		artifact.UpdatedAt = timestamp
	}
	return artifact
}

func contextCitationKey(citation ContextCitation) string {
	parts := []string{
		citation.Backend,
		citation.Source,
		citation.SourceID,
		citation.Path,
		citation.PageID,
		citation.ChunkID,
		citation.SourceURL,
		fmt.Sprintf("%d", citation.LineStart),
		fmt.Sprintf("%d", citation.LineEnd),
	}
	key := strings.Trim(strings.Join(parts, "|"), "|")
	if key == "" {
		key = strings.TrimSpace(citation.Title + "|" + citation.Snippet)
	}
	return key
}

func memoryWorkflowArtifactKey(artifact MemoryWorkflowArtifact) string {
	parts := []string{
		artifact.Backend,
		artifact.Source,
		artifact.Path,
		artifact.PageID,
		artifact.PromotionID,
		artifact.EntityKind,
		artifact.EntitySlug,
		artifact.PlaybookSlug,
	}
	return strings.Trim(strings.Join(parts, "|"), "|")
}

func mergeContextCitation(existing, incoming ContextCitation) ContextCitation {
	if existing.Backend == "" {
		existing.Backend = incoming.Backend
	}
	if existing.Source == "" {
		existing.Source = incoming.Source
	}
	if existing.SourceID == "" {
		existing.SourceID = incoming.SourceID
	}
	if existing.Path == "" {
		existing.Path = incoming.Path
	}
	if existing.PageID == "" {
		existing.PageID = incoming.PageID
	}
	if existing.ChunkID == "" {
		existing.ChunkID = incoming.ChunkID
	}
	if existing.SourceURL == "" {
		existing.SourceURL = incoming.SourceURL
	}
	if existing.LineStart == 0 {
		existing.LineStart = incoming.LineStart
	}
	if existing.LineEnd == 0 {
		existing.LineEnd = incoming.LineEnd
	}
	if existing.Title == "" {
		existing.Title = incoming.Title
	}
	if existing.Snippet == "" {
		existing.Snippet = incoming.Snippet
	}
	if existing.Score == nil {
		existing.Score = incoming.Score
	}
	if existing.Stale == nil {
		existing.Stale = incoming.Stale
	}
	if existing.RetrievedAt == "" {
		existing.RetrievedAt = incoming.RetrievedAt
	}
	return existing
}

func mergeMemoryWorkflowArtifact(existing, incoming MemoryWorkflowArtifact) MemoryWorkflowArtifact {
	before := existing
	if existing.Backend == "" {
		existing.Backend = incoming.Backend
	}
	if existing.Source == "" {
		existing.Source = incoming.Source
	}
	if existing.Path == "" {
		existing.Path = incoming.Path
	}
	if existing.PageID == "" {
		existing.PageID = incoming.PageID
	}
	if existing.PromotionID == "" {
		existing.PromotionID = incoming.PromotionID
	}
	if existing.EntityKind == "" {
		existing.EntityKind = incoming.EntityKind
	}
	if existing.EntitySlug == "" {
		existing.EntitySlug = incoming.EntitySlug
	}
	if existing.PlaybookSlug == "" {
		existing.PlaybookSlug = incoming.PlaybookSlug
	}
	if existing.Title == "" {
		existing.Title = incoming.Title
	}
	if existing.Snippet == "" {
		existing.Snippet = incoming.Snippet
	}
	if existing.CommitSHA == "" {
		existing.CommitSHA = incoming.CommitSHA
	}
	if incoming.State != "" {
		existing.State = incoming.State
	}
	if existing.RecordedAt == "" {
		existing.RecordedAt = incoming.RecordedAt
	}
	contentChanged := existing.Backend != before.Backend ||
		existing.Source != before.Source ||
		existing.Path != before.Path ||
		existing.PageID != before.PageID ||
		existing.PromotionID != before.PromotionID ||
		existing.EntityKind != before.EntityKind ||
		existing.EntitySlug != before.EntitySlug ||
		existing.PlaybookSlug != before.PlaybookSlug ||
		existing.Title != before.Title ||
		existing.Snippet != before.Snippet ||
		existing.CommitSHA != before.CommitSHA ||
		existing.State != before.State ||
		existing.RecordedAt != before.RecordedAt ||
		existing.Missing != incoming.Missing
	if incoming.UpdatedAt != "" && (existing.UpdatedAt == "" || contentChanged) {
		existing.UpdatedAt = incoming.UpdatedAt
	}
	existing.Missing = incoming.Missing
	return existing
}

func memoryWorkflowStepsEqual(a, b []MemoryWorkflowStep) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func memoryWorkflowTimestamp(timestamp string, task *teamTask) string {
	timestamp = strings.TrimSpace(timestamp)
	if timestamp != "" {
		return timestamp
	}
	if task != nil {
		if strings.TrimSpace(task.UpdatedAt) != "" {
			return strings.TrimSpace(task.UpdatedAt)
		}
		if strings.TrimSpace(task.CreatedAt) != "" {
			return strings.TrimSpace(task.CreatedAt)
		}
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func cloneTeamTaskForRollback(task teamTask) teamTask {
	cloned := task
	if task.DependsOn != nil {
		cloned.DependsOn = append([]string(nil), task.DependsOn...)
	}
	cloned.MemoryWorkflow = cloneMemoryWorkflow(task.MemoryWorkflow)
	return cloned
}

func cloneMemoryWorkflow(wf *MemoryWorkflow) *MemoryWorkflow {
	if wf == nil {
		return nil
	}
	cloned := *wf
	if wf.RequiredSteps != nil {
		cloned.RequiredSteps = append([]MemoryWorkflowStep(nil), wf.RequiredSteps...)
	}
	if wf.Citations != nil {
		cloned.Citations = append([]ContextCitation(nil), wf.Citations...)
	}
	if wf.Captures != nil {
		cloned.Captures = append([]MemoryWorkflowArtifact(nil), wf.Captures...)
	}
	if wf.Promotions != nil {
		cloned.Promotions = append([]MemoryWorkflowArtifact(nil), wf.Promotions...)
	}
	if wf.PartialErrors != nil {
		cloned.PartialErrors = append([]string(nil), wf.PartialErrors...)
	}
	if wf.Override != nil {
		override := *wf.Override
		cloned.Override = &override
	}
	return &cloned
}
