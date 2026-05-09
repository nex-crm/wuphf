package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

func (b *Broker) handleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetSkills(w, r)
	case http.MethodPost:
		b.handlePostSkill(w, r)
	case http.MethodPut:
		b.handlePutSkill(w, r)
	case http.MethodDelete:
		b.handleDeleteSkill(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleSkillsSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/skills/")
	if strings.HasSuffix(path, "/invoke") {
		b.handleInvokeSkill(w, r)
		return
	}
	// PR 1b CRUD verbs: patch / archive / files / approve / reject + reject/undo.
	if b.handleSkillsCRUDSubpath(w, r) {
		return
	}
	// PR 1b PUT /skills/{name} — full SKILL.md replacement.
	if b.handleSkillEditOnName(w, r) {
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func skillSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

func (b *Broker) findSkillByNameLocked(name string) *teamSkill {
	slug := skillSlug(name)
	for i := range b.skills {
		if skillSlug(b.skills[i].Name) == slug && b.skills[i].Status != "archived" {
			return &b.skills[i]
		}
	}
	return nil
}

// findSkillByNameIncludingArchivedLocked is the archive-aware sibling of
// findSkillByNameLocked. It exists so /skills/{name}/restore can locate a
// skill that the regular lookup intentionally hides. Caller holds b.mu.
func (b *Broker) findSkillByNameIncludingArchivedLocked(name string) *teamSkill {
	slug := skillSlug(name)
	for i := range b.skills {
		if skillSlug(b.skills[i].Name) == slug {
			return &b.skills[i]
		}
	}
	return nil
}

// allocateSkillIDLocked mints a new skill ID derived from the name's slug.
// If the bare slug-based ID already exists in the broker (active OR archived),
// a numeric discriminator is appended so create→archive→create cycles can't
// collide. Existing IDs are preserved on the common, no-collision path.
func (b *Broker) allocateSkillIDLocked(name string) string {
	base := fmt.Sprintf("skill-%s", skillSlug(name))
	if !b.skillIDExistsLocked(base) {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !b.skillIDExistsLocked(candidate) {
			return candidate
		}
	}
}

func (b *Broker) skillIDExistsLocked(id string) bool {
	for i := range b.skills {
		if b.skills[i].ID == id {
			return true
		}
	}
	return false
}

func (b *Broker) findSkillByWorkflowKeyLocked(key string) *teamSkill {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	for i := range b.skills {
		if strings.TrimSpace(b.skills[i].WorkflowKey) == key && b.skills[i].Status != "archived" {
			return &b.skills[i]
		}
	}
	return nil
}

func (b *Broker) handleGetSkills(w http.ResponseWriter, r *http.Request) {
	channelFilter := normalizeChannelSlug(r.URL.Query().Get("channel"))

	b.mu.Lock()
	result := make([]teamSkill, 0, len(b.skills))
	for _, sk := range b.skills {
		if sk.Status == "archived" {
			continue
		}
		if channelFilter != "" && normalizeChannelSlug(sk.Channel) != channelFilter {
			continue
		}
		result = append(result, sk)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skills": result})
}

func (b *Broker) handlePostSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action              string   `json:"action"`
		Name                string   `json:"name"`
		Title               string   `json:"title"`
		Description         string   `json:"description"`
		Content             string   `json:"content"`
		CreatedBy           string   `json:"created_by"`
		Channel             string   `json:"channel"`
		Tags                []string `json:"tags"`
		Trigger             string   `json:"trigger"`
		WorkflowProvider    string   `json:"workflow_provider"`
		WorkflowKey         string   `json:"workflow_key"`
		WorkflowDefinition  string   `json:"workflow_definition"`
		WorkflowSchedule    string   `json:"workflow_schedule"`
		RelayID             string   `json:"relay_id"`
		RelayPlatform       string   `json:"relay_platform"`
		RelayEventTypes     []string `json:"relay_event_types"`
		LastExecutionAt     string   `json:"last_execution_at"`
		LastExecutionStatus string   `json:"last_execution_status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(body.Action)
	if action == "" {
		action = "create"
	}
	if action != "create" && action != "propose" {
		http.Error(w, "action must be create or propose", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Content) == "" || strings.TrimSpace(body.CreatedBy) == "" || strings.TrimSpace(body.Description) == "" {
		// Description is required by RenderSkillMarkdown — without it the
		// SKILL.md write would silently no-op and the wiki UI would 404 on
		// the skill, which is exactly the regression this PR fixes.
		http.Error(w, "name, description, content, and created_by required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	status := "active"
	msgKind := "skill_update"
	if action == "propose" {
		status = "proposed"
		msgKind = "skill_proposal"
	}

	b.mu.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			b.mu.Unlock()
		}
	}()

	createdBy := strings.TrimSpace(body.CreatedBy)
	if action == "propose" {
		createdBy = normalizeActorSlug(createdBy)
		if b.findMemberLocked(createdBy) == nil {
			http.Error(w, "created_by must be a registered agent for skill proposals", http.StatusForbidden)
			return
		}
	}

	if existing := b.findSkillByNameLocked(body.Name); existing != nil {
		http.Error(w, "skill with this name already exists", http.StatusConflict)
		return
	}

	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = strings.TrimSpace(body.Name)
	}

	b.counter++
	sk := teamSkill{
		ID:                  b.allocateSkillIDLocked(body.Name),
		Name:                strings.TrimSpace(body.Name),
		Title:               title,
		Description:         strings.TrimSpace(body.Description),
		Content:             strings.TrimSpace(body.Content),
		CreatedBy:           createdBy,
		Channel:             channel,
		Tags:                body.Tags,
		Trigger:             strings.TrimSpace(body.Trigger),
		WorkflowProvider:    strings.TrimSpace(body.WorkflowProvider),
		WorkflowKey:         strings.TrimSpace(body.WorkflowKey),
		WorkflowDefinition:  strings.TrimSpace(body.WorkflowDefinition),
		WorkflowSchedule:    strings.TrimSpace(body.WorkflowSchedule),
		RelayID:             strings.TrimSpace(body.RelayID),
		RelayPlatform:       strings.TrimSpace(body.RelayPlatform),
		RelayEventTypes:     append([]string(nil), body.RelayEventTypes...),
		LastExecutionAt:     strings.TrimSpace(body.LastExecutionAt),
		LastExecutionStatus: strings.TrimSpace(body.LastExecutionStatus),
		UsageCount:          0,
		Status:              status,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	b.skills = append(b.skills, sk)

	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      sk.CreatedBy,
		Channel:   channel,
		Kind:      msgKind,
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q %sd by @%s", sk.Name, action, sk.CreatedBy),
		Timestamp: now,
	})
	b.appendActionLocked(msgKind, "office", channel, sk.CreatedBy, truncateSummary(sk.Title, 140), sk.ID)
	if action == "propose" {
		b.appendSkillProposalRequestLocked(sk, channel, now)
	}

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	// Enqueue the SKILL.md write so the wiki has the file on disk. Without
	// this, a freshly created skill exists in broker-state.json but
	// /wiki/article?path=team/skills/<slug>.md returns "no such file" until
	// the skill is edited or archived. Mirrors the pattern used by every
	// other CRUD handler in skill_crud_endpoints.go.
	wikiWorker := b.wikiWorker
	wikiPath := skillWikiPath(sk.Name)
	skSnapshot := sk
	b.mu.Unlock()
	unlocked = true

	commitMsg := fmt.Sprintf("wuphf: %s skill %s", action, skSnapshot.Name)
	// Use the broker lifecycle context, not r.Context(): the skill is
	// already in broker state, and a client disconnect mid-Enqueue would
	// cancel the SKILL.md commit and recreate the very "broker has it,
	// disk doesn't" condition this PR exists to fix.
	enqueueCtx := b.brokerLifecycleContext()
	if err := enqueueSkillWikiWrite(enqueueCtx, wikiWorker, skSnapshot, wikiPath, commitMsg); err != nil {
		// Wiki enqueue failures are logged but do not fail the create — the
		// skill still lives in broker state, and a later save (edit, archive,
		// or boot backfill) reconciles disk. Returning 500 here would leave
		// callers thinking the skill was not created when broker state has
		// already been mutated.
		slog.Warn("handlePostSkill: wiki enqueue failed",
			"name", skSnapshot.Name, "action", action, "err", err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skill": skSnapshot})
}

// enqueueSkillWikiWrite renders sk into SKILL.md form (with safety scan
// metadata) and enqueues a wiki write through the worker. Caller must NOT
// hold b.mu — WikiWorker.Enqueue acquires b.mu via PublishWikiEvent and would
// deadlock. Returns nil silently when worker is nil (wiki backend offline)
// or when sk lacks the mandatory frontmatter fields.
func enqueueSkillWikiWrite(ctx context.Context, worker *WikiWorker, sk teamSkill, wikiPath, commitMsg string) error {
	if worker == nil {
		return nil
	}
	if strings.TrimSpace(sk.Name) == "" || strings.TrimSpace(sk.Description) == "" {
		// Intentionally retained even though handlePostSkill now rejects empty
		// description at the HTTP layer: backfillSkillFilesFromState (and any
		// future caller) may encounter legacy teamSkill records written before
		// the validation existed. Removing this guard would cause those legacy
		// skills to fail RenderSkillMarkdown and surface a write error instead
		// of a safe, logged skip.
		slog.Warn("enqueueSkillWikiWrite: skipping wiki write — name or description empty",
			"name", sk.Name)
		return nil
	}
	fm := teamSkillToFrontmatter(sk)
	scan := ScanSkill(fm, sk.Content, skillTrustForCreator(sk.CreatedBy))
	fm.Metadata.Wuphf.SafetyScan = &SkillSafetyScan{
		Verdict:    string(scan.Verdict),
		Findings:   append([]string(nil), scan.Findings...),
		TrustLevel: string(scan.TrustLevel),
		Summary:    scan.Summary,
	}
	mdBytes, err := RenderSkillMarkdown(fm, sk.Content)
	if err != nil {
		return fmt.Errorf("render skill markdown: %w", err)
	}
	if _, _, err := worker.Enqueue(ctx, sk.Name, wikiPath, string(mdBytes), "replace", commitMsg); err != nil {
		return fmt.Errorf("wiki enqueue: %w", err)
	}
	return nil
}

func (b *Broker) handlePutSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                string   `json:"name"`
		Title               string   `json:"title"`
		Description         string   `json:"description"`
		Content             string   `json:"content"`
		Channel             string   `json:"channel"`
		Tags                []string `json:"tags"`
		Trigger             string   `json:"trigger"`
		Status              string   `json:"status"`
		WorkflowProvider    string   `json:"workflow_provider"`
		WorkflowKey         string   `json:"workflow_key"`
		WorkflowDefinition  string   `json:"workflow_definition"`
		WorkflowSchedule    string   `json:"workflow_schedule"`
		RelayID             string   `json:"relay_id"`
		RelayPlatform       string   `json:"relay_platform"`
		RelayEventTypes     []string `json:"relay_event_types"`
		LastExecutionAt     string   `json:"last_execution_at"`
		LastExecutionStatus string   `json:"last_execution_status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" && strings.TrimSpace(body.WorkflowKey) == "" {
		http.Error(w, "name or workflow_key required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	defer b.mu.Unlock()

	sk := b.findSkillByNameLocked(body.Name)
	if sk == nil {
		sk = b.findSkillByWorkflowKeyLocked(body.WorkflowKey)
	}
	if sk == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	if t := strings.TrimSpace(body.Title); t != "" {
		sk.Title = t
	}
	if d := strings.TrimSpace(body.Description); d != "" {
		sk.Description = d
	}
	if c := strings.TrimSpace(body.Content); c != "" {
		sk.Content = c
	}
	if ch := normalizeChannelSlug(body.Channel); ch != "" {
		sk.Channel = ch
	}
	if body.Tags != nil {
		sk.Tags = body.Tags
	}
	if t := strings.TrimSpace(body.Trigger); t != "" {
		sk.Trigger = t
	}
	if p := strings.TrimSpace(body.WorkflowProvider); p != "" {
		sk.WorkflowProvider = p
	}
	if key := strings.TrimSpace(body.WorkflowKey); key != "" {
		sk.WorkflowKey = key
	}
	if def := strings.TrimSpace(body.WorkflowDefinition); def != "" {
		sk.WorkflowDefinition = def
	}
	if sched := strings.TrimSpace(body.WorkflowSchedule); sched != "" {
		sk.WorkflowSchedule = sched
	}
	if relayID := strings.TrimSpace(body.RelayID); relayID != "" {
		sk.RelayID = relayID
	}
	if relayPlatform := strings.TrimSpace(body.RelayPlatform); relayPlatform != "" {
		sk.RelayPlatform = relayPlatform
	}
	if body.RelayEventTypes != nil {
		sk.RelayEventTypes = append([]string(nil), body.RelayEventTypes...)
	}
	if ts := strings.TrimSpace(body.LastExecutionAt); ts != "" {
		sk.LastExecutionAt = ts
	}
	if status := strings.TrimSpace(body.LastExecutionStatus); status != "" {
		sk.LastExecutionStatus = status
	}
	if s := strings.TrimSpace(body.Status); s != "" {
		// Don't let PUT /skills smuggle a "proposed" skill into "active"
		// without going through the human-approval flow that
		// handlePostSkill action="propose" creates. Approval lives in
		// handlePostRequestAnswer (skill_proposal kind); allowing this
		// path to flip the status would let any caller bypass it.
		if sk.Status == "proposed" && s != "proposed" && s != "archived" {
			http.Error(w, "proposed skills must be approved or rejected via the request answer flow", http.StatusForbidden)
			return
		}
		sk.Status = s
	}
	sk.UpdatedAt = now

	channel := normalizeChannelSlug(sk.Channel)
	if channel == "" {
		channel = "general"
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      sk.CreatedBy,
		Channel:   channel,
		Kind:      "skill_update",
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q updated", sk.Name),
		Timestamp: now,
	})
	b.appendActionLocked("skill_update", "office", channel, sk.CreatedBy, truncateSummary(sk.Title+" [updated]", 140), sk.ID)

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skill": *sk})
}

func (b *Broker) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	defer b.mu.Unlock()

	sk := b.findSkillByNameLocked(body.Name)
	if sk == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	sk.Status = "archived"
	sk.UpdatedAt = now

	channel := normalizeChannelSlug(sk.Channel)
	if channel == "" {
		channel = "general"
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      sk.CreatedBy,
		Channel:   channel,
		Kind:      "skill_update",
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q archived", sk.Name),
		Timestamp: now,
	})
	b.appendActionLocked("skill_update", "office", channel, sk.CreatedBy, truncateSummary(sk.Title+" [archived]", 140), sk.ID)

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (b *Broker) handleInvokeSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract skill name from path: /skills/{name}/invoke
	path := strings.TrimPrefix(r.URL.Path, "/skills/")
	skillName := strings.TrimSuffix(path, "/invoke")
	if strings.TrimSpace(skillName) == "" {
		http.Error(w, "skill name required in path", http.StatusBadRequest)
		return
	}

	var body struct {
		InvokedBy string `json:"invoked_by"`
		Channel   string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	defer b.mu.Unlock()

	sk := b.findSkillByNameLocked(skillName)
	if sk == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	// Security fix (Codex T3): only active skills may be invoked. Proposed or
	// archived skills must not be executable — proposed means unapproved,
	// archived means intentionally retired.
	if sk.Status != "active" {
		http.Error(w, "skill not active (status="+sk.Status+")", http.StatusForbidden)
		return
	}

	sk.UsageCount++
	sk.UpdatedAt = now

	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = normalizeChannelSlug(sk.Channel)
	}
	if channel == "" {
		channel = "general"
	}

	invoker := strings.TrimSpace(body.InvokedBy)
	if invoker == "" {
		invoker = "you"
	}
	sk.LastExecutionAt = now
	sk.LastExecutionStatus = "invoked"
	sk.UpdatedAt = now

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      invoker,
		Channel:   channel,
		Kind:      "skill_invocation",
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q invoked by @%s (usage #%d)", sk.Name, invoker, sk.UsageCount),
		Timestamp: now,
	})
	b.appendActionLocked("skill_invocation", "office", channel, invoker, truncateSummary(sk.Title+" [invoked]", 140), sk.ID)

	// Dispatch a real task so an agent picks up and executes the skill.
	// This is best-effort: if task creation fails we log and carry on —
	// the skill_invocation message + action are already recorded.
	taskID, taskErr := b.createSkillRunTaskLocked(sk, channel, invoker, now)
	if taskErr != nil {
		log.Printf("handleInvokeSkill: createSkillRunTaskLocked failed (non-fatal): %v", taskErr)
	}

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"skill": *sk, "channel": channel}
	if taskID != "" {
		resp["task_id"] = taskID
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// createSkillRunTaskLocked dispatches a task so the office lead picks up and
// executes the skill. Caller must hold b.mu. Returns the new task ID.
func (b *Broker) createSkillRunTaskLocked(sk *teamSkill, channel, invoker, now string) (string, error) {
	owner := strings.TrimSpace(officeLeadSlugFrom(b.members))
	if owner == "" {
		owner = strings.TrimSpace(invoker)
	}
	if owner == "" {
		owner = "ceo"
	}

	title := strings.TrimSpace(sk.Title)
	if title == "" {
		title = strings.TrimSpace(sk.Name)
	}
	taskTitle := "Run skill: " + title

	header := fmt.Sprintf("Invoked by @%s on %s. Follow the steps below.\n\n", invoker, now)
	details := header + strings.TrimSpace(sk.Content)

	b.counter++
	task := teamTask{
		ID:            fmt.Sprintf("task-skill-%d", b.counter),
		Channel:       channel,
		Title:         taskTitle,
		Details:       details,
		Owner:         owner,
		Status:        "in_progress",
		CreatedBy:     invoker,
		TaskType:      "skill_run",
		PipelineID:    "skill_invocation",
		ExecutionMode: "office",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	// Run fallible setup first so a failure doesn't leave the broker
	// holding cross-cutting mutations (channel membership, scheduler
	// jobs) that the caller never sees because we returned an error.
	if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
		return "", fmt.Errorf("rejectTheaterTask: %w", err)
	}
	if err := b.syncTaskWorktreeLocked(&task); err != nil {
		return "", fmt.Errorf("syncTaskWorktree: %w", err)
	}
	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(&task)
	b.scheduleTaskLifecycleLocked(&task)
	b.tasks = append(b.tasks, task)
	b.appendActionLocked("task_created", "office", channel, invoker, truncateSummary(task.Title, 140), task.ID)
	return task.ID, nil
}

func (b *Broker) appendSkillProposalRequestLocked(skill teamSkill, channel, now string) {
	title := strings.TrimSpace(skill.Title)
	if title == "" {
		title = strings.TrimSpace(skill.Name)
	}
	description := strings.TrimSpace(skill.Description)
	createdBy := strings.TrimSpace(skill.CreatedBy)
	if createdBy == "" {
		createdBy = "system"
	}
	b.counter++
	interview := humanInterview{
		ID:        fmt.Sprintf("request-%d", b.counter),
		Kind:      "skill_proposal",
		Status:    "pending",
		From:      createdBy,
		Channel:   normalizeChannelSlug(channel),
		Title:     "Approve skill: " + title,
		Question:  fmt.Sprintf("@%s proposed skill **%s**: %s\n\nActivate it?", createdBy, title, description),
		ReplyTo:   strings.TrimSpace(skill.Name),
		Blocking:  false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	interview.Options, interview.RecommendedID = normalizeRequestOptions(interview.Kind, "accept", []interviewOption{
		{ID: "accept", Label: "Accept"},
		{ID: "reject", Label: "Reject"},
	})
	interview = sanitizeHumanInterview(interview)
	b.requests = append(b.requests, interview)
}

// SeedDefaultSkills pre-populates the broker with the given skill specs.
// It is idempotent: skills whose name already exists (by slug) are skipped.
// No production callers remain; tests use it to set up broker skill state.
func (b *Broker) SeedDefaultSkills(specs []agent.PackSkillSpec) {
	if len(specs) == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		if b.findSkillByNameLocked(name) != nil {
			continue // already exists, skip
		}
		title := strings.TrimSpace(spec.Title)
		if title == "" {
			title = name
		}
		b.counter++
		sk := teamSkill{
			ID:          b.allocateSkillIDLocked(name),
			Name:        name,
			Title:       title,
			Description: strings.TrimSpace(spec.Description),
			Content:     strings.TrimSpace(spec.Content),
			CreatedBy:   "system",
			Tags:        append([]string(nil), spec.Tags...),
			Trigger:     strings.TrimSpace(spec.Trigger),
			Status:      "active",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		b.skills = append(b.skills, sk)
	}
	if err := b.saveLocked(); err != nil {
		log.Printf("broker: saveLocked after seeding skills: %v", err)
	}
}
