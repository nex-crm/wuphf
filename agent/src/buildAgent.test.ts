import { expect, test } from "bun:test";
import { extractJson, validateSpec } from "./wire.js";

test("extractJson pulls the object out of a fenced/noisy reply", () => {
	const text = '```json\n{"name":"X","steps":[]}\n```\ntrailing';
	expect(extractJson(text)).toEqual({ name: "X", steps: [] });
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
