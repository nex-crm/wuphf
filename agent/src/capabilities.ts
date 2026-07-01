// The capability runtime behind tool execution (slice 6): what a tool's code can
// actually DO. Composed per host configuration:
//
//   nex.ai.score/summarize/write  REAL via one pi-ai `complete` each when a model
//                                 is enabled (TOOL_RUNTIME_MODEL=1); deterministic
//                                 simulation otherwise, and the fallback on any
//                                 model failure (never blocks a tool run).
//   integrations.call             REAL via the broker's Bridge v2 endpoint
//                                 (POST /apps/integrations/call) when
//                                 WUPHF_BROKER_URL + WUPHF_BROKER_TOKEN are set.
//                                 The broker classifies read-vs-mutate SERVER-SIDE:
//                                 reads execute against the connected integration;
//                                 a mutation is refused execution and raises the
//                                 human approval card instead — so this capability
//                                 is NOT double-gated at the agent layer.
//   nex.browser                   REAL via the broker's cua engine
//                                 (POST /execute/browser, SSE) when configured.
//                                 GATED at the agent layer (browser control needs
//                                 the operator's in-chat approval, mirroring the
//                                 browser-step reframe).
//   nex.send / crm.*              Still simulated (real sends/CRM go through
//                                 integrations.call); nex.send stays gated so the
//                                 approval flow is exercised end to end.
//
// Secrets discipline: the broker token comes from the agent's OWN environment and
// goes out only as an Authorization header to the configured broker — never to
// tool code (capabilities execute on the HOST side of the sandbox RPC; the worker
// only ever sees call results).

import { complete, type Context, type Model, type StreamOptions } from "@mariozechner/pi-ai";
import { apiKeyFor, resolveModel } from "./model.js";
import { deadlineSignal, textOf } from "./modelCall.js";
import { currentRunSignal } from "./runContext.js";
import type { CapabilityFn, CapabilityTree } from "./toolRuntime.js";

// THE SEND-GATE ALLOW-LIST (CQ1, default deny). Every capability that mutates
// the outside world or seizes the operator's browser MUST be listed here, keyed
// by dotted path — toolRuntime.ts default-ALLOWS anything not in this set, so a
// new mutating/side-effectful capability added below without an entry here ships
// ungated. Kept next to the capability definitions on purpose.
// (integrations.call is intentionally absent: the broker classifies
// read-vs-mutate server-side and raises its own approval card for mutations.)
export const GATED_CAPABILITIES: ReadonlySet<string> = new Set(["crm.assign", "nex.send", "nex.browser"]);

export interface CapabilityConfig {
	/** Broker base URL (e.g. http://127.0.0.1:7893) for integrations + browser. */
	brokerUrl?: string;
	/** Broker API token; sent as a Bearer header, never exposed to tool code. */
	brokerToken?: string;
	/** Model for real nex.ai.* calls; unset -> simulated. */
	aiModel?: Model<string>;
	apiKey?: string;
	/** Per-capability network/model call bound. */
	callTimeoutMs?: number;
	/** Test override for pi-ai completion. */
	complete?: typeof complete;
	/** Test override for HTTP (broker) calls. */
	fetch?: typeof fetch;
}

const DEFAULT_CAP_TIMEOUT_MS = 60_000;

// ---------------------------------------------------------------------------
// Simulated implementations (the default, and the fallback for real ones).
// ---------------------------------------------------------------------------

function labelOf(v: unknown): string {
	if (v == null) return "…";
	if (typeof v === "string") return v;
	if (typeof v === "object") {
		const o = v as Record<string, unknown>;
		if (typeof o.name === "string") return o.name;
		if (typeof o.title === "string") return o.title;
	}
	return preview(v);
}

function preview(v: unknown): string {
	let s: string;
	try {
		s = v === undefined ? "undefined" : JSON.stringify(v);
	} catch {
		s = String(v);
	}
	return s.length > 60 ? `${s.slice(0, 57)}…` : s;
}

