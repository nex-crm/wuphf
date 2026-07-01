import { expect, test } from "bun:test";
import { buildCapabilities, type CapabilityConfig, capabilityConfigFromEnv } from "./capabilities.js";
import type { CapabilityFn, CapabilityTree } from "./toolRuntime.js";

function cap(tree: CapabilityTree, path: string): CapabilityFn {
	let node: CapabilityTree | CapabilityFn = tree;
	for (const part of path.split(".")) node = (node as CapabilityTree)[part];
	return node as CapabilityFn;
}

type FetchFn = NonNullable<CapabilityConfig["fetch"]>;
type CompleteFn = NonNullable<CapabilityConfig["complete"]>;

function jsonFetch(body: unknown, status = 200): FetchFn {
	return (async () => new Response(JSON.stringify(body), { status })) as unknown as FetchFn;
}

function fakeComplete(text: string): CompleteFn {
	return (async () => ({ content: [{ type: "text", text }] })) as unknown as CompleteFn;
}

const MODEL = { id: "test-model" } as unknown as NonNullable<CapabilityConfig["aiModel"]>;

// --- composition ---------------------------------------------------------------

test("unconfigured host: integrations.call throws an explanatory error", async () => {
	const tree = buildCapabilities({});
	await expect(Promise.resolve(cap(tree, "integrations.call")("gmail", "GMAIL_FETCH_EMAILS"))).rejects.toThrow(
		/not connected on this host/,
	);
});

test("unconfigured host: nex.browser degrades to a simulated marker", async () => {
	const tree = buildCapabilities({});
	const out = await cap(tree, "nex.browser")("open the vendor portal");
	expect(String(out)).toContain("browser engine not configured");
});

test("capabilityConfigFromEnv reads the broker seam + model gate", () => {
	const cfg = capabilityConfigFromEnv({
		WUPHF_BROKER_URL: "http://127.0.0.1:7893",
		WUPHF_BROKER_TOKEN: "tok",
	});
	expect(cfg.brokerUrl).toBe("http://127.0.0.1:7893");
	expect(cfg.brokerToken).toBe("tok");
	expect(cfg.aiModel).toBeUndefined(); // TOOL_RUNTIME_MODEL unset -> simulated ai
});

// --- real nex.ai (stubbed complete) ---------------------------------------------

test("real nex.ai.score parses the model's integer and clamps it", async () => {
	const tree = buildCapabilities({ aiModel: MODEL, complete: fakeComplete("Score: 87") });
	expect(await cap(tree, "nex.ai.score")("Acme", { rubric: "ICP fit" })).toBe(87);
});

test("real nex.ai falls back to the simulation when the model fails", async () => {
	const throwing = (async () => {
		throw new Error("provider down");
	}) as unknown as CompleteFn;
	const tree = buildCapabilities({ aiModel: MODEL, complete: throwing });
	const score = await cap(tree, "nex.ai.score")("Acme");
	expect(typeof score).toBe("number"); // deterministic hash fallback
	const recap = await cap(tree, "nex.ai.summarize")([{ name: "Globex" }]);
	expect(String(recap)).toContain("simulated recap");
});

test("real nex.ai.summarize returns the model's text", async () => {
	const tree = buildCapabilities({ aiModel: MODEL, complete: fakeComplete("6 deals moved; Globex leads.") });
	expect(await cap(tree, "nex.ai.summarize")([1, 2, 3])).toBe("6 deals moved; Globex leads.");
});

// --- real integrations.call (stubbed broker) ------------------------------------

const BROKER: CapabilityConfig = { brokerUrl: "http://broker.test", brokerToken: "tok" };

test("integrations.call executes a read and returns the result", async () => {
	const tree = buildCapabilities({
		...BROKER,
		fetch: jsonFetch({ connected: true, read_only: true, result: [{ subject: "Renewal" }] }),
	});
	const out = await cap(tree, "integrations.call")("gmail", "GMAIL_FETCH_EMAILS", { max_results: 5 });
	expect(out).toEqual([{ subject: "Renewal" }]);
});

test("integrations.call surfaces the broker's approval card for a mutation", async () => {
	const tree = buildCapabilities({
		...BROKER,
		fetch: jsonFetch({ connected: true, status: "needs_approval", request_id: "req_9" }),
	});
	const out = await cap(tree, "integrations.call")("slack", "SLACK_SENDS_A_MESSAGE", {});
	expect(String(out)).toContain("Held for your approval");
	expect(String(out)).toContain("req_9");
});

test("integrations.call throws on a broker error / disconnected platform", async () => {
	const errTree = buildCapabilities({ ...BROKER, fetch: jsonFetch({ error: "boom" }) });
	await expect(Promise.resolve(cap(errTree, "integrations.call")("gmail", "X"))).rejects.toThrow("boom");
	const discTree = buildCapabilities({ ...BROKER, fetch: jsonFetch({ connected: false }) });
	await expect(Promise.resolve(cap(discTree, "integrations.call")("gmail", "X"))).rejects.toThrow(/not connected/);
});

// --- real nex.browser (stubbed SSE) ---------------------------------------------

test("nex.browser streams the run and returns the outcome + action trace", async () => {
	const sse = [
		'data: {"type":"run","run_id":"r1"}',
		"",
		'data: {"type":"action","label":"Click New message"}',
		"",
		'data: {"type":"action","label":"Type the digest"}',
		"",
		'data: {"type":"done","result":"Posted the digest."}',
		"",
	].join("\n");
	const tree = buildCapabilities({
		...BROKER,
		fetch: (async () => new Response(sse, { status: 200 })) as unknown as FetchFn,
	});
	const out = String(await cap(tree, "nex.browser")("post the digest"));
	expect(out).toContain("Posted the digest.");
	expect(out).toContain("2 browser actions");
	expect(out).toContain("Click New message");
});

test("nex.browser fails loud when the run errors", async () => {
	const sse = ['data: {"type":"error","message":"No window for Chrome"}', ""].join("\n");
	const tree = buildCapabilities({
		...BROKER,
		fetch: (async () => new Response(sse, { status: 200 })) as unknown as FetchFn,
	});
	await expect(Promise.resolve(cap(tree, "nex.browser")("x"))).rejects.toThrow("No window for Chrome");
});
