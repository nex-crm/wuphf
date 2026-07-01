// File-backed per-agent persistence (routines slice 2): tools, routines, chat
// sessions, and artifacts, one JSON file per agent id under a data dir.
//
//   - Data dir: WUPHF_AGENT_DATA_DIR, default agent/.wuphf-agent-data/ relative
//     to this package. Created lazily on first save.
//   - Writes are atomic-ish: write <file>.tmp, then rename over the target.
//   - Agent ids are sanitized into safe filenames (path separators and other
//     unsafe characters normalize to "_"; empty / dot-only ids are rejected).
//   - A missing file reads as the empty shape; a CORRUPT file throws (loading
//     it as empty would clobber the operator's data on the next save).

import { mkdirSync, readdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { Routine, SessionMessage, SessionMeta, StoredArtifact, StoredTool, Tool } from "./wire.js";

export interface AgentData {
	tools: StoredTool[];
	routines: Routine[];
	sessions: { meta: SessionMeta[]; messages: Record<string, SessionMessage[]> };
	artifacts: StoredArtifact[];
}

const PACKAGE_DEFAULT_DIR = join(dirname(fileURLToPath(import.meta.url)), "..", ".wuphf-agent-data");

export function defaultDataDir(env: Record<string, string | undefined> = process.env): string {
	return env.WUPHF_AGENT_DATA_DIR?.trim() || PACKAGE_DEFAULT_DIR;
}

/** Normalize an agent id into a safe filename stem. Rejects ids that would
 * escape the data dir (path separators normalize to "_"; "."/".."/empty throw). */
export function sanitizeAgentId(agent: string): string {
	const safe = agent.trim().replace(/[^A-Za-z0-9._-]+/g, "_");
	if (!safe || /^\.+$/.test(safe)) throw new Error(`invalid agent id: ${JSON.stringify(agent)}`);
	return safe;
}

function emptyData(): AgentData {
	return { tools: [], routines: [], sessions: { meta: [], messages: {} }, artifacts: [] };
}

function newId(prefix: string): string {
	return `${prefix}_${crypto.randomUUID().slice(0, 8)}`;
}

function isEnoent(e: unknown): boolean {
	return e instanceof Error && (e as NodeJS.ErrnoException).code === "ENOENT";
}

export class AgentStore {
	constructor(private readonly dir: string = defaultDataDir()) {}

	private fileFor(agent: string): string {
		return join(this.dir, `${sanitizeAgentId(agent)}.json`);
	}

	/** Every agent id with a data file (used by the scheduler's sweep). */
	agents(): string[] {
		try {
			return readdirSync(this.dir)
				.filter((f) => f.endsWith(".json"))
				.map((f) => f.slice(0, -".json".length));
		} catch (e) {
			if (isEnoent(e)) return []; // data dir not created yet
			throw e;
		}
	}

	load(agent: string): AgentData {
		let raw: string;
		try {
			raw = readFileSync(this.fileFor(agent), "utf8");
		} catch (e) {
			if (isEnoent(e)) return emptyData();
			throw e;
		}
		const parsed = JSON.parse(raw) as Partial<AgentData>;
		// Tolerate older/partial files: missing sections read as empty.
		return {
			tools: Array.isArray(parsed.tools) ? parsed.tools : [],
			routines: Array.isArray(parsed.routines) ? parsed.routines : [],
			sessions: {
				meta: Array.isArray(parsed.sessions?.meta) ? parsed.sessions.meta : [],
				messages: parsed.sessions?.messages && typeof parsed.sessions.messages === "object" ? parsed.sessions.messages : {},
			},
			artifacts: Array.isArray(parsed.artifacts) ? parsed.artifacts : [],
		};
	}

	save(agent: string, data: AgentData): void {
		mkdirSync(this.dir, { recursive: true }); // lazy dir creation
		const file = this.fileFor(agent);
		const tmp = `${file}.tmp`;
		writeFileSync(tmp, JSON.stringify(data, null, 2));
		renameSync(tmp, file); // atomic-ish: readers never see a half-written file
	}

	// -------------------------------------------------------------------------
	// Tools
	// -------------------------------------------------------------------------

	listTools(agent: string): StoredTool[] {
		return this.load(agent).tools;
	}

	/** Persist an authored tool. A same-named tool is replaced with version+1. */
	upsertTool(agent: string, tool: Tool): StoredTool {
		const data = this.load(agent);
		const existing = data.tools.find((t) => t.name === tool.name);
		const stored: StoredTool = { ...tool, version: existing ? existing.version + 1 : 1 };
		const tools = existing ? data.tools.map((t) => (t.name === tool.name ? stored : t)) : [...data.tools, stored];
		this.save(agent, { ...data, tools });
		return stored;
	}

	// -------------------------------------------------------------------------
	// Routines
	// -------------------------------------------------------------------------

	listRoutines(agent: string): Routine[] {
		return this.load(agent).routines;
	}

	getRoutine(agent: string, id: string): Routine | null {
		return this.load(agent).routines.find((r) => r.id === id) ?? null;
	}

	/** Create a routine plus its chat session (kind "routine", title = name). */
	createRoutine(agent: string, name: string, prompt: string, schedule: string, now: Date = new Date()): { routine: Routine; session: SessionMeta } {
		const data = this.load(agent);
		const session: SessionMeta = { id: newId("sess"), agent, title: name, kind: "routine", at: now.toISOString() };
		const routine: Routine = { id: newId("rt"), agent, name, prompt, schedule, enabled: true, version: 1, sessionId: session.id };
		this.save(agent, {
			...data,
			routines: [...data.routines, routine],
			sessions: { meta: [...data.sessions.meta, session], messages: { ...data.sessions.messages, [session.id]: [] } },
		});
		return { routine, session };
	}

	/** Apply an immutable patch to a routine; null when the id is unknown. */
	updateRoutine(agent: string, id: string, patch: (r: Routine) => Routine): Routine | null {
		const data = this.load(agent);
		const existing = data.routines.find((r) => r.id === id);
		if (!existing) return null;
		const updated = patch(existing);
		this.save(agent, { ...data, routines: data.routines.map((r) => (r.id === id ? updated : r)) });
		return updated;
	}

	// -------------------------------------------------------------------------
	// Sessions
	// -------------------------------------------------------------------------

	listSessions(agent: string): SessionMeta[] {
		return this.load(agent).sessions.meta;
	}

	getSession(agent: string, id: string): { session: SessionMeta; messages: SessionMessage[] } | null {
		const data = this.load(agent);
		const session = data.sessions.meta.find((s) => s.id === id);
		if (!session) return null;
		return { session, messages: data.sessions.messages[id] ?? [] };
	}

	/** New manual session; default title "Chat <n>" over the manual-session count. */
	createSession(agent: string, title?: string, now: Date = new Date()): SessionMeta {
		const data = this.load(agent);
		const n = data.sessions.meta.filter((s) => s.kind === "manual").length + 1;
		const session: SessionMeta = { id: newId("sess"), agent, title: title?.trim() || `Chat ${n}`, kind: "manual", at: now.toISOString() };
		this.save(agent, {
			...data,
			sessions: { meta: [...data.sessions.meta, session], messages: { ...data.sessions.messages, [session.id]: [] } },
		});
		return session;
	}

	/** Append to a session's transcript (append-only). False when the session is unknown. */
	appendMessage(agent: string, sessionId: string, message: SessionMessage): boolean {
		const data = this.load(agent);
		if (!data.sessions.meta.some((s) => s.id === sessionId)) return false;
		const existing = data.sessions.messages[sessionId] ?? [];
		this.save(agent, {
			...data,
			sessions: { ...data.sessions, messages: { ...data.sessions.messages, [sessionId]: [...existing, message] } },
		});
		return true;
	}

	// -------------------------------------------------------------------------
	// Artifacts
	// -------------------------------------------------------------------------

	listArtifacts(agent: string): StoredArtifact[] {
		return this.load(agent).artifacts;
	}

	addArtifact(agent: string, artifact: Omit<StoredArtifact, "id" | "at">, now: Date = new Date()): StoredArtifact {
		const data = this.load(agent);
		const stored: StoredArtifact = { ...artifact, id: newId("art"), at: now.toISOString() };
		this.save(agent, { ...data, artifacts: [...data.artifacts, stored] });
		return stored;
	}
}
