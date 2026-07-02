// File-backed per-agent persistence: tools and artifacts, one JSON file per
// agent id under a data dir. Chat sessions live in pi's own session format
// (sessions.ts / PiSessions); routines live in the BROKER's scheduler registry
// (cron, revisions, run history) — neither is stored here.
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
import type { StoredArtifact, StoredTool, Tool } from "./wire.js";

export interface AgentData {
	tools: StoredTool[];
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
	return { tools: [], artifacts: [] };
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
		// Tolerate older/partial files: missing sections read as empty (a file
		// from the pre-rework store may also carry routines/sessions keys —
		// ignored here; the broker and pi sessions own those now).
		return {
			tools: Array.isArray(parsed.tools) ? parsed.tools : [],
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
