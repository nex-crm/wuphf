// Sandboxed tool execution: the app's chat CALLS a saved Tool (agent-authored by
// create_tool, agent/src/tools.ts) and this runs its `code` in a constrained scope
// against a simulated, deterministic capability runtime — mirroring how the
// executor simulates steps (executor.ts); real integrations come later.
//
// SEND-GATE (CQ1, default deny): the gated capabilities (`crm.assign`, `nex.send`)
// throw a GateError unless the run carries approved=true, halting the run with
// status="needs_approval" so the FE renders the human approval card — the same
// needs_approval pattern as executor.ts's gated steps.
//
// SANDBOX LIMITATION — a prototype boundary, NOT a security boundary yet. Only
// agent-authored tool code is executed (never operator-typed content), and the
// containment is best-effort:
//   - `new Function` compiles the code in strict mode with ONLY the allowed
//     bindings (capabilities + declared inputs) as parameters; dangerous globals
//     (fetch, process, require, globalThis, Bun, ...) are shadowed by undefined
//     parameters of the same name.
//   - `import` and `eval` cannot be shadowed as parameter names, so a code scan
//     rejects code containing them outright.
//   - The wall-clock timeout is COOPERATIVE (Promise.race): it stops waiting, it
//     does not kill the code. A synchronous infinite loop still hangs the worker
//     — known limitation, out of scope for this slice.
// TODO(security): replace with a real isolate (worker/subprocess with no ambient
// authority) when real integrations land; parameter shadowing does not stop
// prototype-chain escapes (e.g. via constructors).

import type { Tool, ToolCallGate, ToolCallResult } from "./wire.js";

/** One callable capability (e.g. crm.deals). Args/return are untyped on purpose:
 * tool code is agent-authored JS, not a typed consumer. */
export type CapabilityFn = (...args: unknown[]) => unknown;

/** A nested tree of capabilities: { nex: { ai: { score } }, crm: { deals } }. */
export interface CapabilityTree {
	[key: string]: CapabilityTree | CapabilityFn;
}

export interface ToolRunOptions {
	/** Human approval for gated capabilities. DEFAULT DENY. */
	approved?: boolean;
	/** Cooperative wall-clock timeout (see header comment). */
	timeoutMs?: number;
	/** Override the simulated runtime (tests inject their own capabilities). */
	capabilities?: CapabilityTree;
}

export type ToolRunResult =
	| { status: "ok"; result: string; actions: string[] }
	| { status: "needs_approval"; gate: ToolCallGate; actions: string[] }
	| { status: "error"; detail: string; actions: string[] };

const DEFAULT_TIMEOUT_MS = 5000;

// Capabilities that mutate the outside world: halt for the approval card unless
// the run is approved. Keyed by dotted path so injected runtimes stay gated too.
const GATED = new Set(["crm.assign", "nex.send"]);

// Globals shadowed as unused strict-mode parameters. `eval`/`arguments` are not
// legal strict-mode parameter names and `import` is a keyword — those are handled
// by the code scan below instead.
const SHADOWED_GLOBALS = [
	"fetch",
	"process",
	"require",
	"globalThis",
	"Bun",
	"Function",
	"setTimeout",
	"setInterval",
	"setImmediate",
	"XMLHttpRequest",
	"WebSocket",
] as const;

const IDENT = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

/** Thrown by a gated capability when the run is not approved. */
class GateError extends Error {
	constructor(
		readonly capability: string,
		readonly detail: string,
	) {
		super(`${capability} needs approval`);
	}
}

class TimeoutError extends Error {}

// ---------------------------------------------------------------------------
// Simulated, deterministic capability runtime (real integrations come later).
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

