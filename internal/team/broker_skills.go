package team

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// listSkillsOpts controls listSkillsForAgentLocked filtering.
type listSkillsOpts struct {
	// activeOnly drops anything whose Status is not "active". When false, every
	// non-tombstoned skill is returned (including proposed/archived/disabled).
	activeOnly bool
}

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
	// PR 7 Lane C: /skills/list returns scope-aware skill metadata for cross-role
	// discovery. Routed BEFORE the /invoke and CRUD branches so the literal
	// "list" segment is not interpreted as a skill name.
	if path == "list" {
		b.handleListSkills(w, r)
		return
	}
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

// canAgentSeeSkillLocked is the canonical visibility predicate for per-agent
// skill scoping (PR 7). The caller MUST hold b.mu.
//
// An agent sees a skill when either:
//   - the agent slug (case-insensitive, trimmed) appears in sk.OwnerAgents, OR
//   - sk.OwnerAgents is empty AND the agent slug equals the office lead slug
//     (the lead-routable, shared-skill default — back-compat with skills that
//     pre-date the OwnerAgents field).
//
// Status is intentionally ignored here so the predicate stays orthogonal to
// active/disabled/archived filtering. Callers that want only active skills
// pass listSkillsOpts{activeOnly: true} to listSkillsForAgentLocked, or apply
// their own status check after the visibility gate.
func (b *Broker) canAgentSeeSkillLocked(slug string, sk *teamSkill) bool {
	if sk == nil {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(slug))
	if want == "" {
		return false
	}
	if len(sk.OwnerAgents) == 0 {
		lead := strings.ToLower(strings.TrimSpace(officeLeadSlugFrom(b.members)))
		return lead != "" && lead == want
	}
	for _, owner := range sk.OwnerAgents {
		if strings.ToLower(strings.TrimSpace(owner)) == want {
			return true
		}
	}
	return false
}

// listSkillsForAgentLocked returns the subset of b.skills visible to slug,
// sorted by Name lexicographically so catalog injection in buildPrompt produces
// byte-identical output across calls (cache stability). The caller MUST hold
// b.mu.
func (b *Broker) listSkillsForAgentLocked(slug string, opts listSkillsOpts) []teamSkill {
	out := make([]teamSkill, 0, len(b.skills))
	for i := range b.skills {
		sk := &b.skills[i]
		if opts.activeOnly && sk.Status != "active" {
			continue
		}
		if !b.canAgentSeeSkillLocked(slug, sk) {
			continue
		}
		out = append(out, *sk)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (b *Broker) handleGetSkills(w http.ResponseWriter, r *http.Request) {
	channelFilter := normalizeChannelSlug(r.URL.Query().Get("channel"))
	// PR 7 Lane C (F6 fold-in) + /review P1: when ?for_agent=<slug> is present,
	// scope the response to skills that agent can see AND strip skill body
	// content (broker has no first-class identity verification under the
	// shared-bearer-token model). Absent param preserves the existing
	// humans/UI-facing unfiltered view.
	forAgent := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("for_agent")))
	// PR 7 follow-up: opt-in flags so the Skills app can request the full
	// catalog (proposed + active + disabled + archived) without breaking
	// older consumers that expect the active+proposed+disabled subset.
	// include_disabled is currently a no-op — disabled skills already pass
	// through this handler — but it's wired now so a future filter change
	// has a stable opt-in surface.
	includeArchived := truthyQuery(r.URL.Query().Get("include_archived"))
	_ = truthyQuery(r.URL.Query().Get("include_disabled"))

	b.mu.Lock()
	result := make([]teamSkill, 0, len(b.skills))
	for i := range b.skills {
		sk := b.skills[i]
		if sk.Status == "archived" && !includeArchived {
			continue
		}
		if channelFilter != "" && normalizeChannelSlug(sk.Channel) != channelFilter {
			continue
		}
		if forAgent != "" {
			if !b.canAgentSeeSkillLocked(forAgent, &b.skills[i]) {
				continue
			}
			// Strip the body so the metadata-listing endpoint never serves
			// another agent's playbook content. Callers that need the body
			// hit the per-skill endpoint with explicit identity.
			sk.Content = ""
		}
		result = append(result, sk)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skills": result})
}

// handleListSkills serves GET /skills/list?scope=own|all&for_agent={slug}&tag={x}.
//
// scope=own (default): returns only skills the agent can see (visibility predicate),
// active-only, with full payload including Content. Token discipline is left to
// the agent's catalog injection (see Lane A step 5).
//
// scope=all: returns every active skill with Content stripped. The metadata
// (name, description, trigger, owner_agents, tags) lets agents discover what
// other roles can do without leaking the playbook bodies.
//
// tag filter is applied on top of either scope when present.
func (b *Broker) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if scope == "" {
		scope = "own"
	}
	if scope != "own" && scope != "all" {
		http.Error(w, "scope must be own or all", http.StatusBadRequest)
		return
	}
	tag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag")))
	forAgent := strings.TrimSpace(r.URL.Query().Get("for_agent"))

	b.mu.Lock()
	var collected []teamSkill
	switch scope {
	case "own":
		if forAgent == "" {
			b.mu.Unlock()
			http.Error(w, "for_agent is required for scope=own", http.StatusBadRequest)
			return
		}
		collected = b.listSkillsForAgentLocked(forAgent, listSkillsOpts{activeOnly: true})
	case "all":
		for i := range b.skills {
			if b.skills[i].Status != "active" {
				continue
			}
			collected = append(collected, b.skills[i])
		}
		sort.Slice(collected, func(i, j int) bool {
			return collected[i].Name < collected[j].Name
		})
	}
	b.mu.Unlock()

	result := make([]teamSkill, 0, len(collected))
	for _, sk := range collected {
		if tag != "" && !skillHasTag(sk, tag) {
			continue
		}
		if scope == "all" {
			// Privacy + token discipline: cross-role discovery returns metadata
			// only — no playbook body.
			sk.Content = ""
		}
		result = append(result, sk)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"skills": result,
		"scope":  scope,
	})
}

