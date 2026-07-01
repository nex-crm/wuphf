// FE <-> agent contract. Mirrors web/src/operator/mock/data.ts (WorkflowStep) and
// the Python harness wire.py, so the operator FE and either backend speak the same
// WorkflowSpec. Keep these shapes in sync across the three.

export type WorkflowStepKind = "trigger" | "enrich" | "ai" | "decision" | "action" | "branch";
const STEP_KINDS: readonly WorkflowStepKind[] = ["trigger", "enrich", "ai", "decision", "action", "branch"];

// An executable API call a step replays deterministically (the EXECUTE half). Built
// by discovery (browsersniff: HAR -> ApiCall) and replayed by the executor. auth_ref
// is a NAMED credential reference, never a secret value (operator-mlp A3/A4) — the
// executor resolves it from the credential store at run time.
export interface ApiCall {
	method: string;
	url: string;
	query?: Record<string, string>;
	headers?: Record<string, string>;
	body?: unknown;
	auth_ref?: string;
}

export interface WorkflowStep {
	id: string;
	kind: WorkflowStepKind;
	title: string;
	detail: string;
	integration?: string;
	gated?: boolean; // external mutation -> human approval card (CQ1)
	api?: ApiCall; // present -> the executor replays a real call; absent -> simulated
}

export interface ClarifyQuestion {
	field: "threshold" | "channel";
	prompt: string;
	step_id: string;
}

export interface WorkflowSpec {
	name: string;
	tool_id: string;
	steps: WorkflowStep[];
	narration: string;
	clarify: ClarifyQuestion | null;
}

export const SCHEMA_VERSION = 1;

export interface BuildRequest {
	schema_version?: number;
	message: string;
	tool_id?: string;
}

export interface RunRequest {
	schema_version?: number;
	spec: WorkflowSpec;
	input?: Record<string, unknown>;
}

export interface RunStep {
	step_id: string;
	status: "ok" | "skipped" | "awaiting_approval" | "error";
	detail: string;
	http_status?: number; // present for replayed API steps
}

export interface ToolInput {
	name: string;
	type?: "string" | "number" | "record";
}

// A callable capability the chat agent authored for an app — a workflow the
// operator taught it, saved so the agent can call it by `name` later. Mirrors the
// FE (web/src/operator/tools/mockTools.ts). These are AGENT tools: the app's chat
// calls them; a human does not run them by hand.
export interface Tool {
	name: string; // callable id, e.g. "scoreAndRouteLead"
	title: string; // plain-language, e.g. "Score & route a lead"
	purpose: string; // one line: what running it does
	inputs: ToolInput[];
	code: string; // the (agent-written) implementation
}

export interface ToolBuildRequest {
	schema_version?: number;
	message: string; // the operator's plain-language task
	app?: string; // the app the tool is for (copy only)
}

export interface ToolBuildResult {
	tool: Tool | null; // the tool the agent made this turn (if it made one)
	narration: string; // the agent's reflect-back line
}

export interface RunResult {
	status: "done" | "needs_approval" | "error";
	steps: RunStep[];
	digest: string;
	pending_approval: { step_id: string; title: string; integration?: string; detail: string } | null;
}

// The BUILD brief: identical intent to the Python harness so any engine produces
// the same shape. The agent must output ONLY this JSON object.
export const SCHEMA_PROMPT = `You are the BUILD agent for an operator tool-builder. The operator describes an internal workflow. FIGURE OUT a small deterministic pipeline and OUTPUT ONLY a single JSON object (no prose, no code fence) of this shape:

{"name": str, "tool_id": str, "narration": str,
 "steps": [{"id": str, "kind": "trigger|enrich|ai|decision|action|branch", "title": str, "detail": str, "integration": str|null, "gated": bool}],
 "clarify": {"field": "threshold|channel", "prompt": str, "step_id": str} | null}

Rules: 3-6 steps; any step that mutates an external system MUST have gated=true and an integration; at most one clarify question (only if you truly cannot proceed); tool_id is a slug. Output the JSON object and nothing else.`;

/** Pull the first balanced JSON object out of model output (tolerates code fences / preamble). */
export function extractJson(text: string): Record<string, unknown> {
	let start = text.indexOf("{");
	while (start !== -1) {
		let depth = 0;
		let inStr = false;
		let esc = false;
		for (let i = start; i < text.length; i++) {
			const c = text[i];
			if (inStr) {
				// Resolve an escape FIRST: a backslash-escaped char (e.g. \" inside
				// a string) must not be read as a string terminator.
				if (esc) esc = false;
				else if (c === "\\") esc = true;
				else if (c === '"') inStr = false;
			} else if (c === '"') inStr = true;
			else if (c === "{") depth++;
			else if (c === "}") {
				depth--;
				if (depth === 0) {
					try {
						return JSON.parse(text.slice(start, i + 1));
					} catch {
						break; // try the next "{"
					}
				}
			}
		}
		start = text.indexOf("{", start + 1);
	}
	throw new Error("no JSON object found in model output");
}

/** Coerce raw model JSON into a validated WorkflowSpec (tolerant of missing optionals). */
export function validateSpec(raw: Record<string, unknown>, fallbackToolId?: string): WorkflowSpec {
	const rawSteps = Array.isArray(raw.steps) ? (raw.steps as Record<string, unknown>[]) : [];
	const steps: WorkflowStep[] = rawSteps
		.filter((s) => s && STEP_KINDS.includes(s.kind as WorkflowStepKind))
		.map((s) => ({
			id: String(s.id ?? ""),
			kind: s.kind as WorkflowStepKind,
			title: String(s.title ?? ""),
			detail: String(s.detail ?? ""),
			integration: s.integration ? String(s.integration) : undefined,
			gated: Boolean(s.gated),
		}));
	const c = raw.clarify as Record<string, unknown> | null | undefined;
	const clarify: ClarifyQuestion | null =
		c && (c.field === "threshold" || c.field === "channel")
			? { field: c.field, prompt: String(c.prompt ?? ""), step_id: String(c.step_id ?? "") }
			: null;
	return {
		name: String(raw.name ?? "Untitled workflow"),
		tool_id: String(raw.tool_id ?? fallbackToolId ?? "inbound-routing"),
		steps,
		narration: String(raw.narration ?? ""),
		clarify,
	};
}
