import { afterAll, beforeAll, expect, test } from "bun:test";
import { createServer } from "./service.js";
import { authorTool, authorToolWithModel, buildTool, type ToolAuthorOptions } from "./tools.js";

type CompleteFn = NonNullable<ToolAuthorOptions["complete"]>;

// A minimal stand-in for pi-ai's complete: returns canned text content (and can
// count calls), so these tests never hit a live model.
function fakeComplete(text: string, captured?: { calls: number }): CompleteFn {
	return (async () => {
		if (captured) captured.calls++;
		return { content: [{ type: "text", text }] };
	}) as unknown as CompleteFn;
}

const MODEL_TOOL_JSON = JSON.stringify({
	name: "chaseUnpaidInvoices",
	title: "Chase unpaid invoices",
	purpose: "Find overdue invoices and draft a chase note for each.",
	inputs: ["invoice"],
	code: "async function chaseUnpaidInvoices(invoice) {\n  return nex.run(invoice);\n}",
});

// --- deterministic (stub) path ---------------------------------------------

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

test("authorTool synthesizes a valid identifier from a digit-leading workflow", () => {
	// "2026" leads after stopword filtering — bare camelCasing would emit
	// `async function 2026RenewalSync(...)`, which is not legal JS.
	const t = authorTool("2026 renewal sync");
	expect(t.name).toBe("run2026RenewalSync");
	expect(/^[A-Za-z_$][A-Za-z0-9_$]*$/.test(t.name)).toBe(true);
	expect(t.code).toContain(`async function ${t.name}(input)`);
});

test("authorTool keeps a multi-line description inside the scripted-from comment", () => {
	// A raw newline in the description would terminate the `//` comment and spill
	// text into the function body. It must be collapsed to a single space.
	const t = authorTool("archive old records\nnightly across regions");
	const commentLine = t.code.split("\n").find((l) => l.includes("Nex scripted this from"));
	expect(commentLine).toContain('archive old records nightly across regions"');
});

test("buildTool returns the tool + a narration (stub by default)", async () => {
	const r = await buildTool("draft a follow-up for a stalled deal");
	expect(r.tool?.name).toBe("draftFollowup");
	expect(r.narration).toContain("Built");
	expect(r.authored_by).toBe("stub");
});

test("buildTool does not spend a model call unless tryModel is set", async () => {
	const cap = { calls: 0 };
	const r = await buildTool("archive old records nightly", { complete: fakeComplete(MODEL_TOOL_JSON, cap) });
	expect(cap.calls).toBe(0);
	expect(r.authored_by).toBe("stub");
});

// --- model path --------------------------------------------------------------

test("buildTool uses the model's tool when it answers with valid JSON", async () => {
	const r = await buildTool("chase unpaid invoices weekly", { tryModel: true, complete: fakeComplete(MODEL_TOOL_JSON) });
	expect(r.authored_by).toBe("model");
	expect(r.tool?.name).toBe("chaseUnpaidInvoices");
	expect(r.tool?.title).toBe("Chase unpaid invoices");
	expect(r.tool?.inputs).toEqual([{ name: "invoice", type: "string" }]);
	expect(r.tool?.code).toContain("async function chaseUnpaidInvoices(invoice)");
	expect(r.narration).toContain("Chase unpaid invoices");
});

test("buildTool falls back to the stub on a garbage model reply", async () => {
	const r = await buildTool("draft a follow-up for a stalled deal", {
		tryModel: true,
		complete: fakeComplete("sorry, I cannot help with that"),
	});
	expect(r.authored_by).toBe("stub");
	expect(r.tool?.name).toBe("draftFollowup"); // the deterministic shape, not the model's
});

test("buildTool falls back to the stub when the model call throws", async () => {
	const throwing = (async () => {
		throw new Error("provider unreachable");
	}) as unknown as CompleteFn;
	const r = await buildTool("draft a follow-up for a stalled deal", { tryModel: true, complete: throwing });
	expect(r.authored_by).toBe("stub");
	expect(r.tool?.name).toBe("draftFollowup");
});

test("buildTool falls back to the stub when the model tool fails validation", async () => {
	// Parseable JSON, but no code -> not a usable tool.
	const r = await buildTool("draft a follow-up for a stalled deal", {
		tryModel: true,
		complete: fakeComplete('{"name":"draftIt","title":"Draft it","inputs":[]}'),
	});
	expect(r.authored_by).toBe("stub");
});

test("authorToolWithModel coerces inputs and derives a missing title from the description", async () => {
	// inputs mixes strings, {name} objects, and garbage; title is omitted.
	const raw = JSON.stringify({
		name: "syncRenewals",
		inputs: ["deal", { name: "owner" }, 42, {}, null, "  "],
		code: "async function syncRenewals(deal, owner) { return nex.run(deal); }",
	});
	const t = await authorToolWithModel("When a renewal nears, sync renewal owners weekly", { complete: fakeComplete(raw) });
	expect(t.inputs).toEqual([
		{ name: "deal", type: "string" },
		{ name: "owner", type: "string" },
	]);
	expect(t.title).toBe("Sync renewal owners weekly"); // humanized from the description
	expect(t.purpose).toBe("When a renewal nears, sync renewal owners weekly");
});

test("authorToolWithModel rejects before calling the model when the signal is already aborted", async () => {
	const cap = { calls: 0 };
	const ctrl = new AbortController();
	ctrl.abort(new Error("client gone"));
	await expect(
		authorToolWithModel("x", { complete: fakeComplete(MODEL_TOOL_JSON, cap), signal: ctrl.signal }),
	).rejects.toThrow("client gone");
	expect(cap.calls).toBe(0); // no model call spent on a dropped request
});

// --- service ------------------------------------------------------------------

let server: ReturnType<typeof createServer>;
let base: string;
let prevToolAuthorModel: string | undefined;
beforeAll(() => {
	// With TOOL_AUTHOR_MODEL unset, /tools/build answers deterministically (no
	// model attempted, no authoring timeout) — these requests must be fast. Clear
	// it explicitly so a dev shell that exports it cannot make this suite hit a
	// live model; restore the shell's value in afterAll.
	prevToolAuthorModel = process.env.TOOL_AUTHOR_MODEL;
	delete process.env.TOOL_AUTHOR_MODEL;
	server = createServer({ port: 0 });
	base = server.url.toString().replace(/\/$/, "");
});
afterAll(() => {
	server.stop(true);
	if (prevToolAuthorModel === undefined) delete process.env.TOOL_AUTHOR_MODEL;
	else process.env.TOOL_AUTHOR_MODEL = prevToolAuthorModel;
});

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
	expect(body.authored_by).toBe("stub");
});

test("POST /tools/build rejects a schema mismatch", async () => {
	const res = await fetch(`${base}/tools/build`, {
		method: "POST",
		headers: { "content-type": "application/json" },
		body: JSON.stringify({ schema_version: 99, message: "x" }),
	});
	expect(res.status).toBe(400);
});
