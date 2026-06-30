import { expect, test } from "bun:test";
import { type BuildOptions, buildWorkflow } from "./buildAgent.js";
import { extractJson, validateSpec } from "./wire.js";

type CompleteFn = NonNullable<BuildOptions["complete"]>;

// A minimal stand-in for pi-ai's complete: records the signal it was handed and
// returns one JSON spec as text content, so these tests never hit a live model.
function fakeComplete(captured: { calls: number; signal?: AbortSignal }): CompleteFn {
	return (async (_model: unknown, _ctx: unknown, opts?: { signal?: AbortSignal }) => {
		captured.calls++;
		captured.signal = opts?.signal;
		return { content: [{ type: "text", text: '{"name":"X","tool_id":"x","narration":"n","steps":[],"clarify":null}' }] };
	}) as unknown as CompleteFn;
}

test("extractJson pulls the object out of a fenced/noisy reply", () => {
	const text = '```json\n{"name":"X","steps":[]}\n```\ntrailing';
	expect(extractJson(text)).toEqual({ name: "X", steps: [] });
});

test("extractJson handles escaped quotes inside strings (regression: CodeRabbit wire.ts:95)", () => {
	// A backslash-escaped quote must not terminate the string early, or valid
	// model output with quoted detail/narration gets treated as malformed.
	const text = 'preamble {"name":"X","narration":"say \\"hi\\" now","steps":[]} tail';
	expect(extractJson(text)).toEqual({ name: "X", narration: 'say "hi" now', steps: [] });
});

test("validateSpec coerces + keeps only valid step kinds and gating", () => {
	const spec = validateSpec({
		name: "Inbound routing",
		tool_id: "inbound-routing",
		narration: "n",
		steps: [
			{ id: "t", kind: "trigger", title: "New lead", detail: "d" },
			{ id: "x", kind: "bogus", title: "drop me", detail: "d" },
			{ id: "a", kind: "action", title: "Route", detail: "d", integration: "Slack", gated: true },
		],
		clarify: { field: "channel", prompt: "where?", step_id: "a" },
	});
	expect(spec.tool_id).toBe("inbound-routing");
	expect(spec.steps.map((s) => s.kind)).toEqual(["trigger", "action"]);
	expect(spec.steps[1].gated).toBe(true);
	expect(spec.clarify?.field).toBe("channel");
});

test("validateSpec falls back when fields are missing", () => {
	const spec = validateSpec({}, "fallback-tool");
	expect(spec.tool_id).toBe("fallback-tool");
	expect(spec.steps).toEqual([]);
	expect(spec.clarify).toBeNull();
});

test("buildWorkflow forwards a live abort signal to the model call (regression: CodeRabbit buildAgent.ts:33)", async () => {
	const cap = { calls: 0 } as { calls: number; signal?: AbortSignal };
	const ctrl = new AbortController();
	const spec = await buildWorkflow("route inbound leads", { complete: fakeComplete(cap), signal: ctrl.signal });
	expect(cap.calls).toBe(1);
	expect(cap.signal).toBeInstanceOf(AbortSignal); // a signal is always threaded through
	expect(cap.signal?.aborted).toBe(false);
	expect(spec.tool_id).toBe("x");
});

test("buildWorkflow rejects before calling the model when the signal is already aborted", async () => {
	const cap = { calls: 0 } as { calls: number; signal?: AbortSignal };
	const ctrl = new AbortController();
	ctrl.abort(new Error("client gone"));
	await expect(buildWorkflow("x", { complete: fakeComplete(cap), signal: ctrl.signal })).rejects.toThrow("client gone");
	expect(cap.calls).toBe(0); // no model call spent on a dropped request
});
