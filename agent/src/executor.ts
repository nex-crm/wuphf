// Deterministic executor: run a compiled WorkflowSpec step by step.
//
// The DETERMINISTIC half of the spine — it runs the compiled spec, it does not
// reason. S-now simulates each step (no live integrations yet); the discovery
// slices replace the per-step simulation with real API-first replay -> UI replay
// -> bounded CUA-heal while keeping this control flow: a `gated` step (external
// mutation) HALTS with status="needs_approval" and the pending step surfaced to
// the human approval card (CQ1). Approve -> the FE re-runs with that id in
// input.approved.

import type { RunResult, RunStep, WorkflowSpec } from "./wire.js";

export function runWorkflow(spec: WorkflowSpec, input: Record<string, unknown> = {}): RunResult {
	const approved = new Set<string>(Array.isArray(input.approved) ? (input.approved as unknown[]).map(String) : []);
	const steps: RunStep[] = [];
	for (const step of spec.steps) {
		if (step.gated && !approved.has(step.id)) {
			steps.push({
				step_id: step.id,
				status: "awaiting_approval",
				detail: `${step.title} mutates ${step.integration ?? "an external system"} — needs approval.`,
			});
			return {
				status: "needs_approval",
				steps,
				digest: `Paused at ${step.title}: external mutation needs the human approval card.`,
				pending_approval: { step_id: step.id, title: step.title, integration: step.integration, detail: step.detail },
			};
		}
		steps.push({ step_id: step.id, status: "ok", detail: `${step.title} ran.` });
	}
	return { status: "done", steps, digest: `Ran ${steps.length} steps to completion.`, pending_approval: null };
}
