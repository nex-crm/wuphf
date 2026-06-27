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

test("halts at a gated step for approval (CQ1)", () => {
	const r = runWorkflow(gatedSpec);
	expect(r.status).toBe("needs_approval");
	expect(r.pending_approval?.step_id).toBe("s2");
	expect(r.steps.at(-1)?.status).toBe("awaiting_approval");
});

test("completes once the gated step is approved", () => {
	const r = runWorkflow(gatedSpec, { approved: ["s2"] });
	expect(r.status).toBe("done");
	expect(r.steps.every((s) => s.status === "ok")).toBe(true);
});

test("ungated workflow runs to completion", () => {
	const r = runWorkflow({ ...gatedSpec, steps: [{ id: "a", kind: "trigger", title: "t", detail: "d" }] });
	expect(r.status).toBe("done");
});