/** Deterministic hash of the subject -> 55..95 (a plausible fit score). */
function hashScore(subject: unknown): number {
	const s = typeof subject === "string" ? subject : preview(subject);
	let h = 0;
	for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
	return 55 + (h % 41);
}

const DEALS = [
	{ name: "Globex", stage: "Negotiation", amount: 120_000, stageChanged: true },
	{ name: "Initech", stage: "Discovery", amount: 45_000, stageChanged: false },
	{ name: "Acme", stage: "Proposal", amount: 80_000, stageChanged: true },
	{ name: "Umbrella", stage: "Closed Won", amount: 96_000, stageChanged: true },
] as const;

function simSummarize(items: unknown): string {
	const list = Array.isArray(items) ? items : [items];
	const names = list.map(labelOf).slice(0, 3).join(", ");
	return `${list.length} item${list.length === 1 ? "" : "s"} — ${names || "nothing notable"} (simulated recap)`;
}

/** The all-simulated runtime: deterministic, no network, no model. */
export function simulatedCapabilities(): CapabilityTree {
	return {
		nex: {
			ai: {
				score: (subject: unknown) => hashScore(subject),
				summarize: (items: unknown) => simSummarize(items),
				write: (kind: unknown) => `Drafted ${labelOf(kind)} — warm, brief, ready to review (simulated).`,
			},
			run: (input: unknown) => `Ran on ${labelOf(input)} (simulated).`,
			send: (target: unknown) => `Sent to ${labelOf(target)} (simulated).`,
			browser: (goal: unknown) => `Would drive the browser: ${labelOf(goal)} (browser engine not configured).`,
		},
		crm: {
			deals: () => DEALS.map((d) => ({ ...d })),
			dealContext: (deal: unknown) => ({
				deal: labelOf(deal),
				stage: "Negotiation",
				lastTouch: "9 days ago",
				owner: "Priya (AE)",
			}),
			ownerFor: () => ({ name: "Priya (AE)" }),
			assign: (lead: unknown, ae: unknown) => `Assigned ${labelOf(lead)} to ${labelOf(ae)} (simulated).`,
		},
	};
}

// ---------------------------------------------------------------------------
// Real nex.ai.* — one structured pi-ai call each, simulated fallback on failure.
// ---------------------------------------------------------------------------

async function aiComplete(cfg: CapabilityConfig, system: string, user: string): Promise<string> {
	const model = cfg.aiModel;
	if (!model) throw new Error("no runtime model");
	const completeFn = cfg.complete ?? complete;
	// Compose the ambient RUN signal (runContext.ts) with the per-call timeout:
	// a settled tool run aborts this model call instead of leaving it burning.
	const deadline = deadlineSignal(currentRunSignal(), cfg.callTimeoutMs ?? DEFAULT_CAP_TIMEOUT_MS, {
		timeoutMessage: "ai capability timed out",
		abortFallback: "tool run settled",
	});
	try {
		const ctx: Context = {
			systemPrompt: system,
			messages: [{ role: "user", content: user, timestamp: Date.now() }],
		};
		const res = await completeFn(model, ctx, {
			apiKey: cfg.apiKey ?? apiKeyFor(model),
			signal: deadline.signal,
		} satisfies StreamOptions);
		return textOf(res.content as { type: string; text?: string }[]).trim();
	} finally {
		deadline.done();
	}
}

