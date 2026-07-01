import { afterAll, beforeAll, expect, test } from "bun:test";
import { createServer } from "./service.js";
import { type CapabilityTree, runTool } from "./toolRuntime.js";
import type { Tool, ToolCallResult, WorkflowSpec } from "./wire.js";

// Tool fixtures are inlined (not imported from tools.ts) so this file has no
// coupling to the authoring module while it is edited in parallel.

function makeTool(code: string, inputs: Tool["inputs"] = []): Tool {
	return { name: "t", title: "T", purpose: "p", inputs, code };
}

const READ_TOOL = makeTool(
	[
		"async function weeklyPipelineSummary() {",
		"  const deals = await crm.deals({ since: '7d' });",
		"  const moved = deals.filter((d) => d.stageChanged);",
		"  return nex.ai.summarize(moved, { style: 'exec recap' });",
		"}",
	].join("\n"),
);

const GATED_TOOL = makeTool(
	[
		"async function routeLead(lead) {",
		"  const ae = await crm.ownerFor(lead);",
		"  await crm.assign(lead, ae);",
		"  return `routed ${lead} to ${ae.name}`;",
		"}",
	].join("\n"),
	[{ name: "lead", type: "string" }],
);

test("a read tool (crm.deals + summarize) runs ok with actions recorded", async () => {
	const r = await runTool(READ_TOOL, {});
	expect(r.status).toBe("ok");
	if (r.status !== "ok") throw new Error("unreachable");
	expect(r.result).toContain("simulated recap");
	expect(r.actions.some((a) => a.startsWith("crm.deals("))).toBe(true);
	expect(r.actions.some((a) => a.startsWith("nex.ai.summarize("))).toBe(true);
});

test("crm.assign halts needs_approval by default (default deny)", async () => {
	const r = await runTool(GATED_TOOL, { lead: "Acme" });
	expect(r.status).toBe("needs_approval");
	if (r.status !== "needs_approval") throw new Error("unreachable");
	expect(r.gate.capability).toBe("crm.assign");
	expect(r.gate.detail).toContain("Acme");
	expect(r.gate.detail).toContain("Priya (AE)");
});

test("the same call with approved: true completes", async () => {
	const r = await runTool(GATED_TOOL, { lead: "Acme" }, { approved: true });
	expect(r.status).toBe("ok");
	if (r.status !== "ok") throw new Error("unreachable");
	expect(r.result).toBe("routed Acme to Priya (AE)");
	expect(r.actions.some((a) => a.startsWith("crm.assign("))).toBe(true);
});

test("nex.send is gated too", async () => {
	const t = makeTool('async function ping() { return nex.send("#sales", "hi"); }');
	const denied = await runTool(t, {});
	expect(denied.status).toBe("needs_approval");
	if (denied.status !== "needs_approval") throw new Error("unreachable");
	expect(denied.gate.capability).toBe("nex.send");
	const sent = await runTool(t, {}, { approved: true });
	expect(sent.status).toBe("ok");
});

test("a thrown error -> status error (with prior actions kept)", async () => {
	const t = makeTool('async function boom() { await crm.deals(); throw new Error("kaput"); }');
	const r = await runTool(t, {});
	expect(r.status).toBe("error");
	if (r.status !== "error") throw new Error("unreachable");
	expect(r.detail).toContain("kaput");
	expect(r.actions.some((a) => a.startsWith("crm.deals("))).toBe(true);
});

test("the code scan rejects import (dynamic and static) and eval", async () => {
	for (const code of [
		'async function t() { const fs = await import("fs"); return "x"; }',
		'import fs from "fs";\nasync function t() { return "x"; }',
		'async function t() { return eval("1+1"); }',
	]) {
		const r = await runTool(makeTool(code), {});
		expect(r.status).toBe("error");
	}
});

test("dangerous globals are shadowed to undefined inside tool code", async () => {
	const t = makeTool("async function t() { return String(typeof fetch) + '/' + String(typeof process); }");
	const r = await runTool(t, {});
	expect(r.status).toBe("ok");
	if (r.status !== "ok") throw new Error("unreachable");
	expect(r.result).toBe("undefined/undefined");
});

