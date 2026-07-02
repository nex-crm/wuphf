import { afterAll, expect, test } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { kebab, matchTool, runRoutine } from "./routineRunner.js";
import { AgentStore } from "./store.js";
import type { CapabilityTree } from "./toolRuntime.js";
import type { StoredTool, Tool } from "./wire.js";

// Everything here runs offline: capabilities are the injectable simulated tree
// (never a live broker) and authoring is either the injected stub below or the
// runner's default buildTool with tryModel unset (deterministic stub path).

const dirs: string[] = [];
function tmpStore(): AgentStore {
	const dir = mkdtempSync(join(tmpdir(), "wuphf-agent-runner-"));
	dirs.push(dir);
	return new AgentStore(join(dir, "data"));
}
afterAll(() => {
	for (const dir of dirs) rmSync(dir, { recursive: true, force: true });
});

function stored(partial: Partial<StoredTool> & Pick<StoredTool, "name" | "title">): StoredTool {
	return { purpose: "p", inputs: [], code: "async function x() {}", version: 1, ...partial };
}

test("matchTool: exact name or title mention wins; else >=2 title-word overlap", () => {
	const tools = [
		stored({ name: "weeklyPipelineSummary", title: "Weekly pipeline summary" }),
		stored({ name: "scoreAndRouteLead", title: "Score & route a lead" }),
	];
	expect(matchTool("run weeklyPipelineSummary now", tools)?.name).toBe("weeklyPipelineSummary");
	expect(matchTool("please do the weekly pipeline summary", tools)?.name).toBe("weeklyPipelineSummary");
	expect(matchTool("Score every new lead and route hot ones", tools)?.name).toBe("scoreAndRouteLead");
	// One overlapping word is not enough — author fresh instead of hijacking.
	expect(matchTool("email the pipeline doc to finance", tools)).toBeNull();
	expect(matchTool("totally unrelated", tools)).toBeNull();
});

test("kebab names are filesystem-tame", () => {
	expect(kebab("Monday pipeline recap")).toBe("monday-pipeline-recap");
	expect(kebab("  Chase stalled deals!! ")).toBe("chase-stalled-deals");
	expect(kebab("***")).toBe("routine");
});

test("run with a matched tool: transcript, lastRun, and an md artifact land", async () => {
	const store = tmpStore();
	store.upsertTool("a1", {
		name: "weeklyPipelineSummary",
		title: "Weekly pipeline summary",
		purpose: "p",
		inputs: [],
		code: "async function weeklyPipelineSummary() { return 'the recap'; }",
	});
	const { routine } = store.createRoutine("a1", "Monday recap", "run weeklyPipelineSummary", "Every Monday 9:00");
	const authored: string[] = [];
	const r = await runRoutine("a1", routine.id, {
		store,
		author: (p) => {
			authored.push(p);
			return Promise.resolve({ tool: null });
		},
	});
	expect(r?.status).toBe("ok");
	expect(r?.outcome).toBe("the recap");
	expect(authored).toEqual([]); // matched -> no authoring
	// Transcript: the scheduled prompt in, the outcome back.
	const msgs = store.getSession("a1", routine.sessionId)?.messages ?? [];
	expect(msgs.map((m) => m.from)).toEqual(["you", "nex"]);
	expect(msgs[0].body).toBe("(scheduled) run weeklyPipelineSummary");
	expect(msgs[1].body).toBe("the recap");
	expect(new Date(msgs[0].at).toISOString()).toBe(msgs[0].at);
	// lastRun updated (ISO) + the run artifact saved.
	const updated = store.getRoutine("a1", routine.id);
	expect(updated?.lastRun).toBeTruthy();
	const arts = store.listArtifacts("a1");
	expect(arts).toHaveLength(1);
	expect(arts[0]).toMatchObject({ type: "md", title: "monday-recap-run-1.md", producedBy: "Monday recap", content: "the recap" });
});

test("no matching tool -> the authored tool is persisted, then run", async () => {
	const store = tmpStore();
	const { routine } = store.createRoutine("a1", "Fresh", "do the brand new thing", "Every hour");
	const tool: Tool = { name: "brandNewThing", title: "Brand new thing", purpose: "p", inputs: [], code: "async function brandNewThing() { return 'made it'; }" };
	const r = await runRoutine("a1", routine.id, { store, author: () => Promise.resolve({ tool }) });
	expect(r?.status).toBe("ok");
	expect(r?.outcome).toBe("made it");
	const tools = store.listTools("a1");
	expect(tools).toHaveLength(1);
	expect(tools[0]).toMatchObject({ name: "brandNewThing", version: 1 });
});

test("default authoring path (buildTool stub) stays offline and persists", async () => {
	const store = tmpStore();
	const { routine } = store.createRoutine("a1", "Recap", "Summarize last week's pipeline movement", "Every Monday 9:00");
	const r = await runRoutine("a1", routine.id, { store }); // no author injected -> buildTool stub (TOOL_AUTHOR_MODEL unset)
	expect(r?.status).toBe("ok");
	expect(store.listTools("a1").map((t) => t.name)).toEqual(["weeklyPipelineSummary"]);
});

test("SEND-GATE: a gated routine records needs_approval — it never auto-sends", async () => {
	const store = tmpStore();
	let sent = 0;
	const capabilities: CapabilityTree = {
		nex: {
			send: () => {
				sent += 1;
				return "sent";
			},
		},
	};
	store.upsertTool("a1", {
		name: "pingSales",
		title: "Ping sales",
		purpose: "p",
		inputs: [],
		code: "async function pingSales() { return nex.send('#sales', 'hi'); }",
	});
	const { routine } = store.createRoutine("a1", "Ping", "run pingSales", "Every hour");
	const r = await runRoutine("a1", routine.id, { store, capabilities });
	expect(r?.status).toBe("needs_approval");
	expect(r?.outcome).toContain("paused for your approval:");
	expect(sent).toBe(0); // the send NEVER executed
	// The paused outcome still lands in the transcript and as an artifact.
	const msgs = store.getSession("a1", routine.sessionId)?.messages ?? [];
	expect(msgs[1].body).toContain("paused for your approval:");
	expect(store.listArtifacts("a1")[0].content).toContain("paused for your approval:");
});

test("an error outcome is recorded too, and run numbering increments", async () => {
	const store = tmpStore();
	store.upsertTool("a1", {
		name: "boomTool",
		title: "Boom tool",
		purpose: "p",
		inputs: [],
		code: "async function boomTool() { throw new Error('kaput'); }",
	});
	const { routine } = store.createRoutine("a1", "Boom", "run boomTool", "Every hour");
	const r1 = await runRoutine("a1", routine.id, { store });
	expect(r1?.status).toBe("error");
	expect(r1?.outcome).toContain("kaput");
	await runRoutine("a1", routine.id, { store });
	expect(store.listArtifacts("a1").map((a) => a.title)).toEqual(["boom-run-1.md", "boom-run-2.md"]);
});

test("unknown routine id -> null (nothing written)", async () => {
	const store = tmpStore();
	expect(await runRoutine("a1", "ghost", { store })).toBeNull();
	expect(store.listArtifacts("a1")).toEqual([]);
});
