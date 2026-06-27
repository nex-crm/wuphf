// Deterministic executor: run a compiled WorkflowSpec step by step.
//
// The DETERMINISTIC half of the spine — it runs the compiled spec, it does not
// reason. A step that carries an `api` (built by discovery: browsersniff HAR ->
// ApiCall) is REPLAYED as a real HTTP call (API-first execution); a step without
// one is simulated (the plan was authored by chat, not captured yet). A `gated`
// step (external mutation) HALTS with status="needs_approval" -> human approval
// card (CQ1); approve -> the FE re-runs with that id in input.approved. A failed
// real call halts the run with status="error" (deterministic: stop, don't guess).

import type { ApiCall, RunResult, RunStep, WorkflowSpec } from "./wire.js";

// ref -> headers (e.g. {Authorization: "Bearer …"}). A NAMED credential ref is
// resolved here at run time; secret values never live in the spec (A3/A4).
export type CredentialResolver = (ref: string) => Record<string, string> | undefined;

export interface ExecOptions {
	resolveCredential?: CredentialResolver;
	fetchImpl?: typeof fetch;
	timeoutMs?: number;
}

function buildUrl(call: ApiCall): string {
	if (!call.query || Object.keys(call.query).length === 0) return call.url;
	const u = new URL(call.url);
	for (const [k, v] of Object.entries(call.query)) u.searchParams.set(k, v);
	return u.toString();
}

async function replay(call: ApiCall, opts: ExecOptions): Promise<{ ok: boolean; status: number; detail: string }> {
	const doFetch = opts.fetchImpl ?? fetch;
	const headers: Record<string, string> = { ...(call.headers ?? {}) };
	if (call.auth_ref) Object.assign(headers, opts.resolveCredential?.(call.auth_ref) ?? {});
	const ctrl = new AbortController();
	const t = setTimeout(() => ctrl.abort(), opts.timeoutMs ?? 30_000);
	try {
		const res = await doFetch(buildUrl(call), {
			method: call.method || "GET",
			headers,
			body: call.body == null ? undefined : typeof call.body === "string" ? call.body : JSON.stringify(call.body),
			signal: ctrl.signal,
		});
		return { ok: res.ok, status: res.status, detail: `${call.method} ${call.url} -> ${res.status}` };
	} catch (e) {
		return { ok: false, status: 0, detail: `${call.method} ${call.url} failed: ${String(e)}` };
	} finally {
		clearTimeout(t);
	}
}

export async function runWorkflow(spec: WorkflowSpec, input: Record<string, unknown> = {}, opts: ExecOptions = {}): Promise<RunResult> {
	const approved = new Set<string>(Array.isArray(input.approved) ? (input.approved as unknown[]).map(String) : []);
	const steps: RunStep[] = [];
	for (const step of spec.steps) {
		if (step.gated && !approved.has(step.id)) {
			steps.push({ step_id: step.id, status: "awaiting_approval", detail: `${step.title} mutates ${step.integration ?? "an external system"} — needs approval.` });
			return {
				status: "needs_approval",
				steps,
				digest: `Paused at ${step.title}: external mutation needs the human approval card.`,
				pending_approval: { step_id: step.id, title: step.title, integration: step.integration, detail: step.detail },
			};
		}
		if (step.api) {
			const r = await replay(step.api, opts);
			steps.push({ step_id: step.id, status: r.ok ? "ok" : "error", detail: r.detail, http_status: r.status });
			if (!r.ok) {
				return { status: "error", steps, digest: `Halted at ${step.title}: ${r.detail}`, pending_approval: null };
			}
		} else {
			steps.push({ step_id: step.id, status: "ok", detail: `${step.title} ran (simulated).` });
		}
	}
	return { status: "done", steps, digest: `Ran ${steps.length} steps to completion.`, pending_approval: null };
}