test("cooperative timeout: a never-resolving capability -> error", async () => {
	const capabilities: CapabilityTree = {
		nex: { run: () => new Promise(() => {}) },
		crm: {},
	};
	const t = makeTool("async function t(input) { return nex.run(input); }", [{ name: "input", type: "string" }]);
	const r = await runTool(t, { input: "x" }, { capabilities, timeoutMs: 20 });
	expect(r.status).toBe("error");
	if (r.status !== "error") throw new Error("unreachable");
	expect(r.detail).toContain("timed out after 20ms");
});

test("injected capabilities are used AND recorded (and stay gated)", async () => {
	const seen: string[] = [];
	const capabilities: CapabilityTree = {
		crm: {
			deals: () => {
				seen.push("deals");
				return [{ name: "OnlyDeal", stageChanged: true }];
			},
			assign: () => "assigned",
		},
		nex: { ai: { summarize: (items: unknown) => `got ${(items as unknown[]).length}` } },
	};
	const read = makeTool("async function t() { const d = await crm.deals({ since: '7d' }); return nex.ai.summarize(d); }");
	const r = await runTool(read, {}, { capabilities });
	expect(r.status).toBe("ok");
	if (r.status !== "ok") throw new Error("unreachable");
	expect(r.result).toBe("got 1");
	expect(seen).toEqual(["deals"]);
	expect(r.actions[0]).toBe('crm.deals({"since":"7d"})');
	// The gate is enforced at the instrumentation layer, so an injected
	// crm.assign is still default-deny.
	const gated = makeTool('async function t() { return crm.assign("Acme", "Priya"); }');
	const g = await runTool(gated, {}, { capabilities });
	expect(g.status).toBe("needs_approval");
});

test("an input name that collides with the sandbox is rejected", async () => {
	const t = makeTool("async function t(fetch) { return fetch; }", [{ name: "fetch", type: "string" }]);
	const r = await runTool(t, { fetch: "x" });
	expect(r.status).toBe("error");
	if (r.status !== "error") throw new Error("unreachable");
	expect(r.detail).toContain("invalid tool input name");
});

// ---------------------------------------------------------------------------
// Service-level: POST /tools/call
// ---------------------------------------------------------------------------

async function* fakeBuild() {
	yield {
		type: "spec" as const,
		spec: { name: "n", tool_id: "t", narration: "", clarify: null, steps: [] } as WorkflowSpec,
	};
}

let server: ReturnType<typeof createServer>;
let base: string;
beforeAll(() => {
	server = createServer({ port: 0, buildStream: fakeBuild });
	base = server.url.toString().replace(/\/$/, "");
});
afterAll(() => server.stop(true));

function post(body: unknown): Promise<Response> {
	return fetch(`${base}/tools/call`, {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify(body),
	});
}

test("POST /tools/call runs a read tool ok", async () => {
	const res = await post({ schema_version: 1, tool: READ_TOOL, args: {} });
	expect(res.status).toBe(200);
	const data = (await res.json()) as ToolCallResult;
	expect(data.status).toBe("ok");
	expect(Array.isArray(data.actions)).toBe(true);
});

test("POST /tools/call halts needs_approval, then completes with approved: true", async () => {
	const r1 = (await (await post({ schema_version: 1, tool: GATED_TOOL, args: { lead: "Acme" } })).json()) as ToolCallResult;
	expect(r1.status).toBe("needs_approval");
	expect(r1.gate?.capability).toBe("crm.assign");
	const r2 = (await (
		await post({ schema_version: 1, tool: GATED_TOOL, args: { lead: "Acme" }, approved: true })
	).json()) as ToolCallResult;
	expect(r2.status).toBe("ok");
});

test("POST /tools/call 400s on a missing/malformed tool", async () => {
	for (const bad of [{}, { tool: null }, { tool: { name: "x" } }, { tool: { code: "y" } }]) {
		const res = await post(bad);
		expect(res.status).toBe(400);
	}
	const notJson = await fetch(`${base}/tools/call`, {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: "{nope",
	});
	expect(notJson.status).toBe(400);
});

test("POST /tools/call rejects a schema_version mismatch", async () => {
	const res = await post({ schema_version: 99, tool: READ_TOOL });
	expect(res.status).toBe(400);
});
