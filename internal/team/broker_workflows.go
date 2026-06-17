package team

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"
)

// Workflow-detection HTTP surface (thin vertical slice). Two endpoints back the
// "Spotted Workflows" panel:
//
//	GET  /workflows/spotted  -> run the miner over the persisted manifest sink,
//	                            return candidates annotated with a proposed title
//	                            and whether they're already frozen into a skill.
//	POST /workflows/freeze   -> turn one candidate into an active teamSkill.
//
// Detection (file read) happens OUTSIDE the broker lock; only the skill
// create/lookup touches b.mu. Design: docs/specs/workflow-detection-positioning.md.

// spottedWorkflow is the wire shape the panel renders: a DetectionCandidate plus
// a human-readable title and a frozen flag.
type spottedWorkflow struct {
	DetectionCandidate
	Title  string `json:"title"`
	Frozen bool   `json:"frozen"`
}

// workflowSkillName is the stable, deterministic skill name for a candidate, so
// re-freezing the same shape updates rather than duplicates (the "update
// existing over create new" promise). fnv keeps it dependency-free and stable.
func workflowSkillName(c DetectionCandidate) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(c.Fingerprint))
	agent := strings.TrimSpace(c.Agent)
	if agent == "" {
		agent = "office"
	}
	return fmt.Sprintf("spotted-%s-%08x", agent, h.Sum32())
}

// workflowSkillTitle is a readable placeholder title derived from the shape.
// The LLM naming pass (T11) will replace this with a goal-shaped title.
func workflowSkillTitle(c DetectionCandidate) string {
	if len(c.Shape) == 0 {
		return "Spotted workflow"
	}
	return "Workflow: " + strings.Join(c.Shape, " -> ")
}

func workflowSkillContent(c DetectionCandidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", workflowSkillTitle(c))
	fmt.Fprintf(&b, "Spotted from %d repeated runs by `%s`.\n\n", c.Count, c.Agent)
	b.WriteString("## Steps\n\n")
	for i, tool := range c.Shape {
		fmt.Fprintf(&b, "%d. `%s`\n", i+1, tool)
	}
	b.WriteString("\n## Source tasks\n\n")
	for _, id := range c.TaskIDs {
		fmt.Fprintf(&b, "- %s\n", id)
	}
	return b.String()
}

func (b *Broker) handleWorkflowsSpotted(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	cands, err := DetectWorkflowsFromSink(EventSinkPath(), DetectOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "detect_failed"})
		return
	}
	b.mu.Lock()
	out := make([]spottedWorkflow, 0, len(cands))
	for _, c := range cands {
		out = append(out, spottedWorkflow{
			DetectionCandidate: c,
			Title:              workflowSkillTitle(c),
			Frozen:             b.findSkillByNameLocked(workflowSkillName(c)) != nil,
		})
	}
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"workflows": out})
}

func (b *Broker) handleWorkflowsFreeze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	fp := strings.TrimSpace(body.Fingerprint)
	if fp == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "fingerprint_required"})
		return
	}

	// Resolve the candidate from the live corpus (file read outside the lock).
	cands, err := DetectWorkflowsFromSink(EventSinkPath(), DetectOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "detect_failed"})
		return
	}
	var cand *DetectionCandidate
	for i := range cands {
		if cands[i].Fingerprint == fp {
			cand = &cands[i]
			break
		}
	}
	if cand == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no_spotted_workflow_for_fingerprint"})
		return
	}

	createdBy := "system"
	if actor, ok := requestActorFromContext(r.Context()); ok && strings.TrimSpace(actor.Slug) != "" {
		createdBy = actor.Slug
	}
	now := time.Now().UTC().Format(time.RFC3339)
	name := workflowSkillName(*cand)
	title := workflowSkillTitle(*cand)
	channel := normalizeChannelSlug("general")

	b.mu.Lock()
	// Update-over-create: re-freezing a known shape returns the existing skill.
	if existing := b.findSkillByNameLocked(name); existing != nil {
		sk := *existing
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"skill": sk, "created": false})
		return
	}
	sk := teamSkill{
		ID:          b.allocateSkillIDLocked(name),
		Name:        name,
		Title:       title,
		Description: fmt.Sprintf("Frozen from a workflow %q repeated %d times.", cand.Agent, cand.Count),
		Content:     workflowSkillContent(*cand),
		CreatedBy:   createdBy,
		OwnerAgents: b.allMemberSlugsLocked(),
		Channel:     channel,
		Tags:        []string{"workflow", "spotted"},
		Trigger:     "manual",
		Status:      "active",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	b.skills = append(b.skills, sk)
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "persist_failed"})
		return
	}
	b.mu.Unlock()

	// Announce in-channel (PostMessage takes its own lock; best-effort).
	_, _ = b.PostMessage("system", channel,
		fmt.Sprintf("Created workflow %q from a pattern repeated %d times.", title, cand.Count),
		nil, "")

	writeJSON(w, http.StatusOK, map[string]any{"skill": sk, "created": true})
}
