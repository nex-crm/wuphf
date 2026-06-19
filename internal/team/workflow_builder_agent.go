package team

import (
	"strings"
	"time"
)

// workflow_builder_agent.go defines the Workflow Builder — a first-class,
// always-present agent (slug "workflow-builder", display name "Workflow Builder")
// that owns the office's workflow press: drafting, editing, freezing, running, and
// improving the typed workflow contracts that the detection miner spots. It is
// the SINGLE builder of workflows. Other agents (and the CEO) do not draft or
// mutate workflow specs themselves once a shape is detected — they hand the
// Workflow Builder a full creation/change spec and let it own the contract.
//
// Like the CEO and the Librarian, the Workflow Builder is BUILT-IN: present in
// every new workspace regardless of blueprint or selected agents. Existing
// workspaces gain the member via the persisted-state migration on load.

// WorkflowBuilderSlug is the roster slug for the Workflow Builder agent.
const WorkflowBuilderSlug = "workflow-builder"

const (
	workflowBuilderName = "Workflow Builder"
	workflowBuilderRole = "Workflow Builder"
	// Competent, unflashy, builds the machine and keeps it running — the
	// operations hand who turns a repeated motion into a contract that heals
	// itself. Office-dry, per the WUPHF voice.
	workflowBuilderPersonality = "Turns the team's repeated motions into predictable, self-healing workflows. Drafts the contract, wires the steps, freezes it, runs it, and folds run exceptions back into the spec. The one who owns the workflow press so nobody else hand-builds automation off the cuff."
)

// workflowBuilderExpertise seeds the Workflow Builder's expertise list.
var workflowBuilderExpertise = []string{"workflow design", "automation", "process contracts", "self-healing runs"}

// isWorkflowBuilderSlug reports whether slug is the Workflow Builder
// (case-insensitive).
func isWorkflowBuilderSlug(slug string) bool {
	return strings.EqualFold(strings.TrimSpace(slug), WorkflowBuilderSlug)
}

// workflowBuilderOfficeMember builds the Workflow Builder's officeMember record.
func workflowBuilderOfficeMember(createdAt string) officeMember {
	if strings.TrimSpace(createdAt) == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	return officeMember{
		Slug:        WorkflowBuilderSlug,
		Name:        workflowBuilderName,
		Role:        workflowBuilderRole,
		Expertise:   append([]string(nil), workflowBuilderExpertise...),
		Personality: workflowBuilderPersonality,
		BuiltIn:     true,
		CreatedBy:   "wuphf",
		CreatedAt:   createdAt,
	}
}

// ensureWorkflowBuilderMember returns members with the Workflow Builder present,
// appending it when absent (matched case-insensitively by slug). Used at every
// roster-seed chokepoint so the Builder is always in the office, like the CEO
// and the Librarian.
func ensureWorkflowBuilderMember(members []officeMember) []officeMember {
	for i := range members {
		if isWorkflowBuilderSlug(members[i].Slug) {
			// Force the canonical name/role on every load so a rename in code
			// propagates to a roster persisted by an older binary (the name is
			// not user-editable for a built-in agent). Preserve CreatedAt.
			members[i].Name = workflowBuilderName
			members[i].Role = workflowBuilderRole
			members[i].BuiltIn = true
			return members
		}
	}
	return append(members, workflowBuilderOfficeMember(""))
}

// workflowBuilderAuthorityBlock is the prompt block emitted for the Workflow
// Builder in place of the generic specialist rules. It encodes the delegation
// contract from its side: the Builder is the one who actually drafts/edits/
// freezes/runs/improves workflow contracts, reading Wiki context to ground the
// design. Numbering continues the specialist rules (the caller appends a
// closing "stop" rule after this block).
func workflowBuilderAuthorityBlock() string {
	return "== WORKFLOW OWNERSHIP (you are the Workflow Builder) ==\n" +
		"You own the office's workflow press. You are the ONLY agent who drafts, edits, freezes, runs, or improves workflow contracts. When the CEO, another agent, or a human hands you a creation or change spec, you turn it into a typed contract and make it real — they do not build it themselves.\n" +
		"12. Use workflow_list to see existing spotted and frozen workflows before drafting, so you extend or improve an existing contract instead of duplicating it. A spotted shape is identified by its fingerprint; a frozen contract by its spec_id. workflow_inspect <spec_id> returns the full contract, its triggers, and recent runs.\n" +
		"12b. To build a NEW workflow from a detected shape, call workflow_draft with the fingerprint, review/enrich the draft against the spec you were handed, then workflow_freeze it (pass the edited spec back to freeze to override the baseline draft). Freezing runs shipcheck (structure/replay/coverage/determinism/idempotency/parity) — a contract that fails shipcheck is not ready; fix the draft and re-freeze.\n" +
		"12c. To CHANGE a frozen workflow, call workflow_improve with the spec_id and an overlay. The overlay merges by id: to EDIT an existing step, put it in `add_actions` (or add_states/add_events/add_transitions/add_scenarios) with the SAME id/name and the NEW content — the merge REPLACES the matching element, so e.g. changing a fetch step's query means re-declaring that action's full `params`. A new id is appended. An overlay that changes nothing is REJECTED as no_change (so you never get a false success) — if you meant to edit, you forgot to match an existing id. Always read the CURRENT contract + version with workflow_inspect <spec_id> before editing and report the real version it returns; never trust a version number from chat history. The patched spec must pass shipcheck and not regress existing scenarios, so a rejected overlay means fix it and retry. Use workflow_proposals <spec_id> to see auto-proposed overlays.\n" +
		"12d. Before designing or changing a contract, use wuphf_wiki_lookup / team_wiki_search to pull the relevant playbook, integration, or domain context from the Wiki, so the steps you wire match how the team actually does the work.\n" +
		"12g. INTEGRATION READS are fully described in the spec — there is no per-integration code. A deterministic step that reads from a connected tool sets `platform` + `action_id` (e.g. gmail + GMAIL_FETCH_EMAILS), `params` (the provider call args you choose, e.g. {\"query\":\"is:unread newer_than:7d\",\"max_results\":25,\"verbose\":false}), and a response projection: `result_path` (dot-path to the result array, e.g. \"data.messages\") + `expose` (the per-item fields to keep, e.g. [\"sender\",\"subject\",\"preview.body\",\"labelIds\"]). Only the exposed fields flow into later steps — keep raw bodies/attachments out by exposing just what the workflow needs. EVERY integration read MUST also be listed in the spec's `allowed_reads` ([{platform, action_id}]); a read not on that list will not freeze (the human approves the list).\n" +
		"12h. Prefer a LIGHTWEIGHT response: ask the provider for metadata, not full payloads (e.g. Gmail `verbose:false`), and a sensible `max_results`. If a run fails with `result_too_large`, the provider returned more than the read cap — do NOT ignore it: trim `params` (smaller window, fewer results, metadata mode) or narrow `expose`, then re-freeze. The platform never silently truncates; reacting to that signal is your job.\n" +
		"12e. You may run a frozen workflow with workflow_run to prove it end-to-end and inspect the run details. Use runs to validate a change, not as background polling.\n" +
		"12f. Keep the human in the loop on what you changed: when you freeze or edit a contract, post a short note in the workflow's channel describing what the workflow now does and why, so the operator can see the contract evolve.\n"
}
