import { expect, test } from "bun:test";
import { runWorkflow } from "./executor.js";
import type { WorkflowSpec } from "./wire.js";

const gatedSpec: WorkflowSpec = {
	name: "n", tool_id: "t", narration: "", clarify: null,
	steps: [
		{ id: "s1", kind: "trigger", title: "Start", detail: "d" },
		{ id: "s2", kind: "action", title: "Route", detail: "d", integration: "Slack", gated: true },
	],
};

test("halts at a gated step for approval (CQ1)", async () => {
	const r = await runWorkflow(gatedSpec);
	expect(r.status).toBe("needs_approval");
	expect(r.pending_approval?.step_id).toBe("s2");
});

test("completes once the gated step is approved", async () => {
	const r = await runWorkflow(gatedSpec, { approved: ["s2"] });
	expect(r.status).toBe("done");
	expect(r.steps.every((s) => s.status === "ok")).toBe(true);
});

test("simulates steps without an api", async () => {
	const r = await runWorkflow({ ...gatedSpec, steps: [{ id: "a", kind: "trigger", title: "t", detail: "d" }] });
	expect(r.status).toBe("done");
	expect(r.steps[0].detail).toContain("simulated");
});

function withFetch(captured: { url?: string; headers?: Record<string, string> }, ok: boolean, status: number): typeof fetch {
	return (async (url: string, init: { headers?: Record<string, string> }) => {
		captured.url = String(url);
		captured.headers = init?.headers;
		return { ok, status } as Response;
	}) as unknown as typeof fetch;
}

test("replays an api step as a real call (API-first execution)", async () => {
	const cap: { url?: string } = {};
	const spec: WorkflowSpec = {
		name: "n", tool_id: "t", narration: "", clarify: null,
		steps: [{ id: "x", kind: "enrich", title: "Lookup", detail: "d", api: { method: "GET", url: "https://api.example.com/c", query: { id: "42" } } }],
	};
	const r = await runWorkflow(spec, {}, { fetchImpl: withFetch(cap, true, 200) });
	expect(r.status).toBe("done");
	expect(r.steps[0].http_status).toBe(200);
	expect(cap.url).toBe("https://api.example.com/c?id=42");
});

test("resolves auth_ref into headers (secret never in the spec)", async () => {
	const cap: { headers?: Record<string, string> } = {};
	const spec: WorkflowSpec = {
		name: "n", tool_id: "t", narration: "", clarify: null,
		steps: [{ id: "x", kind: "enrich", title: "Lookup", detail: "d", api: { method: "GET", url: "https://api.example.com/c", auth_ref: "acme_key" } }],
	};
	await runWorkflow(spec, {}, {
		fetchImpl: withFetch(cap, true, 200),
		resolveCredential: (ref) => (ref === "acme_key" ? { Authorization: "Bearer SECRET" } : undefined),
	});
	expect(cap.headers?.Authorization).toBe("Bearer SECRET");
});

test("a 3xx halts the run with error and does NOT follow the redirect (regression: CodeRabbit executor.ts:42)", async () => {
	let calls = 0;
	let redirectMode: string | undefined;
	const fetchImpl = (async (_url: string, init: { redirect?: string }) => {
		calls++;
		redirectMode = init?.redirect;
		return { ok: false, status: 302 } as Response;
	}) as unknown as typeof fetch;
	const spec: WorkflowSpec = {
		name: "n", tool_id: "t", narration: "", clarify: null,
		steps: [{ id: "x", kind: "enrich", title: "Lookup", detail: "d", api: { method: "GET", url: "https://api.example.com/c" } }],
	};
	const r = await runWorkflow(spec, {}, { fetchImpl });
	expect(r.status).toBe("error");
	expect(r.steps[0].status).toBe("error");
	expect(r.steps[0].http_status).toBe(302);
	expect(calls).toBe(1); // replayed exactly once
	expect(redirectMode).toBe("manual"); // redirect not auto-followed
});

test("halts with error when a replayed call fails", async () => {
	const spec: WorkflowSpec = {
		name: "n", tool_id: "t", narration: "", clarify: null,
		steps: [{ id: "x", kind: "action", title: "Post", detail: "d", api: { method: "POST", url: "https://api.example.com/x" } }],
	};
	const r = await runWorkflow(spec, {}, { fetchImpl: withFetch({}, false, 500) });
	expect(r.status).toBe("error");
	expect(r.steps[0].status).toBe("error");
});
