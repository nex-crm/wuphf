package team

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	taskMemoryPolicyNone        = "none"
	taskMemoryPolicyRecommended = "recommended"
	taskMemoryPolicyRequired    = "required"

	taskMemoryEvidencePriorSearch       = "prior_search"
	taskMemoryEvidenceNotebookWrite     = "notebook_write"
	taskMemoryEvidencePromotionDecision = "promotion_decision"

	taskMemoryDecisionSubmitted = "submitted"
	taskMemoryDecisionPromoted  = "promoted"
	taskMemoryDecisionSkipped   = "skipped"
)

type taskMemoryEvidence struct {
	Kind        string `json:"kind"`
	Tool        string `json:"tool,omitempty"`
	Actor       string `json:"actor,omitempty"`
	Query       string `json:"query,omitempty"`
	Topic       string `json:"topic,omitempty"`
	TargetSlug  string `json:"target_slug,omitempty"`
	Path        string `json:"path,omitempty"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	PromotionID string `json:"promotion_id,omitempty"`
	Decision    string `json:"decision,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Hits        int    `json:"hits,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type taskMemoryChecklist struct {
	Required          bool     `json:"required"`
	PriorSearch       bool     `json:"prior_search"`
	NotebookWrite     bool     `json:"notebook_write"`
	PromotionDecision bool     `json:"promotion_decision"`
	Complete          bool     `json:"complete"`
	Missing           []string `json:"missing,omitempty"`
}

func normalizeTaskMemoryPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "auto":
		return ""
	case "none", "off", "disabled", "not_required", "not-required":
		return taskMemoryPolicyNone
	case "recommend", "recommended":
		return taskMemoryPolicyRecommended
	case "require", "required", "must":
		return taskMemoryPolicyRequired
	default:
		return ""
	}
}

func normalizeTaskMemoryDecision(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case taskMemoryDecisionSubmitted, "submit":
		return taskMemoryDecisionSubmitted
	case taskMemoryDecisionPromoted, "promote":
		return taskMemoryDecisionPromoted
	case taskMemoryDecisionSkipped, "skip":
		return taskMemoryDecisionSkipped
	default:
		return ""
	}
}

func normalizeTaskMemoryPlan(task *teamTask) {
	if task == nil {
		return
	}
	policy := normalizeTaskMemoryPolicy(task.MemoryPolicy)
	if policy == "" {
		policy = inferTaskMemoryPolicy(task)
	}
	task.MemoryPolicy = policy
	if task.MemoryPolicy == "" {
		task.MemoryPolicy = taskMemoryPolicyNone
	}
	if strings.TrimSpace(task.MemoryTopic) == "" && task.MemoryPolicy != taskMemoryPolicyNone {
		task.MemoryTopic = inferTaskMemoryTopic(task)
	}
	refreshTaskMemoryChecklist(task)
}

func inferTaskMemoryPolicy(task *teamTask) string {
	if task == nil {
		return taskMemoryPolicyNone
	}
	text := strings.ToLower(strings.Join([]string{
		task.Owner,
		task.TaskType,
		task.Title,
		task.Details,
	}, " "))
	if looksLikeProcessResearch(text) {
		return taskMemoryPolicyRequired
	}
	if strings.EqualFold(strings.TrimSpace(task.TaskType), "research") {
		return taskMemoryPolicyRecommended
	}
	return taskMemoryPolicyNone
}

func looksLikeProcessResearch(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	if containsAnyTaskFragment(text,
		"passport", "visa", "uscis", "immigration", "consulate",
		"permit", "license", "licence", "dmv", "notary",
		"tax filing", "irs", "incorporation", "government form",
		"department of state", "renewal requirements",
	) {
		return true
	}
	return containsAnyTaskFragment(text,
		"application process", "how to apply", "steps to apply",
		"process for applying", "requirements for applying",
		"filing process", "renewal process", "government process",
		"vendor onboarding process", "procurement process",
	)
}

func inferTaskMemoryTopic(task *teamTask) string {
	if task == nil {
		return ""
	}
	text := strings.ToLower(strings.Join([]string{task.Title, task.Details}, " "))
	switch {
	case strings.Contains(text, "passport"):
		return "passport-application"
	case strings.Contains(text, "visa"):
		return "visa-application"
	case strings.Contains(text, "tax filing") || strings.Contains(text, "irs"):
		return "tax-filing"
	case strings.Contains(text, "vendor onboarding"):
		return "vendor-onboarding"
	case strings.Contains(text, "procurement"):
		return "procurement-process"
	}
	return slugifyTaskMemoryTopic(task.Title)
}

