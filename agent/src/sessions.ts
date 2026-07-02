// pi-backed chat sessions (routines rework): transcripts persist as pi
// SessionManager JSONL trees instead of a hand-rolled store. One session dir
// per agent under <dataDir>/sessions/<agent>/; each session file is pi's
// native format, so anything pi provides later (resume, branching, compaction)
// applies to these sessions unchanged.
//
// Wire mapping (SessionMeta / SessionMessage stay the FE shapes):
//   - "you"  -> a pi UserMessage entry
//   - "nex"  -> a pi AssistantMessage entry stamped provider "wuphf" with zero
//     usage — the agent's reply, produced by the tool runtime rather than an
//     LLM API. A future real pi agent turn appends the same shape natively.
//   - kind ("routine"|"manual") + the owning routine slug ride a custom entry
//     (customType SESSION_META) appended at creation.
//
// pi's OWN persistence semantics apply: a session file is flushed once the
// first assistant message lands, so a chat nobody ever ran does not litter the
// sessions dir — and does not appear in list() after a restart. The FE keeps
// a just-created empty session in memory until its first exchange.

import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { SessionManager, type SessionEntry } from "@earendil-works/pi-coding-agent";
import { sanitizeAgentId } from "./store.js";
import type { SessionMessage, SessionMeta } from "./wire.js";

const SESSION_META = "wuphf-session-meta";

interface SessionMetaDetails {
	kind: "routine" | "manual";
	/** Scheduler slug of the owning routine (routine sessions only). */
	routine?: string;
}

export class PiSessions {
	/** One live SessionManager per session this process has touched. Keeps a
	 * just-created session reachable BEFORE pi's first flush writes its file,
	 * and guarantees a single writer per file within the process. */
	private readonly live = new Map<string, SessionManager>();

	constructor(private readonly dataDir: string) {}

	private dirFor(agent: string): string {
		const dir = join(this.dataDir, "sessions", sanitizeAgentId(agent));
		mkdirSync(dir, { recursive: true });
		return dir;
	}

	private liveKey(agent: string, id: string): string {
		return `${sanitizeAgentId(agent)}/${id}`;
	}

	/** New persisted session. Title lands as the pi session name. */
	create(agent: string, title: string, kind: "routine" | "manual", routineSlug?: string): SessionMeta {
		const dir = this.dirFor(agent);
		const sm = SessionManager.create(dir, dir);
		sm.appendSessionInfo(title);
		const details: SessionMetaDetails = routineSlug ? { kind, routine: routineSlug } : { kind };
		sm.appendCustomEntry(SESSION_META, details);
		this.live.set(this.liveKey(agent, sm.getSessionId()), sm);
		return { id: sm.getSessionId(), agent, title, kind, at: new Date().toISOString(), routine: routineSlug };
	}

	/** All sessions for an agent, newest first. Persisted sessions plus any
	 * live-in-this-process ones pi has not flushed yet (first exchange pending),
	 * so a just-created chat stays listable until it either flushes or the
	 * process restarts (pi semantics: an empty chat does not survive). */
	async list(agent: string): Promise<SessionMeta[]> {
		const dir = this.dirFor(agent);
		const infos = await SessionManager.list(dir, dir);
		const out = infos.map((info) => {
			const sm = SessionManager.open(info.path, dir);
			const meta = readMetaDetails(sm.getEntries());
			return {
				id: info.id,
				agent,
				title: info.name ?? sm.getSessionName() ?? "Chat",
				kind: meta?.kind ?? "manual",
				at: info.created.toISOString(),
				routine: meta?.routine,
			} satisfies SessionMeta;
		});
		const seen = new Set(out.map((s) => s.id));
		const prefix = `${sanitizeAgentId(agent)}/`;
		for (const [key, sm] of this.live) {
			if (!key.startsWith(prefix) || seen.has(sm.getSessionId())) continue;
			const meta = readMetaDetails(sm.getEntries());
			out.push({
				id: sm.getSessionId(),
				agent,
				title: sm.getSessionName() ?? "Chat",
				kind: meta?.kind ?? "manual",
				at: new Date().toISOString(),
				routine: meta?.routine,
			});
		}
		return out.sort((a, b) => (a.at < b.at ? 1 : -1));
	}

	/** One session + its transcript; null when the id is unknown for the agent. */
	async get(agent: string, id: string): Promise<{ session: SessionMeta; messages: SessionMessage[] } | null> {
		const opened = await this.open(agent, id);
		if (!opened) return null;
		const { sm, meta } = opened;
		return { session: meta, messages: sm.getEntries().flatMap(entryToMessage) };
	}

