// Routine execution (routines slice 2): running a routine = what the chat does
// with a prompt. Match one of the agent's persisted tools (light title/name
// word-overlap heuristic, mirroring the FE chat's matchTool); if nothing
// matches, author a new tool (create_tool) and persist it, then run it.
//
// SEND-GATE (hard rule): a routine run ALWAYS executes with approved: false —
// default deny. A gated capability records needs_approval into the transcript
// ("paused for your approval: …"); a scheduled run never auto-sends.
//
// Every run appends to the routine's session transcript, updates lastRun, and
// ALWAYS saves the outcome as an "md" artifact (<kebab-name>-run-<n>.md).

import { buildCapabilities, capabilityConfigFromEnv } from "./capabilities.js";
import type { AgentStore } from "./store.js";
import { type CapabilityTree, runTool, type ToolRunResult } from "./toolRuntime.js";
import { buildTool } from "./tools.js";
import type { Routine, SessionMeta, StoredTool, Tool } from "./wire.js";

export interface RoutineRunnerDeps {
	store: AgentStore;
	/** Host capability tree; defaults to the env-composed runtime. */
	capabilities?: CapabilityTree;
	/** Tool authoring seam; defaults to buildTool (stub unless TOOL_AUTHOR_MODEL=1). */
	author?: (prompt: string) => Promise<{ tool: Tool | null }>;
	/** Tool execution seam; defaults to runTool (tests inject a recorder). */
	execute?: typeof runTool;
	now?: () => Date;
}

export interface RoutineRunOutcome {
	routine: Routine;
	session: SessionMeta;
	status: ToolRunResult["status"];
	outcome: string;
}

/** Light title/name word-overlap match (ported from the FE chat's matchTool,
 * minus the run/call/use cue — a routine prompt is already imperative). An
 * exact mention of the callable name or full title wins; otherwise the
 * strongest title-word overlap, requiring at least two words so one generic
 * word cannot hijack the prompt. */
export function matchTool(prompt: string, tools: readonly StoredTool[]): StoredTool | null {
	const lower = prompt.toLowerCase();
	const exact = tools.find((t) => lower.includes(t.name.toLowerCase()) || lower.includes(t.title.toLowerCase()));
	if (exact) return exact;
	let best: StoredTool | null = null;
	let bestScore = 0;
	for (const t of tools) {
		const words = t.title
			.toLowerCase()
			.split(/[^a-z0-9]+/)
			.filter((w) => w.length > 2);
		const score = words.filter((w) => lower.includes(w)).length;
		if (score > bestScore) {
			best = t;
			bestScore = score;
		}
	}
	return bestScore >= 2 ? best : null;
}

/** "Monday pipeline recap" -> "monday-pipeline-recap". */
export function kebab(name: string): string {
	return (
		name
			.toLowerCase()
			.replace(/[^a-z0-9]+/g, "-")
			.replace(/^-+|-+$/g, "") || "routine"
	);
}

function outcomeText(r: ToolRunResult): string {
	if (r.status === "ok") return r.result;
	if (r.status === "needs_approval") return `paused for your approval: ${r.gate.detail}`;
	return r.detail;
}

/** Run a routine NOW (the scheduler and POST /routines/<id>/run both land here).
 * Null when the routine id is unknown for this agent. */
export async function runRoutine(agent: string, routineId: string, deps: RoutineRunnerDeps): Promise<RoutineRunOutcome | null> {
	const { store } = deps;
	const routine = store.getRoutine(agent, routineId);
	if (!routine) return null;
	const now = deps.now ?? (() => new Date());

	// 1. A persisted tool that matches the prompt, else author + persist one.
	let tool: Tool | null = matchTool(routine.prompt, store.listTools(agent));
	if (!tool) {
		const author = deps.author ?? ((p: string) => buildTool(p, { tryModel: process.env.TOOL_AUTHOR_MODEL === "1" }));
		const authored = (await author(routine.prompt)).tool;
		tool = authored ? store.upsertTool(agent, authored) : null;
	}

	// 2. Run it — approved: false ALWAYS (default deny; see header).
	let status: ToolRunResult["status"];
	let outcome: string;
	if (tool) {
		const execute = deps.execute ?? runTool;
		const capabilities = deps.capabilities ?? buildCapabilities(capabilityConfigFromEnv());
		const timeoutMs = Number(process.env.TOOL_CALL_TIMEOUT_MS) || undefined;
		const r = await execute(tool, {}, { approved: false, capabilities, timeoutMs });
		status = r.status;
		outcome = outcomeText(r);
	} else {
		status = "error";
		outcome = "no tool could be authored for this routine";
	}

	// 3. Transcript: the scheduled prompt in, the outcome back.
	const at = now().toISOString();
	store.appendMessage(agent, routine.sessionId, { from: "you", body: `(scheduled) ${routine.prompt}`, at });
	store.appendMessage(agent, routine.sessionId, { from: "nex", body: outcome, at: now().toISOString() });

	// 4. lastRun + the run artifact (always, whatever the outcome).
	const updated = store.updateRoutine(agent, routineId, (r) => ({ ...r, lastRun: at })) ?? routine;
	const n = store.listArtifacts(agent).filter((a) => a.producedBy === routine.name).length + 1;
	store.addArtifact(
		agent,
		{
			type: "md",
			title: `${kebab(routine.name)}-run-${n}.md`,
			producedBy: routine.name,
			content: outcome,
			size: `${new TextEncoder().encode(outcome).length} B`,
		},
		now(),
	);

	const session = store.getSession(agent, routine.sessionId)?.session ?? {
		id: routine.sessionId,
		agent,
		title: routine.name,
		kind: "routine" as const,
		at,
	};
	return { routine: updated, session, status, outcome };
}
