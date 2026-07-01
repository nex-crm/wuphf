// The routine scheduler (routines slice 2): an interval tick that sweeps every
// agent's routines and runs the due, ENABLED ones via routineRunner (which
// always runs approved: false — a scheduled run never auto-sends).
//
// OFF BY DEFAULT: createServer starts it only when ROUTINE_SCHEDULER=1, so
// tests and plain dev never spin an interval. Tick cadence: ROUTINE_TICK_MS
// (default 30_000ms). The timer is unref'd so it can never pin the process.
//
// Schedule labels -> due logic. SIMPLIFICATION (documented): this is not cron.
//   - Interval labels ("Every 30 minutes", "Every hour", "Every N hours") are
//     due when now - lastRun >= the interval (or never ran).
//   - Time-of-day labels ("Every day 18:00", "Weekdays 8:00", "Every Monday
//     9:00") are due once per matching day, on the first tick at/after HH:MM,
//     when the routine has not run since that day's occurrence. If the process
//     is down across the whole matching day, that occurrence is skipped (no
//     backfill), and a weekly label missed on its weekday waits for the next
//     one.
//   - An unrecognized label is never due (the routine still runs via
//     POST /routines/<id>/run).

import { runRoutine, type RoutineRunnerDeps } from "./routineRunner.js";
import type { AgentStore } from "./store.js";
import type { Routine } from "./wire.js";

const DEFAULT_TICK_MS = 30_000;

const WEEKDAY_INDEX: Record<string, number> = {
	sunday: 0,
	monday: 1,
	tuesday: 2,
	wednesday: 3,
	thursday: 4,
	friday: 5,
	saturday: 6,
};

function intervalDue(last: Date | null, now: Date, ms: number): boolean {
	return !last || now.getTime() - last.getTime() >= ms;
}

/** Due when today matches, now >= today's HH:MM, and the routine has not run
 * since that occurrence. */
function timeOfDayDue(last: Date | null, now: Date, hh: number, mm: number, dayOk: (d: Date) => boolean): boolean {
	if (!dayOk(now)) return false;
	const occurrence = new Date(now);
	occurrence.setHours(hh, mm, 0, 0);
	if (now.getTime() < occurrence.getTime()) return false;
	return !last || last.getTime() < occurrence.getTime();
}

/** Is this routine due at `now`? A due routine must be enabled. */
export function isDue(routine: Routine, now: Date): boolean {
	if (!routine.enabled) return false;
	const last = routine.lastRun ? new Date(routine.lastRun) : null;
	if (last && Number.isNaN(last.getTime())) return false; // corrupt lastRun: hold, do not thrash
	const label = routine.schedule.trim().toLowerCase();

	const everyMinutes = /^every\s+(\d+)\s+minutes?$/.exec(label);
	if (everyMinutes) return intervalDue(last, now, Number(everyMinutes[1]) * 60_000);
	if (label === "every hour") return intervalDue(last, now, 3_600_000);
	const everyHours = /^every\s+(\d+)\s+hours?$/.exec(label);
	if (everyHours) return intervalDue(last, now, Number(everyHours[1]) * 3_600_000);

	const daily = /^every\s+day\s+(\d{1,2}):(\d{2})$/.exec(label);
	if (daily) return timeOfDayDue(last, now, Number(daily[1]), Number(daily[2]), () => true);
	const weekdays = /^weekdays\s+(\d{1,2}):(\d{2})$/.exec(label);
	if (weekdays) {
		return timeOfDayDue(last, now, Number(weekdays[1]), Number(weekdays[2]), (d) => d.getDay() >= 1 && d.getDay() <= 5);
	}
	const weekly = /^every\s+(sunday|monday|tuesday|wednesday|thursday|friday|saturday)\s+(\d{1,2}):(\d{2})$/.exec(label);
	if (weekly) {
		return timeOfDayDue(last, now, Number(weekly[2]), Number(weekly[3]), (d) => d.getDay() === WEEKDAY_INDEX[weekly[1]]);
	}

	return false; // unknown label — never auto-due
}

export interface TickDeps {
	store: AgentStore;
	/** How a due routine runs; injectable so tests trigger without the interval. */
	run: (agent: string, routineId: string) => Promise<unknown>;
}

/** One sweep: run every due routine across every agent. Returns the ids that
 * ran. One routine's failure must not starve the rest of the sweep. */
export async function tick(deps: TickDeps, now: Date = new Date()): Promise<string[]> {
	const ran: string[] = [];
	for (const agent of deps.store.agents()) {
		for (const routine of deps.store.listRoutines(agent)) {
			if (!isDue(routine, now)) continue;
			try {
				await deps.run(agent, routine.id);
				ran.push(routine.id);
			} catch (e) {
				console.error(`routine ${routine.id} (${agent}) failed:`, e);
			}
		}
	}
	return ran;
}

export interface SchedulerHandle {
	stop(): void;
}

export function startScheduler(deps: RoutineRunnerDeps, opts: { tickMs?: number } = {}): SchedulerHandle {
	const envTick = Number(process.env.ROUTINE_TICK_MS);
	const tickMs = opts.tickMs ?? (Number.isFinite(envTick) && envTick > 0 ? envTick : DEFAULT_TICK_MS);
	const run = (agent: string, routineId: string) => runRoutine(agent, routineId, deps);
	const timer = setInterval(() => {
		void tick({ store: deps.store, run }).catch((e: unknown) => console.error("routine tick failed:", e));
	}, tickMs);
	// Never pin the process open on the scheduler alone (tests must exit cleanly
	// even if the env leaks ROUTINE_SCHEDULER=1).
	timer.unref?.();
	return { stop: () => clearInterval(timer) };
}
