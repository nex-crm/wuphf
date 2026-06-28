import { afterAll, beforeAll, expect, test } from "bun:test";
import { createServer } from "./service.js";
import type { WorkflowSpec } from "./wire.js";

// Stub the build engine so the service test never hits a live model.
async function* fakeBuild() {
	const spec: WorkflowSpec = {
		name: "Inbound routing", tool_id: "inbound-routing", narration: "n", clarify: null,
		steps: [
			{ id: "t", kind: "trigger", title: "New lead", detail: "d" },
			{ id: "a", kind: "action", title: "Route", detail: "d", integration: "Slack", gated: true },
		],
	};
	for (const step of spec.steps) yield { type: "step" as const, step };
	yield { type: "spec" as const, spec };
}

let server: ReturnType<typeof createServer>;
let base: string;
beforeAll(() => {
	server = createServer({ port: 0, buildStream: fakeBuild });
	base = server.url.toString().replace(/\/$/, "");
});
afterAll(() => server.stop(true));

test("/health and /providers", async () => {
	expect((await (await fetch(`${base}/health`)).json()).status).toBe("ok");
	const p = await (await fetch(`${base}/providers`)).json();
	expect(Array.isArray(p.providers)).toBe(true);
});

test("/build/stream emits start, steps, then spec", async () => {
	const res = await fetch(`${base}/build/stream`, {
		method: "POST", headers: { "content-type": "application/json" },
		body: JSON.stringify({ schema_version: 1, message: "route inbound leads" }),
	});
	const text = await res.text();
	const events = [...text.matchAll(/event: (\w+)/g)].map((m) => m[1]);
	expect(events[0]).toBe("start");
	expect(events).toContain("step");
	expect(events.at(-1)).toBe("spec");
});

test("/run halts on a gate then completes when approved", async () => {
	const spec = { name: "n", tool_id: "t", narration: "", clarify: null,
		steps: [{ id: "a", kind: "action", title: "Route", detail: "d", integration: "Slack", gated: true }] };
	const r1 = await (await fetch(`${base}/run`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ spec, input: {} }) })).json();
	expect(r1.status).toBe("needs_approval");
	const r2 = await (await fetch(`${base}/run`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ spec, input: { approved: ["a"] } }) })).json();
	expect(r2.status).toBe("done");
});

test("schema_version mismatch is rejected", async () => {
	const res = await fetch(`${base}/build/stream`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ schema_version: 99, message: "x" }) });
	expect(res.status).toBe(400);
});

test("/run returns 400 on a malformed JSON body (regression: CodeRabbit service.ts:48)", async () => {
	const res = await fetch(`${base}/run`, { method: "POST", headers: { "content-type": "application/json" }, body: "{not valid json" });
	expect(res.status).toBe(400);
	expect((await res.json()).error).toBeTruthy();
});

test("/build/stream returns 400 on a malformed JSON body before streaming", async () => {
	const res = await fetch(`${base}/build/stream`, { method: "POST", headers: { "content-type": "application/json" }, body: "{nope" });
	expect(res.status).toBe(400);
	expect(res.headers.get("content-type")).toContain("application/json"); // a clean 400, not a half-open SSE
});
