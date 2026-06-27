// agentClient — talks to the real pi-mono build agent (agent/ service) over the
// same WorkflowSpec contract the mock produces. Frontend-first + graceful: if the
// service is unreachable, fall back to the deterministic mock planWorkflow so the
// prototype always works. When the service is up, the operator gets the real
// engine (key-free via the operator's subscription /login, or local Ollama).

import type { WorkflowStep } from "../mock/data";
import { planWorkflow, type ClarifyQuestion, type WorkflowPlan } from "./planWorkflow";

// Vite env; defaults to the local agent service.
const AGENT_URL =
	(import.meta as unknown as { env?: Record<string, string> }).env?.VITE_AGENT_URL ?? "http://127.0.0.1:8820";

interface WireSpec {
	name?: string;
	tool_id?: string;
	steps?: WorkflowStep[];
	narration?: string;
	clarify?: { field?: string; prompt?: string; step_id?: string } | null;
}

function toPlan(spec: WireSpec): WorkflowPlan {
	const clarify: ClarifyQuestion | null =
		spec.clarify && (spec.clarify.field === "threshold" || spec.clarify.field === "channel")
			? { field: spec.clarify.field, prompt: String(spec.clarify.prompt ?? ""), stepId: String(spec.clarify.step_id ?? "") }
			: null;
	return {
		name: String(spec.name ?? "Untitled workflow"),
		toolId: String(spec.tool_id ?? "inbound-routing"),
		steps: Array.isArray(spec.steps) ? spec.steps : [],
		narration: String(spec.narration ?? ""),
		clarify,
	};
}

/** Parse the SSE body for the terminal `spec` event's payload. */
function specFromSse(text: string): WireSpec {
	let event = "";
	for (const block of text.split("\n\n")) {
		for (const line of block.split("\n")) {
			if (line.startsWith("event:")) event = line.slice(6).trim();
			else if (line.startsWith("data:") && event === "spec") {
				const data = JSON.parse(line.slice(5).trim()) as { spec?: WireSpec };
				if (data.spec) return data.spec;
			}
		}
	}
	throw new Error("no spec event in build stream");
}

async function buildPlanViaService(description: string): Promise<WorkflowPlan> {
	const res = await fetch(`${AGENT_URL}/build/stream`, {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify({ schema_version: 1, message: description }),
	});
	if (!res.ok) throw new Error(`agent service ${res.status}`);
	return toPlan(specFromSse(await res.text()));
}

/** Real engine when the service is reachable, else the deterministic mock. */
export async function buildPlanSmart(description: string): Promise<WorkflowPlan> {
	try {
		return await buildPlanViaService(description);
	} catch {
		return planWorkflow(description);
	}
}

/** Execute a built workflow on the agent service; returns the run result JSON. */
export async function runWorkflowViaService(spec: {
	name: string;
	tool_id: string;
	steps: WorkflowStep[];
	narration?: string;
	clarify?: unknown;
}, input: Record<string, unknown> = {}): Promise<unknown> {
	const res = await fetch(`${AGENT_URL}/run`, {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify({ schema_version: 1, spec, input }),
	});
	if (!res.ok) throw new Error(`agent service ${res.status}`);
	return res.json();
}