	/** Append a chat message; false when the session id is unknown. */
	async append(agent: string, id: string, msg: SessionMessage): Promise<boolean> {
		const opened = await this.open(agent, id);
		if (!opened) return false;
		const { sm } = opened;
		const timestamp = Date.parse(msg.at) || Date.now();
		if (msg.from === "you") {
			sm.appendMessage({ role: "user", content: msg.body, timestamp });
		} else {
			// The agent's reply as a real AssistantMessage (open provider union):
			// produced by the tool runtime, so usage/cost are honest zeros.
			sm.appendMessage({
				role: "assistant",
				content: [{ type: "text", text: msg.body }],
				api: "wuphf",
				provider: "wuphf",
				model: "tool-runner",
				usage: {
					input: 0,
					output: 0,
					cacheRead: 0,
					cacheWrite: 0,
					totalTokens: 0,
					cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
				},
				stopReason: "stop",
				timestamp,
			});
		}
		return true;
	}

	/** The session a routine's runs land in — found by its scheduler slug, or
	 * created on first run. Re-labels the session when the routine was renamed. */
	async ensureRoutineSession(agent: string, routineSlug: string, name: string): Promise<SessionMeta> {
		const prefix = `${sanitizeAgentId(agent)}/`;
		// A live (possibly not-yet-flushed) session for this routine wins first.
		for (const [key, sm] of this.live) {
			if (!key.startsWith(prefix)) continue;
			const meta = readMetaDetails(sm.getEntries());
			if (meta?.kind === "routine" && meta.routine === routineSlug) {
				if (name && sm.getSessionName() !== name) sm.appendSessionInfo(name);
				return { id: sm.getSessionId(), agent, title: name, kind: "routine", at: new Date().toISOString(), routine: routineSlug };
			}
		}
		const dir = this.dirFor(agent);
		const infos = await SessionManager.list(dir, dir);
		for (const info of infos) {
			const opened = await this.open(agent, info.id);
			if (!opened) continue;
			const meta = readMetaDetails(opened.sm.getEntries());
			if (meta?.kind === "routine" && meta.routine === routineSlug) {
				if (name && opened.sm.getSessionName() !== name) opened.sm.appendSessionInfo(name);
				return { id: info.id, agent, title: name, kind: "routine", at: info.created.toISOString(), routine: routineSlug };
			}
		}
		return this.create(agent, name, "routine", routineSlug);
	}

	private async open(agent: string, id: string): Promise<{ sm: SessionManager; meta: SessionMeta } | null> {
		const key = this.liveKey(agent, id);
		const cached = this.live.get(key);
		if (cached) {
			const details = readMetaDetails(cached.getEntries());
			return {
				sm: cached,
				meta: {
					id,
					agent,
					title: cached.getSessionName() ?? "Chat",
					kind: details?.kind ?? "manual",
					at: new Date().toISOString(),
					routine: details?.routine,
				},
			};
		}
		const dir = this.dirFor(agent);
		const infos = await SessionManager.list(dir, dir);
		const info = infos.find((i) => i.id === id);
		if (!info) return null;
		const sm = SessionManager.open(info.path, dir);
		this.live.set(key, sm);
		const details = readMetaDetails(sm.getEntries());
		return {
			sm,
			meta: {
				id,
				agent,
				title: info.name ?? sm.getSessionName() ?? "Chat",
				kind: details?.kind ?? "manual",
				at: info.created.toISOString(),
				routine: details?.routine,
			},
		};
	}
}

function readMetaDetails(entries: SessionEntry[]): SessionMetaDetails | null {
	for (const e of entries) {
		if (e.type === "custom" && e.customType === SESSION_META) {
			return (e.data ?? null) as SessionMetaDetails | null;
		}
	}
	return null;
}

function textOf(content: unknown): string {
	if (typeof content === "string") return content;
	if (Array.isArray(content)) {
		return content
			.filter((p): p is { type: "text"; text: string } => !!p && (p as { type?: string }).type === "text")
			.map((p) => p.text)
			.join("\n");
	}
	return "";
}

function entryToMessage(e: SessionEntry): SessionMessage[] {
	if (e.type !== "message") return [];
	const m = e.message as { role?: string; content?: unknown };
	if (m.role === "user") return [{ from: "you", body: textOf(m.content), at: e.timestamp }];
	if (m.role === "assistant") return [{ from: "nex", body: textOf(m.content), at: e.timestamp }];
	return [];
}