function realAI(cfg: CapabilityConfig): CapabilityTree {
	return {
		score: async (subject: unknown, opts?: unknown) => {
			try {
				const rubric = labelOf((opts as Record<string, unknown> | undefined)?.rubric ?? "fit");
				const out = await aiComplete(
					cfg,
					"You score business subjects 0-100. Output ONLY an integer, nothing else.",
					`Rubric: ${rubric}\nSubject: ${preview(subject)}\nScore 0-100:`,
				);
				const n = Number.parseInt(out.replace(/[^0-9]/g, " ").trim().split(/\s+/)[0] ?? "", 10);
				if (Number.isNaN(n)) throw new Error("non-numeric score");
				return Math.max(0, Math.min(100, n));
			} catch {
				return hashScore(subject);
			}
		},
		summarize: async (items: unknown, opts?: unknown) => {
			try {
				const style = labelOf((opts as Record<string, unknown> | undefined)?.style ?? "one glanceable line");
				return await aiComplete(
					cfg,
					`You summarize data for a busy operator. Style: ${style}. Output the summary text only.`,
					preview(items).slice(0, 4000),
				);
			} catch {
				return simSummarize(items);
			}
		},
		write: async (kind: unknown, opts?: unknown) => {
			try {
				const o = (opts ?? {}) as Record<string, unknown>;
				return await aiComplete(
					cfg,
					`You write a ${labelOf(kind)} for a busy operator. Tone: ${labelOf(o.tone ?? "warm, brief")}. Output the text only.`,
					`Context: ${preview(o.context ?? "none")}`,
				);
			} catch {
				return `Drafted ${labelOf(kind)} — warm, brief, ready to review (simulated).`;
			}
		},
	};
}

// ---------------------------------------------------------------------------
// Real integrations.call — the broker's Bridge v2 seam.
// ---------------------------------------------------------------------------

interface BrokerCallResponse {
	connected?: boolean;
	status?: string;
	request_id?: string;
	read_only?: boolean;
	result?: unknown;
	error?: string;
}

function realIntegrations(cfg: CapabilityConfig): CapabilityTree {
	const call: CapabilityFn = async (platform: unknown, action: unknown, params?: unknown) => {
		if (!cfg.brokerUrl || !cfg.brokerToken) {
			throw new Error("integrations are not connected on this host (set WUPHF_BROKER_URL and WUPHF_BROKER_TOKEN)");
		}
		const fetchFn = cfg.fetch ?? fetch;
		// Run signal + timeout in one signal: a settled tool run aborts the fetch.
		const deadline = deadlineSignal(currentRunSignal(), cfg.callTimeoutMs ?? DEFAULT_CAP_TIMEOUT_MS, {
			timeoutMessage: "integration call timed out",
			abortFallback: "tool run settled",
		});
		let body: BrokerCallResponse;
		try {
			const res = await fetchFn(`${cfg.brokerUrl.replace(/\/$/, "")}/apps/integrations/call`, {
				method: "POST",
				headers: {
					"content-type": "application/json",
					authorization: `Bearer ${cfg.brokerToken}`,
				},
				body: JSON.stringify({ platform: String(platform), action: String(action), params: params ?? {} }),
				signal: deadline.signal,
			});
			if (!res.ok) throw new Error(`integration call failed (${res.status})`);
			body = (await res.json()) as BrokerCallResponse;
		} finally {
			deadline.done();
		}
		if (body.error) throw new Error(body.error);
		if (body.connected === false) throw new Error(`${String(platform)} is not connected`);
		if (body.status === "needs_approval") {
			// The broker classified this as a MUTATION and raised the human approval
			// card itself; the tool reports that honestly instead of pretending it ran.
			return `Held for your approval in WUPHF (request ${body.request_id ?? "pending"}) — nothing was sent yet.`;
		}
		return body.result ?? "done";
	};
	return { call };
}

// ---------------------------------------------------------------------------
// Real nex.browser — the broker's cua engine, SSE. Gated at the agent layer.
// ---------------------------------------------------------------------------

