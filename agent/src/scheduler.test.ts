import { afterAll, expect, test } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { isDue, startScheduler, tick } from "./scheduler.js";
import { AgentStore } from "./store.js";
import type { Routine } from "./wire.js";

const dirs: string[] = [];
function tmpStore(): AgentStore {
	const dir = mkdtempSync(join(tmpdir(), "wuphf-agent-sched-"));
	dirs.push(dir);
	return new AgentStore(join(dir, "data"));
}
afterAll(() => {
	for (const dir of dirs) rmSync(dir, { recursive: true, force: true });
});

function routine(schedule: string, over: Partial<Routine> = {}): Routine {
	return { id: "rt_x", agent: "a1", name: "R", prompt: "p", schedule, enabled: true, version: 1, sessionId: "sess_x", ...over };
}

// Local-time anchors (the scheduler works in host-local time).
// 2026-07-01 is a Wednesday; 2026-07-06 is a Monday.
const WED_9 = new Date(2026, 6, 1, 9, 0, 0);
const WED_7 = new Date(2026, 6, 1, 7, 0, 0);
const SAT_9 = new Date(2026, 6, 4, 9, 0, 0);
const MON_9_01 = new Date(2026, 6, 6, 9, 1, 0);
const MON_8_59 = new Date(2026, 6, 6, 8, 59, 0);

test("interval labels: due when never run or the interval elapsed", () => {
	expect(isDue(routine("Every 30 minutes"), WED_9)).toBe(true); // never ran
	const ranRecently = new Date(WED_9.getTime() - 10 * 60_000).toISOString();
	expect(isDue(routine("Every 30 minutes", { lastRun: ranRecently }), WED_9)).toBe(false);
	const ranAgo = new Date(WED_9.getTime() - 31 * 60_000).toISOString();
	expect(isDue(routine("Every 30 minutes", { lastRun: ranAgo }), WED_9)).toBe(true);
	expect(isDue(routine("Every hour", { lastRun: ranAgo }), WED_9)).toBe(false);
	expect(isDue(routine("Every hour", { lastRun: new Date(WED_9.getTime() - 61 * 60_000).toISOString() }), WED_9)).toBe(true);
	expect(isDue(routine("Every 2 hours", { lastRun: new Date(WED_9.getTime() - 61 * 60_000).toISOString() }), WED_9)).toBe(false);
});

test("Weekdays 8:00: due after 8 on a weekday, once per day, never weekends", () => {
	const r = routine("Weekdays 8:00");
	expect(isDue(r, WED_9)).toBe(true); // Wed 9:00, never ran
	expect(isDue(r, WED_7)).toBe(false); // before 8:00
	expect(isDue(r, SAT_9)).toBe(false); // Saturday
	// Already ran after today's occurrence -> not due again today.
	const ranToday = new Date(2026, 6, 1, 8, 0, 30).toISOString();
	expect(isDue(routine("Weekdays 8:00", { lastRun: ranToday }), WED_9)).toBe(false);
	// Ran yesterday -> due again today at/after 8.
	const ranYesterday = new Date(2026, 5, 30, 8, 0, 30).toISOString();
	expect(isDue(routine("Weekdays 8:00", { lastRun: ranYesterday }), WED_9)).toBe(true);
});

test("Every day 18:00: due when now >= today's 18:00 and not run since", () => {
	const evening = new Date(2026, 6, 1, 18, 30, 0);
	expect(isDue(routine("Every day 18:00"), evening)).toBe(true);
	expect(isDue(routine("Every day 18:00"), WED_9)).toBe(false);
	const ranTonight = new Date(2026, 6, 1, 18, 5, 0).toISOString();
	expect(isDue(routine("Every day 18:00", { lastRun: ranTonight }), evening)).toBe(false);
});

test("Every Monday 9:00: only Mondays, at/after 9, once", () => {
	const r = routine("Every Monday 9:00");
	expect(isDue(r, MON_9_01)).toBe(true);
	expect(isDue(r, MON_8_59)).toBe(false);
	expect(isDue(r, WED_9)).toBe(false); // not Monday
	const ranThisMonday = new Date(2026, 6, 6, 9, 0, 30).toISOString();
	expect(isDue(routine("Every Monday 9:00", { lastRun: ranThisMonday }), MON_9_01)).toBe(false);
	const ranLastMonday = new Date(2026, 5, 29, 9, 0, 30).toISOString();
	expect(isDue(routine("Every Monday 9:00", { lastRun: ranLastMonday }), MON_9_01)).toBe(true);
});

test("a due routine must be enabled; unknown labels are never auto-due", () => {
	expect(isDue(routine("Every 30 minutes", { enabled: false }), WED_9)).toBe(false);
	expect(isDue(routine("whenever mercury is in retrograde"), WED_9)).toBe(false);
	expect(isDue(routine("Every 30 minutes", { lastRun: "not-a-date" }), WED_9)).toBe(false);
});

test("tick sweeps every agent and runs only the due+enabled routines", async () => {
	const store = tmpStore();
	const { routine: due } = store.createRoutine("a1", "Due", "p", "Every 30 minutes");
	const { routine: off } = store.createRoutine("a1", "Off", "p", "Every 30 minutes");
	store.updateRoutine("a1", off.id, (r) => ({ ...r, enabled: false }));
	const { routine: other } = store.createRoutine("a2", "Other", "p", "Every hour");
	const ran: string[] = [];
	const runs = await tick(
		{
			store,
			run: (agent, id) => {
				ran.push(`${agent}:${id}`);
				return Promise.resolve();
			},
		},
		WED_9,
	);
	expect(ran.sort()).toEqual([`a1:${due.id}`, `a2:${other.id}`].sort());
	expect(runs.sort()).toEqual([due.id, other.id].sort());
});

test("one routine's failure does not starve the rest of the sweep", async () => {
	const store = tmpStore();
	const { routine: bad } = store.createRoutine("a1", "Bad", "p", "Every hour");
	const { routine: good } = store.createRoutine("a1", "Good", "p", "Every hour");
	const runs = await tick(
		{
			store,
			run: (_agent, id) => (id === bad.id ? Promise.reject(new Error("boom")) : Promise.resolve()),
		},
		WED_9,
	);
	expect(runs).toEqual([good.id]);
});

test("startScheduler ticks on its interval and stop() halts it cleanly", async () => {
	const store = tmpStore();
	store.createRoutine("a1", "Fast", "run nothing that matches so buildTool stubs", "Every 30 minutes");
	// Exercise the real interval wiring with the real runner (offline: simulated
	// capabilities, stub authoring) at a tiny tick, then STOP — the suite exiting
	// cleanly is itself the no-leaked-interval assertion.
	const handle = startScheduler({ store, author: () => Promise.resolve({ tool: null }) }, { tickMs: 20 });
	try {
		const deadline = Date.now() + 2000;
		while (!store.getRoutine("a1", store.listRoutines("a1")[0].id)?.lastRun && Date.now() < deadline) {
			await new Promise((r) => setTimeout(r, 10));
		}
	} finally {
		handle.stop();
	}
	expect(store.listRoutines("a1")[0].lastRun).toBeTruthy();
});