// skillHasTag reports whether sk carries the given (already lowercased+trimmed) tag.
func skillHasTag(sk teamSkill, tag string) bool {
	for _, t := range sk.Tags {
		if strings.ToLower(strings.TrimSpace(t)) == tag {
			return true
		}
	}
	return false
}

// truthyQuery accepts any of "1", "true", "yes" (case-insensitive) as true.
// Anything else, including empty, is false.
func truthyQuery(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	}
	return false
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
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Content) == "" || strings.TrimSpace(body.CreatedBy) == "" {
		http.Error(w, "name, content, and created_by required", http.StatusBadRequest)
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
	defer b.mu.Unlock()

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
		b.appendSkillProposalRequestLocked(sk, channel, now, "")
	}

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skill": sk})
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
	// archived means intentionally retired. Disabled (PR 7 step 4) is a
	// reversible pause and gets a structured 403 so callers can distinguish
	// it from the unrecoverable "not active" case.
	if sk.Status == "disabled" {
		writeSkillForbidden(w, "disabled", sk, b.members,
			"skill is disabled — re-enable it from the Skills app to invoke")
		return
	}
	if sk.Status != "active" {
		http.Error(w, "skill not active (status="+sk.Status+")", http.StatusForbidden)
		return
	}

	// PR 7: per-agent visibility gate. The invoker must be in sk.OwnerAgents,
	// or sk.OwnerAgents must be empty AND the invoker must be the office lead
	// (lead-routable shared skills). Returning a structured body lets the LLM
	// caller delegate the request via team_broadcast instead of guessing.
	if !b.canAgentSeeSkillLocked(strings.TrimSpace(body.InvokedBy), sk) {
		writeSkillForbidden(w, "not_owner", sk, b.members,
			"team_broadcast tag the listed agents or hand off the task")
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

func (b *Broker) appendSkillProposalRequestLocked(skill teamSkill, channel, now string, enhancesSlug string) {
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
	enhancesSlug = strings.TrimSpace(enhancesSlug)
	kind := "skill_proposal"
	titleLine := "Approve skill: " + title
	question := fmt.Sprintf("@%s proposed skill **%s**: %s\n\nActivate it?", createdBy, title, description)
	if enhancesSlug != "" {
		kind = "enhance_skill_proposal"
		titleLine = fmt.Sprintf("Enhance %q with new content", enhancesSlug)
		question = fmt.Sprintf("@%s drafted **%s**, but it looks similar to existing skill **%s**.\n\n%s\n\nFold this into %s, or create %s as a separate skill?",
			createdBy, title, enhancesSlug, description, enhancesSlug, title)
	}
	interview := humanInterview{
		ID:        fmt.Sprintf("request-%d", b.counter),
		Kind:      kind,
		Status:    "pending",
		From:      createdBy,
		Channel:   normalizeChannelSlug(channel),
		Title:     titleLine,
		Question:  question,
		ReplyTo:   strings.TrimSpace(skill.Name),
		Blocking:  false,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Three-option contract for enhance interviews; two-option for the
	// legacy skill_proposal kind (PR 7 task #15). The recommended action
	// for an enhance proposal is "enhance" — the gate's whole point is to
	// nudge the human away from creating a near-duplicate.
	if enhancesSlug != "" {
		interview.Options, interview.RecommendedID = normalizeRequestOptions(interview.Kind, "enhance", []interviewOption{
			{ID: "enhance", Label: "Enhance existing", Description: fmt.Sprintf("Fold this into %s. The candidate is dropped.", enhancesSlug)},
			{ID: "approve_anyway", Label: "Approve anyway", Description: "Bypass the similarity gate and create a new skill."},
			{ID: "reject", Label: "Reject", Description: "Drop this draft. The existing skill stays unchanged."},
		})
		// Stash the structured metadata + the candidate spec so the answer
		// handler can replay the candidate through writeSkillProposalLocked
		// when the human picks "approve_anyway".
		if interview.Metadata == nil {
			interview.Metadata = make(map[string]any, 1)
		}
		interview.Metadata["enhances_slug"] = enhancesSlug
		candidate := skill
		interview.EnhanceCandidate = &candidate
	} else {
		interview.Options, interview.RecommendedID = normalizeRequestOptions(interview.Kind, "accept", []interviewOption{
			{ID: "accept", Label: "Accept"},
			{ID: "reject", Label: "Reject"},
		})
		// For ambiguous-band proposals the candidate WAS written and lands
		// in b.skills with SimilarToExisting populated. Mirror that ref into
		// the interview Metadata so the UI doesn't have to walk the skill
		// list to render the "looks similar to <existing>" banner.
		if skill.SimilarToExisting != nil {
			if interview.Metadata == nil {
				interview.Metadata = make(map[string]any, 1)
			}
			interview.Metadata["similar_to_existing"] = *skill.SimilarToExisting
		}
	}
	b.requests = append(b.requests, interview)
}

// SeedDefaultSkills pre-populates the broker with the pack's default skills.
// It is idempotent: skills whose name already exists (by slug) are skipped.
// Call this after broker.Start() from the Launcher so that the first time a
// pack is launched the team has its playbooks ready to reference.
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
			OwnerAgents: append([]string(nil), spec.OwnerAgents...),
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

// writeSkillForbidden emits the structured 403 body that handleInvokeSkill
// returns when a skill is disabled or invisible to the caller. The body
// shape lets LLM clients route the request elsewhere instead of retrying.
//
//	{
//	  "ok": false,
//	  "error": "<reason>",          // "not_owner" | "disabled"
//	  "delegate_to": ["agent-slug"], // sk.OwnerAgents, or [leadSlug] if empty
//	  "hint": "<human-readable next step>"
//	}
func writeSkillForbidden(w http.ResponseWriter, reason string, sk *teamSkill, members []officeMember, hint string) {
	delegateTo := append([]string(nil), sk.OwnerAgents...)
	if len(delegateTo) == 0 {
		if lead := strings.TrimSpace(officeLeadSlugFrom(members)); lead != "" {
			delegateTo = []string{lead}
		} else {
			delegateTo = []string{}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          false,
		"error":       reason,
		"delegate_to": delegateTo,
		"hint":        hint,
	})
}

// drainSkillProposalRequestsLocked marks every pending skill_proposal interview
// whose ReplyTo matches skillName as answered. Use after a skill is approved /
// rejected via the direct endpoints (handleSkillApprove / handleSkillReject)
// so the matching interview-queue entries don't pile up as orphans (D8).
//
// Caller must hold b.mu. Returns the number of requests drained.
func (b *Broker) drainSkillProposalRequestsLocked(skillName, choiceID, now string) int {
	target := skillSlug(skillName)
	if target == "" {
		return 0
	}
	count := 0
	for i := range b.requests {
		r := &b.requests[i]
		if r.Kind != "skill_proposal" || r.Status != "pending" {
			continue
		}
		if skillSlug(r.ReplyTo) != target {
			continue
		}
		r.Status = "answered"
		r.UpdatedAt = now
		r.Answered = &interviewAnswer{
			ChoiceID:   choiceID,
			AnsweredAt: now,
		}
		r.ReminderAt = ""
		r.FollowUpAt = ""
		r.RecheckAt = ""
		r.DueAt = ""
		count++
	}
	if count > 0 {
		b.pendingInterview = firstBlockingRequest(b.requests)
	}
	return count
}