func slugifyTaskMemoryTopic(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func refreshTaskMemoryChecklist(task *teamTask) {
	if task == nil {
		return
	}
	if task.MemoryPolicy == taskMemoryPolicyNone && len(task.MemoryEvidence) == 0 {
		task.MemoryChecklist = nil
		return
	}
	check := &taskMemoryChecklist{
		Required: task.MemoryPolicy == taskMemoryPolicyRequired,
	}
	for _, ev := range task.MemoryEvidence {
		switch ev.Kind {
		case taskMemoryEvidencePriorSearch:
			check.PriorSearch = true
		case taskMemoryEvidenceNotebookWrite:
			check.NotebookWrite = true
		case taskMemoryEvidencePromotionDecision:
			if normalizeTaskMemoryDecision(ev.Decision) != "" {
				check.PromotionDecision = true
			}
		}
	}
	if check.Required {
		if !check.PriorSearch {
			check.Missing = append(check.Missing, "prior memory search")
		}
		if !check.NotebookWrite {
			check.Missing = append(check.Missing, "notebook write")
		}
		if !check.PromotionDecision {
			check.Missing = append(check.Missing, "promote-or-skip decision")
		}
	}
	check.Complete = check.PriorSearch && check.NotebookWrite && check.PromotionDecision
	task.MemoryChecklist = check
}

func taskMemoryGateError(task *teamTask) error {
	normalizeTaskMemoryPlan(task)
	if task == nil || task.MemoryPolicy != taskMemoryPolicyRequired {
		return nil
	}
	if task.MemoryChecklist != nil && task.MemoryChecklist.Complete {
		return nil
	}
	missing := []string{"prior memory search", "notebook write", "promote-or-skip decision"}
	if task.MemoryChecklist != nil && len(task.MemoryChecklist.Missing) > 0 {
		missing = task.MemoryChecklist.Missing
	}
	return fmt.Errorf("memory workflow required before completing task: missing %s", strings.Join(missing, ", "))
}

func (b *Broker) validateTaskMemoryEvidenceTarget(taskID string) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			return nil
		}
	}
	return fmt.Errorf("task %q not found", taskID)
}

func (b *Broker) RecordTaskMemoryEvidence(taskID string, evidence taskMemoryEvidence) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID != taskID {
			continue
		}
		appendTaskMemoryEvidenceLocked(&b.tasks[i], evidence)
		b.tasks[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return b.saveLocked()
	}
	return fmt.Errorf("task %q not found", taskID)
}

func appendTaskMemoryEvidenceLocked(task *teamTask, evidence taskMemoryEvidence) {
	if task == nil {
		return
	}
	evidence.Kind = strings.TrimSpace(evidence.Kind)
	evidence.Tool = strings.TrimSpace(evidence.Tool)
	evidence.Actor = strings.TrimSpace(evidence.Actor)
	evidence.Query = strings.TrimSpace(evidence.Query)
	evidence.Topic = strings.TrimSpace(evidence.Topic)
	evidence.TargetSlug = strings.TrimSpace(evidence.TargetSlug)
	evidence.Path = strings.TrimSpace(evidence.Path)
	evidence.CommitSHA = strings.TrimSpace(evidence.CommitSHA)
	evidence.PromotionID = strings.TrimSpace(evidence.PromotionID)
	evidence.Decision = normalizeTaskMemoryDecision(evidence.Decision)
	evidence.Reason = strings.TrimSpace(evidence.Reason)
	if strings.TrimSpace(evidence.CreatedAt) == "" {
		evidence.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if evidence.Kind == "" {
		return
	}
	for _, existing := range task.MemoryEvidence {
		if sameTaskMemoryEvidence(existing, evidence) {
			refreshTaskMemoryChecklist(task)
			return
		}
	}
	task.MemoryEvidence = append(task.MemoryEvidence, evidence)
	const maxTaskMemoryEvidence = 80
	if len(task.MemoryEvidence) > maxTaskMemoryEvidence {
		task.MemoryEvidence = task.MemoryEvidence[len(task.MemoryEvidence)-maxTaskMemoryEvidence:]
	}
	refreshTaskMemoryChecklist(task)
}

func sameTaskMemoryEvidence(a, b taskMemoryEvidence) bool {
	return a.Kind == b.Kind &&
		a.Tool == b.Tool &&
		a.Query == b.Query &&
		a.Topic == b.Topic &&
		a.TargetSlug == b.TargetSlug &&
		a.Path == b.Path &&
		a.CommitSHA == b.CommitSHA &&
		a.PromotionID == b.PromotionID &&
		a.Decision == b.Decision
}

func applyTaskPromotionDecisionLocked(task *teamTask, actor, decision, notebookPath, reason string) error {
	if task == nil {
		return fmt.Errorf("task required")
	}
	decision = normalizeTaskMemoryDecision(decision)
	if decision == "" {
		return fmt.Errorf("promotion_decision must be one of submitted, promoted, or skipped")
	}
	notebookPath = filepath.ToSlash(strings.TrimSpace(notebookPath))
	if notebookPath == "" {
		return fmt.Errorf("notebook_path is required for a promotion decision")
	}
	if !taskHasNotebookWriteEvidence(task, notebookPath) {
		return fmt.Errorf("notebook_path %q must match a successful notebook_write for this task", notebookPath)
	}
	reason = strings.TrimSpace(reason)
	if decision == taskMemoryDecisionSkipped && reason == "" {
		return fmt.Errorf("promotion_reason is required when promotion_decision is skipped")
	}
	appendTaskMemoryEvidenceLocked(task, taskMemoryEvidence{
		Kind:      taskMemoryEvidencePromotionDecision,
		Tool:      "team_task",
		Actor:     actor,
		Path:      notebookPath,
		Decision:  decision,
		Reason:    reason,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	return nil
}

func taskHasNotebookWriteEvidence(task *teamTask, notebookPath string) bool {
	notebookPath = filepath.ToSlash(strings.TrimSpace(notebookPath))
	for _, ev := range task.MemoryEvidence {
		if ev.Kind == taskMemoryEvidenceNotebookWrite && filepath.ToSlash(strings.TrimSpace(ev.Path)) == notebookPath {
			return true
		}
	}
	return false
}
