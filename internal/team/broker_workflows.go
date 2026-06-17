package team

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/workflow"
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

// workflowSpecPath is where a frozen workflow's contract is persisted.
func workflowSpecPath(id string) string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "office", "workflows", id+".workflow-spec.json")
}

// bindingSkillContent renders the teamSkill body for a frozen workflow. The
// skill is now the RUNTIME BINDING to a proven spec, not a prose skill: it
// points at the contract and records the shipcheck verdict + steps.
func bindingSkillContent(spec workflow.Spec, rep workflow.ShipcheckReport, specPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", spec.Goal)
	fmt.Fprintf(&b, "Runtime binding for workflow-spec `%s`.\n\n", spec.ID)
	if specPath != "" {
		fmt.Fprintf(&b, "Contract: `%s`\n\n", specPath)
	}
	b.WriteString("## Shipcheck\n\n")
	for _, c := range rep.Checks {
		mark := "FAIL"
		if c.Pass {
			mark = "OK"
		}
		fmt.Fprintf(&b, "- [%s] %s — %s\n", mark, c.Name, c.Detail)
	}
	b.WriteString("\n## Steps\n\n")
	for i, a := range spec.Actions {
		fmt.Fprintf(&b, "%d. `%s` (%s)\n", i+1, a.ID, a.Kind)
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

// handleWorkflowsDraft returns the drafted contract + shipcheck for a candidate
// WITHOUT persisting anything — the review/enrich preview the operator edits
// before confirming. Freeze then binds the (possibly edited) spec.
func (b *Broker) handleWorkflowsDraft(w http.ResponseWriter, r *http.Request) {
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
	spec := workflow.DraftSpec(workflowSkillName(*cand), workflowSkillTitle(*cand), cand.Agent, cand.Shape)
	report := workflow.Shipcheck(&spec)
	writeJSON(w, http.StatusOK, map[string]any{"spec": spec, "shipcheck": report})
}

func (b *Broker) handleWorkflowsFreeze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		Fingerprint string         `json:"fingerprint"`
		Spec        *workflow.Spec `json:"spec,omitempty"`
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

	// Discovery -> contract. Either bind the operator's reviewed/enriched spec
	// (when supplied) or auto-draft from the detected shape. Either way the
	// contract is PROVEN with shipcheck before anything is created; one that
	// fails the mechanical proof never ships.
	var spec workflow.Spec
	if body.Spec != nil {
		spec = *body.Spec
		spec.ID = name // always bind to this candidate, regardless of edits
		if strings.TrimSpace(spec.Goal) == "" {
			spec.Goal = title
		}
		if strings.TrimSpace(spec.Operator) == "" {
			spec.Operator = cand.Agent
		}
	} else {
		spec = workflow.DraftSpec(name, title, cand.Agent, cand.Shape)
	}
	report := workflow.Shipcheck(&spec)
	if !report.Passed {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "shipcheck_failed", "shipcheck": report,
		})
		return
	}
	specPath := workflowSpecPath(name)
	if specPath != "" {
		if err := workflow.SaveSpec(specPath, &spec); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "spec_persist_failed"})
			return
		}
	}

	b.mu.Lock()
	// Update-over-create: re-freezing a known shape returns the existing binding.
	if existing := b.findSkillByNameLocked(name); existing != nil {
		sk := *existing
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"skill": sk, "spec_id": spec.ID, "shipcheck": report, "created": false})
		return
	}
	// The teamSkill is now the RUNTIME BINDING to the proven spec, not a prose
	// skill: it points at the contract and records the shipcheck verdict.
	sk := teamSkill{
		ID:          b.allocateSkillIDLocked(name),
		Name:        name,
		Title:       title,
		Description: fmt.Sprintf("Workflow contract for a pattern by %q repeated %d times (shipcheck passed).", cand.Agent, cand.Count),
		Content:     bindingSkillContent(spec, report, specPath),
		CreatedBy:   createdBy,
		OwnerAgents: b.allMemberSlugsLocked(),
		Channel:     channel,
		Tags:        []string{"workflow", "spotted"},
		Trigger:     "manual",
		WorkflowKey: spec.ID,
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
		fmt.Sprintf("Created workflow %q from a pattern repeated %d times. Shipcheck passed (%d checks).", title, cand.Count, len(report.Checks)),
		nil, "")

	writeJSON(w, http.StatusOK, map[string]any{"skill": sk, "spec_id": spec.ID, "shipcheck": report, "created": true})
}

// handleWorkflowsImprove is stage 5: apply an improvement overlay to a stored
// contract. The overlay is reviewed (patched spec must shipcheck and every
// original scenario must still replay); on accept the contract is updated in
// place (update-over-create, version bumped) and the binding skill refreshed.
func (b *Broker) handleWorkflowsImprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		SpecID  string           `json:"spec_id"`
		Overlay workflow.Overlay `json:"overlay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	id := strings.TrimSpace(body.SpecID)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	path := workflowSpecPath(id)
	if path == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "no_runtime_home"})
		return
	}
	spec, err := workflow.LoadSpec(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "spec_not_found"})
		return
	}

	patched, review, err := workflow.AcceptOverlay(spec, body.Overlay)
	if err != nil {
		// Rejected: the contract is unchanged. Return why.
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "overlay_rejected", "review": review})
		return
	}
	if err := workflow.SaveSpec(path, patched); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "spec_persist_failed"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	rep := workflow.Shipcheck(patched)
	b.mu.Lock()
	if sk := b.findSkillByNameLocked(id); sk != nil {
		sk.Content = bindingSkillContent(*patched, rep, path)
		sk.UpdatedAt = now
	}
	_ = b.saveLocked()
	b.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"spec_id": id, "version": patched.Version, "review": review})
}
