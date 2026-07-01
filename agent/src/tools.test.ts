import { afterAll, beforeAll, expect, test } from "bun:test";
import { createServer } from "./service.js";
import { authorTool, buildTool } from "./tools.js";

test("authorTool matches a known workflow shape", () => {
	const t = authorTool("score its fit and route hot leads to the AE");
	expect(t.name).toBe("scoreAndRouteLead");
	expect(t.title).toBe("Score & route a lead");
	expect(t.inputs.map((i) => i.name)).toEqual(["lead"]);
	expect(t.code).toContain("async function scoreAndRouteLead(lead)");
});

test("authorTool synthesizes a name + plain title for an unknown workflow", () => {
	const t = authorTool("When an invoice arrives, archive old records nightly");
	// Trigger clause dropped for the title; stopwords dropped + camelCased for name.
	expect(t.title).toBe("Archive old records nightly");
	expect(t.name).toBe("invoiceArrivesArchive");
	expect(t.inputs.map((i) => i.name)).toEqual(["input"]);
});

test("buildTool returns the tool + a narration", () => {
	const r = buildTool("draft a follow-up for a stalled deal");
	expect(r.tool?.name).toBe("draftFollowup");
	expect(r.narration).toContain("Built");
});

let server: ReturnType<typeof createServer>;
let base: string;
beforeAll(() => {
	server = createServer({ port: 0 });
	base = server.url.toString().replace(/\/$/, "");
});
afterAll(() => server.stop(true));

test("POST /tools/build creates a tool", async () => {
	const res = await fetch(`${base}/tools/build`, {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify({ schema_version: 1, message: "draft a follow-up for a stalled deal", app: "Pipeline" }),
	});
	expect(res.status).toBe(200);
	const body = await res.json();
	expect(body.tool.name).toBe("draftFollowup");
	expect(body.tool.inputs.map((i: { name: string }) => i.name)).toEqual(["deal"]);
	expect(body.narration).toContain("Built");
});

test("POST /tools/build rejects a schema mismatch", async () => {
	const res = await fetch(`${base}/tools/build`, {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify({ schema_version: 99, message: "x" }),
	});
	expect(res.status).toBe(400);
});
