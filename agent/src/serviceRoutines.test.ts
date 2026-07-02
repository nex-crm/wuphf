// Service-level coverage for the persistence routes (routines slice 2):
// /tools/build persistence, /tools, /routines (+patch/run), /sessions
// (+message), /artifacts. Offline: the store points at a tmp dir, the build
// engine is stubbed, authoring is the deterministic stub (TOOL_AUTHOR_MODEL
// unset), and tool runs use the simulated capability tree — never a live
// model or broker.

import { afterAll, beforeAll, expect, test } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "./service.js";
import { AgentStore } from "./store.js";
import type { Routine, SessionMessage, SessionMeta, StoredArtifact, StoredTool, WorkflowSpec } from "./wire.js";

async function* fakeBuild() {
	yield {
		type: "spec" as const,
		spec: { name: "n", tool_id: "t", narration: "", clarify: null, steps: [] } as WorkflowSpec,
	};
}

let tmp: string;
let server: ReturnType<typeof createServer>;
let base: string;
beforeAll(() => {
	tmp = mkdtempSync(join(tmpdir(), "wuphf-agent-svc-"));
	server = createServer({ port: 0, buildStream: fakeBuild, store: new AgentStore(join(tmp, "data")) });
	base = server.url.toString().replace(/\/$/, "");
});
afterAll(() => {
	server.stop(true);
	rmSync(tmp, { recursive: true, force: true });
});