function realBrowser(cfg: CapabilityConfig): CapabilityFn {
	return async (goal: unknown) => {
		if (!cfg.brokerUrl || !cfg.brokerToken) {
			throw new Error("browser execution is not configured on this host (set WUPHF_BROKER_URL and WUPHF_BROKER_TOKEN)");
		}
		const fetchFn = cfg.fetch ?? fetch;
		// Run signal + timeout in one signal, held open across the SSE STREAM (the
		// body read is the long part of a browser run): a settled tool run aborts
		// the in-flight request/stream instead of leaving Chrome driving unattended.
		const deadline = deadlineSignal(currentRunSignal(), cfg.callTimeoutMs ?? DEFAULT_CAP_TIMEOUT_MS, {
			timeoutMessage: "browser execution timed out",
			abortFallback: "tool run settled",
		});
		const actions: string[] = [];
		let result = "";
		try {
			const res = await fetchFn(`${cfg.brokerUrl.replace(/\/$/, "")}/execute/browser`, {
				method: "POST",
				headers: {
					"content-type": "application/json",
					authorization: `Bearer ${cfg.brokerToken}`,
				},
				body: JSON.stringify({ goal: String(goal) }),
				signal: deadline.signal,
			});
			if (!res.ok || !res.body) throw new Error(`browser execution failed (${res.status})`);
			// Parse the runner's SSE frames: collect action labels; `done` carries the
			// outcome; `error` fails the capability. (Sends inside the run pause in the
			// broker's own send-gate; unattended runs there default-deny.)
			const handleFrame = (frame: string) => {
				const data = frame
					.split("\n")
					.filter((l) => l.startsWith("data: "))
					.map((l) => l.slice(6))
					.join("");
				if (!data) return;
				let ev: { type?: string; label?: string; result?: string; message?: string };
				try {
					ev = JSON.parse(data) as typeof ev;
				} catch {
					return; // a non-JSON frame (e.g. the terminal `event: end` marker) — skip
				}
				if (ev.type === "action" && ev.label) actions.push(ev.label);
				if (ev.type === "done" && typeof ev.result === "string") result = ev.result;
				if (ev.type === "error") throw new Error(ev.message || "browser run failed");
			};
			const reader = res.body.getReader();
			const decoder = new TextDecoder();
			let buf = "";
			for (;;) {
				const { done, value } = await reader.read();
				if (done) break;
				buf += decoder.decode(value, { stream: true });
				let idx = buf.indexOf("\n\n");
				while (idx >= 0) {
					handleFrame(buf.slice(0, idx));
					buf = buf.slice(idx + 2);
					idx = buf.indexOf("\n\n");
				}
			}
			// Flush the tail: a final frame without a trailing blank line still counts.
			if (buf.trim()) handleFrame(buf);
		} finally {
			deadline.done();
		}
		const trace = actions.length ? ` (${actions.length} browser action${actions.length === 1 ? "" : "s"}: ${actions.slice(0, 3).join("; ")}${actions.length > 3 ? "; …" : ""})` : "";
		return `${result || "Browser run finished."}${trace}`;
	};
}

// ---------------------------------------------------------------------------
// Composition
// ---------------------------------------------------------------------------

/** Build the capability tree for this host: simulated base, real overlays where
 * configured. The tree shape is stable either way, so authored tool code runs on
 * any host — realness is a deployment property, not a code property. */
export function buildCapabilities(cfg: CapabilityConfig = {}): CapabilityTree {
	const tree = simulatedCapabilities();
	const nex = tree.nex as CapabilityTree;
	if (cfg.aiModel) nex.ai = realAI(cfg);
	if (cfg.brokerUrl && cfg.brokerToken) {
		nex.browser = realBrowser(cfg);
		tree.integrations = realIntegrations(cfg);
	} else {
		tree.integrations = {
			call: async () => {
				throw new Error("integrations are not connected on this host (set WUPHF_BROKER_URL and WUPHF_BROKER_TOKEN)");
			},
		};
	}
	return tree;
}

/** The service's config: real seams switch on via environment. */
export function capabilityConfigFromEnv(env: Record<string, string | undefined> = process.env): CapabilityConfig {
	const cfg: CapabilityConfig = {
		brokerUrl: env.WUPHF_BROKER_URL?.trim() || undefined,
		brokerToken: env.WUPHF_BROKER_TOKEN?.trim() || undefined,
	};
	if (env.TOOL_RUNTIME_MODEL === "1") {
		cfg.aiModel = resolveModel();
	}
	// Per-capability call bound follows the host's tool deadline knob; without
	// this, TOOL_CALL_TIMEOUT_MS raised the worker kill but capability calls
	// stayed capped at the fixed 60s default.
	const callTimeoutMs = Number(env.TOOL_CALL_TIMEOUT_MS);
	if (Number.isFinite(callTimeoutMs) && callTimeoutMs > 0) {
		cfg.callTimeoutMs = callTimeoutMs;
	}
	return cfg;
}