/** The default runtime: every capability is simulated and deterministic. */
export function simulatedCapabilities(): CapabilityTree {
	return {
		nex: {
			ai: {
				score: (subject: unknown) => hashScore(subject),
				summarize: (items: unknown) => {
					const list = Array.isArray(items) ? items : [items];
					const names = list.map(labelOf).slice(0, 3).join(", ");
					return `${list.length} item${list.length === 1 ? "" : "s"} — ${names || "nothing notable"} (simulated recap)`;
				},
				write: (kind: unknown) => `Drafted ${labelOf(kind)} — warm, brief, ready to review (simulated).`,
			},
			run: (input: unknown) => `Ran on ${labelOf(input)} (simulated).`,
			send: (target: unknown) => `Sent to ${labelOf(target)} (simulated).`,
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
// Instrumentation: record every capability call + enforce the send-gate.
// ---------------------------------------------------------------------------

/** Human-readable gate copy for the approval card ("This will <detail>."). */
function gateDetail(path: string, args: unknown[]): string {
	if (path === "crm.assign") return `assign ${labelOf(args[0])} to ${labelOf(args[1])}`;
	if (path === "nex.send") return `send ${labelOf(args[1])} to ${labelOf(args[0])}`;
	return `run ${path}`;
}

function instrument(tree: CapabilityTree, prefix: string, ctx: { actions: string[]; approved: boolean }): CapabilityTree {
	const out: CapabilityTree = {};
	for (const [key, node] of Object.entries(tree)) {
		const path = prefix ? `${prefix}.${key}` : key;
		if (typeof node === "function") {
			out[key] = (...args: unknown[]) => {
				ctx.actions.push(`${path}(${args.map(preview).join(", ")})`);
				// Default deny: a gated capability halts the run for the human
				// approval card unless this run carries approved=true.
				if (GATED.has(path) && !ctx.approved) throw new GateError(path, gateDetail(path, args));
				return node(...args);
			};
		} else {
			out[key] = instrument(node, path, ctx);
		}
	}
	return out;
}

// ---------------------------------------------------------------------------
// Compile + run
// ---------------------------------------------------------------------------

/** Reject code the parameter-shadowing sandbox cannot contain (see header). */
function scanReject(code: string): string | null {
	if (/\bimport\s*\(/.test(code) || /^\s*import[\s"']/m.test(code)) {
		return "tool code may not use import";
	}
	if (/\beval\s*\(/.test(code)) return "tool code may not use eval";
	return null;
}

export async function runTool(tool: Tool, args: Record<string, string> = {}, opts: ToolRunOptions = {}): Promise<ToolRunResult> {
	const ctx = { actions: [] as string[], approved: opts.approved === true };

	const rejected = scanReject(tool.code);
	if (rejected) return { status: "error", detail: rejected, actions: ctx.actions };

	const fnName = /(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)/.exec(tool.code)?.[1];
	if (!fnName) {
		return { status: "error", detail: "tool code must declare a named function", actions: ctx.actions };
	}

	const caps = instrument(opts.capabilities ?? simulatedCapabilities(), "", ctx);
	const capNames = Object.keys(caps);

	// Declared inputs become parameters bound to the (string) args. Reject names
	// that would break or subvert the parameter list.
	const inputNames = tool.inputs.map((i) => i.name);
	const reserved = new Set<string>([...capNames, ...SHADOWED_GLOBALS]);
	for (const name of inputNames) {
		if (!IDENT.test(name) || reserved.has(name)) {
			return { status: "error", detail: `invalid tool input name: ${JSON.stringify(name)}`, actions: ctx.actions };
		}
	}

	const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
	let timer: ReturnType<typeof setTimeout> | undefined;
	try {
		// Strict mode + ONLY the allowed bindings as parameters; dangerous globals
		// are shadowed by trailing undefined parameters (see header for limits).
		const compiled = new Function(
			...capNames,
			...inputNames,
			...SHADOWED_GLOBALS,
			`"use strict";\n${tool.code}\nreturn ${fnName}(${inputNames.join(", ")});`,
		);
		const invocation = Promise.resolve(
			compiled(...capNames.map((n) => caps[n]), ...inputNames.map((n) => args[n] ?? ""), ...SHADOWED_GLOBALS.map(() => undefined)),
		);
		// Cooperative timeout: stops WAITING after timeoutMs; it does not kill the
		// tool code (a sync infinite loop is out of scope — prototype boundary).
		const timeout = new Promise<never>((_, reject) => {
			timer = setTimeout(() => reject(new TimeoutError(`tool timed out after ${timeoutMs}ms`)), timeoutMs);
		});
		const value: unknown = await Promise.race([invocation, timeout]);
		const result = typeof value === "string" ? value : preview(value);
		return { status: "ok", result, actions: ctx.actions };
	} catch (e) {
		if (e instanceof GateError) {
			return { status: "needs_approval", gate: { capability: e.capability, detail: e.detail }, actions: ctx.actions };
		}
		const detail = e instanceof Error ? e.message : String(e);
		return { status: "error", detail, actions: ctx.actions };
	} finally {
		clearTimeout(timer);
	}
}