function post(path: string, body: unknown, method = "POST"): Promise<Response> {
	return fetch(`${base}${path}`, { method, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
}

test("POST /tools/build with app persists the tool; same name bumps version", async () => {
	const r1 = (await (await post("/tools/build", { schema_version: 1, message: "summarize the weekly pipeline", app: "sales" })).json()) as {
		tool: StoredTool;
	};
	expect(r1.tool.name).toBe("weeklyPipelineSummary");
	expect(r1.tool.version).toBe(1);
	const r2 = (await (await post("/tools/build", { schema_version: 1, message: "weekly pipeline digest", app: "sales" })).json()) as {
		tool: StoredTool;
	};
	expect(r2.tool.version).toBe(2);
	// Persisted and listable per agent.
	const listed = (await (await fetch(`${base}/tools?agent=sales`)).json()) as { tools: StoredTool[] };
	expect(listed.tools).toHaveLength(1);
	expect(listed.tools[0].version).toBe(2);
	// No app -> authored but NOT persisted (and no version on the response tool).
	const r3 = (await (await post("/tools/build", { schema_version: 1, message: "draft a follow-up email" })).json()) as {
		tool: StoredTool;
	};
	expect(r3.tool.name).toBe("draftFollowup");
	expect(r3.tool.version).toBeUndefined();
	expect(((await (await fetch(`${base}/tools?agent=sales`)).json()) as { tools: StoredTool[] }).tools).toHaveLength(1);
});

test("routine lifecycle: create (+session), patch draft, publish, disable", async () => {
	const created = (await (
		await post("/routines", { schema_version: 1, agent: "ops", name: "Monday recap", prompt: "run weeklyPipelineSummary", schedule: "Every Monday 9:00" })
	).json()) as { routine: Routine };
	const rt = created.routine;
	expect(rt).toMatchObject({ agent: "ops", name: "Monday recap", enabled: true, version: 1 });
	expect(rt.sessionId).toBeTruthy();
	// The routine's chat session exists: kind "routine", title = name.
	const sessions = (await (await fetch(`${base}/sessions?agent=ops`)).json()) as { sessions: SessionMeta[] };
	expect(sessions.sessions).toEqual([expect.objectContaining({ id: rt.sessionId, kind: "routine", title: "Monday recap" })]);
	// A prompt edit is a draft.
	const drafted = (await (await post(`/routines/${rt.id}`, { schema_version: 1, agent: "ops", prompt: "new prompt" }, "PATCH")).json()) as {
		routine: Routine;
	};
	expect(drafted.routine.draft).toBe(true);
	expect(drafted.routine.version).toBe(1);
	// Publish bumps the version and clears the draft flag.
	const published = (await (await post(`/routines/${rt.id}`, { schema_version: 1, agent: "ops", publish: true }, "PATCH")).json()) as {
		routine: Routine;
	};
	expect(published.routine.version).toBe(2);
	expect(published.routine.draft).toBeUndefined();
	// Disable.
	const disabled = (await (await post(`/routines/${rt.id}`, { schema_version: 1, agent: "ops", enabled: false }, "PATCH")).json()) as {
		routine: Routine;
	};
	expect(disabled.routine.enabled).toBe(false);
	const listed = (await (await fetch(`${base}/routines?agent=ops`)).json()) as { routines: Routine[] };
	expect(listed.routines).toEqual([expect.objectContaining({ id: rt.id, enabled: false, version: 2 })]);
});

test("POST /routines/<id>/run runs NOW (disabled + unscheduled included) and lands transcript + artifact", async () => {
	const { routine } = (await (
		await post("/routines", { schema_version: 1, agent: "runner", name: "Recap", prompt: "Summarize last week's pipeline movement", schedule: "Every Monday 9:00" })
	).json()) as { routine: Routine };
	const res = await post(`/routines/${routine.id}/run`, { schema_version: 1, agent: "runner" });
	expect(res.status).toBe(200);
	const ran = (await res.json()) as { routine: Routine; session: SessionMeta };
	expect(ran.routine.lastRun).toBeTruthy();
	expect(ran.session.id).toBe(routine.sessionId);
	// Transcript persisted.
	const session = (await (await fetch(`${base}/sessions/${routine.sessionId}?agent=runner`)).json()) as {
		session: SessionMeta;
		messages: SessionMessage[];
	};
	expect(session.messages.map((m) => m.from)).toEqual(["you", "nex"]);
	expect(session.messages[0].body).toBe("(scheduled) Summarize last week's pipeline movement");
	// The stub-authored tool was persisted, and the run artifact saved.
	const tools = (await (await fetch(`${base}/tools?agent=runner`)).json()) as { tools: StoredTool[] };
	expect(tools.tools.map((t) => t.name)).toEqual(["weeklyPipelineSummary"]);
	const arts = (await (await fetch(`${base}/artifacts?agent=runner`)).json()) as { artifacts: StoredArtifact[] };
	expect(arts.artifacts).toEqual([expect.objectContaining({ type: "md", title: "recap-run-1.md", producedBy: "Recap" })]);
	// Unknown routine -> 404.
	expect((await post("/routines/ghost/run", { schema_version: 1, agent: "runner" })).status).toBe(404);
});

test("manual sessions: default Chat <n> titles, transcript append, 404s", async () => {
	const s1 = (await (await post("/sessions", { schema_version: 1, agent: "chatter" })).json()) as { session: SessionMeta };
	expect(s1.session).toMatchObject({ kind: "manual", title: "Chat 1", agent: "chatter" });
	const s2 = (await (await post("/sessions", { schema_version: 1, agent: "chatter", title: "Named" })).json()) as { session: SessionMeta };
	expect(s2.session.title).toBe("Named");
	// Append-only transcript mirroring (FE chat stays client-side for now).
	const m1 = await post(`/sessions/${s1.session.id}/message`, { schema_version: 1, agent: "chatter", from: "you", body: "hi" });
	expect(((await m1.json()) as { ok: boolean }).ok).toBe(true);
	await post(`/sessions/${s1.session.id}/message`, { schema_version: 1, agent: "chatter", from: "nex", body: "hello" });
	const got = (await (await fetch(`${base}/sessions/${s1.session.id}?agent=chatter`)).json()) as { messages: SessionMessage[] };
	expect(got.messages.map((m) => `${m.from}:${m.body}`)).toEqual(["you:hi", "nex:hello"]);
	// 404s: unknown session for both GET and message-append.
	expect((await fetch(`${base}/sessions/ghost?agent=chatter`)).status).toBe(404);
	expect((await post("/sessions/ghost/message", { schema_version: 1, agent: "chatter", from: "you", body: "x" })).status).toBe(404);
});

test("validation ladder: bad JSON, bad shape, schema mismatch, missing agent", async () => {
	// JSON parse guard.
	const notJson = await fetch(`${base}/routines`, { method: "POST", headers: { "content-type": "application/json" }, body: "{nope" });
	expect(notJson.status).toBe(400);
	// Shape guard: null/array/missing fields.
	for (const bad of [null, [], {}, { agent: "a" }, { agent: "a", name: "n", prompt: "p" }]) {
		expect((await post("/routines", bad)).status).toBe(400);
	}
	// Path-traversal-shaped agent ids are rejected at the boundary.
	expect((await post("/routines", { schema_version: 1, agent: "..", name: "n", prompt: "p", schedule: "Every hour" })).status).toBe(400);
	// schema_version guard.
	expect((await post("/routines", { schema_version: 99, agent: "a", name: "n", prompt: "p", schedule: "Every hour" })).status).toBe(400);
	expect((await post("/sessions", { schema_version: 99, agent: "a" })).status).toBe(400);
	// PATCH field guards.
	expect((await post("/routines/x", { schema_version: 1, agent: "a", enabled: "yes" }, "PATCH")).status).toBe(400);
	expect((await post("/routines/x", { schema_version: 1, agent: "a", publish: 1 }, "PATCH")).status).toBe(400);
	// PATCH unknown id -> 404.
	expect((await post("/routines/ghost", { schema_version: 1, agent: "a", enabled: false }, "PATCH")).status).toBe(404);
	// Message shape guard.
	expect((await post("/sessions/x/message", { schema_version: 1, agent: "a", from: "them", body: "x" })).status).toBe(400);
	expect((await post("/sessions/x/message", { schema_version: 1, agent: "a", from: "you" })).status).toBe(400);
	// GET routes require a usable agent param.
	for (const path of ["/tools", "/routines", "/sessions", "/artifacts", "/tools?agent=..", "/sessions?agent=%20"]) {
		expect((await fetch(`${base}${path}`)).status).toBe(400);
	}
});

test("GET routes read empty for an unknown agent (no 500s from a missing file)", async () => {
	expect(((await (await fetch(`${base}/tools?agent=nobody`)).json()) as { tools: unknown[] }).tools).toEqual([]);
	expect(((await (await fetch(`${base}/routines?agent=nobody`)).json()) as { routines: unknown[] }).routines).toEqual([]);
	expect(((await (await fetch(`${base}/sessions?agent=nobody`)).json()) as { sessions: unknown[] }).sessions).toEqual([]);
	expect(((await (await fetch(`${base}/artifacts?agent=nobody`)).json()) as { artifacts: unknown[] }).artifacts).toEqual([]);
});
