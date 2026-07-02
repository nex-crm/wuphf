// PiSessions coverage: transcripts persist as pi SessionManager JSONL trees.
// Offline and file-local — a tmp data dir per test run.

import { afterAll, expect, test } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { PiSessions } from "./sessions.js";

const dirs: string[] = [];
function tmpSessions(): { sessions: PiSessions; dataDir: string } {
	const dir = mkdtempSync(join(tmpdir(), "wuphf-agent-pisess-"));
	dirs.push(dir);
	const dataDir = join(dir, "data");
	return { sessions: new PiSessions(dataDir), dataDir };
}
afterAll(() => {
	for (const dir of dirs) rmSync(dir, { recursive: true, force: true });
});

test("create -> list -> get round-trips title, kind, and transcript", async () => {
	const { sessions } = tmpSessions();
	const created = sessions.create("a1", "Monday recap", "routine", "rt-monday");
	expect(created.kind).toBe("routine");

	await sessions.append("a1", created.id, { from: "you", body: "(scheduled) run it", at: new Date().toISOString() });
	await sessions.append("a1", created.id, { from: "nex", body: "the recap", at: new Date().toISOString() });

	const listed = await sessions.list("a1");
	expect(listed.map((s) => s.id)).toContain(created.id);
	expect(listed.find((s) => s.id === created.id)?.title).toBe("Monday recap");

	const got = await sessions.get("a1", created.id);
	expect(got?.session.kind).toBe("routine");
	expect(got?.messages.map((m) => [m.from, m.body])).toEqual([
		["you", "(scheduled) run it"],
		["nex", "the recap"],
	]);
});

test("agents are isolated; unknown ids read null/false", async () => {
	const { sessions } = tmpSessions();
	const a = sessions.create("a1", "Chat 1", "manual");
	expect(await sessions.list("a2")).toEqual([]);
	expect(await sessions.get("a2", a.id)).toBeNull();
	expect(await sessions.append("a2", a.id, { from: "you", body: "x", at: new Date().toISOString() })).toBe(false);
});

test("ensureRoutineSession is stable per slug (post-flush) and re-labels on rename", async () => {
	const { sessions } = tmpSessions();
	const first = await sessions.ensureRoutineSession("a1", "rt-1", "Monday recap");
	// pi semantics: a session file flushes on its first exchange. Every routine
	// run writes one (runRoutine), so ensure after a run finds the session.
	await sessions.append("a1", first.id, { from: "you", body: "(scheduled) run", at: new Date().toISOString() });
	await sessions.append("a1", first.id, { from: "nex", body: "done", at: new Date().toISOString() });

	const again = await sessions.ensureRoutineSession("a1", "rt-1", "Monday recap");
	expect(again.id).toBe(first.id);

	const renamed = await sessions.ensureRoutineSession("a1", "rt-1", "Weekly recap");
	expect(renamed.id).toBe(first.id);
	const got = await sessions.get("a1", first.id);
	expect(got?.session.title).toBe("Weekly recap");

	const other = await sessions.ensureRoutineSession("a1", "rt-2", "Other routine");
	expect(other.id).not.toBe(first.id);
});

test("a never-used session lists live in-process but does not survive a restart", async () => {
	const { sessions, dataDir } = tmpSessions();
	const created = sessions.create("a1", "Empty chat", "manual");
	// Live in this process (the FE can see the chat it just opened) …
	expect((await sessions.list("a1")).map((s) => s.id)).toContain(created.id);
	// … but never flushed, so a fresh instance over the same dir drops it.
	const restarted = new PiSessions(dataDir);
	expect((await restarted.list("a1")).map((s) => s.id)).not.toContain(created.id);
});
