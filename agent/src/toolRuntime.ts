// Sandboxed tool execution (host side): the app's chat CALLS a saved Tool
// (agent-authored by create_tool) and this runs its `code` in a SEPARATE WORKER
// (toolSandboxWorker.ts) against the host's capability runtime (capabilities.ts —
// simulated by default, real seams when configured; see that file).
//
// THE ISOLATE (slice 6, replacing the old in-process cooperative sandbox):
//   - The code executes in a Worker thread. The timeout HARD-KILLS it
//     (worker.terminate()), so a synchronous infinite loop dies at the deadline
//     instead of hanging the service.
//   - Capabilities are NOT given to the worker. Every capability call is an RPC
//     back to this host, where instrumentation records the action and the
//     SEND-GATE enforces default-deny — the tool code physically cannot reach an
//     integration, the broker token, or a model key except through this RPC.
//   - Residual ambient authority inside the worker (fetch etc.) is still reduced
//     by strict-mode parameter shadowing + a code scan (import/eval rejected).
//     TODO(security): a permissioned runtime for full authority stripping.
//
// SEND-GATE (CQ1, default deny): gated capabilities (`crm.assign`, `nex.send`,
// `nex.browser`) halt the run with status="needs_approval" unless the run carries
// approved=true — the FE renders the human approval card in the chat.

import { buildCapabilities } from "./capabilities.js";
import type { Tool, ToolCallGate } from "./wire.js";

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
	/** Wall-clock deadline; the worker is hard-killed when it elapses. */
	timeoutMs?: number;
	/** Override the host capability runtime (tests inject their own). */
	capabilities?: CapabilityTree;
}

export type ToolRunResult =
	| { status: "ok"; result: string; actions: string[] }
	| { status: "needs_approval"; gate: ToolCallGate; actions: string[] }
	| { status: "error"; detail: string; actions: string[] };

const DEFAULT_TIMEOUT_MS = 5000;

// Capabilities that mutate the outside world (or seize the operator's browser):
// halt for the approval card unless the run is approved. Keyed by dotted path so
// injected runtimes stay gated too.
const GATED = new Set(["crm.assign", "nex.send", "nex.browser"]);

// Globals shadowed as unused strict-mode parameters inside the worker compile.
// `eval`/`arguments` are not legal strict-mode parameter names and `import` is a
// keyword — those are handled by the code scan below instead.
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
	"postMessage",
	"self",
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

function preview(v: unknown): string {
	let s: string;
	try {
		s = v === undefined ? "undefined" : JSON.stringify(v);
	} catch {
		s = String(v);
	}
	return s.length > 60 ? `${s.slice(0, 57)}…` : s;
}

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

/** Human-readable gate copy for the approval card ("This will <detail>."). */
function gateDetail(path: string, args: unknown[]): string {
	if (path === "crm.assign") return `assign ${labelOf(args[0])} to ${labelOf(args[1])}`;
	if (path === "nex.send") return `send ${labelOf(args[1])} to ${labelOf(args[0])}`;
	if (path === "nex.browser") return `control your browser to ${labelOf(args[0])}`;
	return `run ${path}`;
}

// ---------------------------------------------------------------------------
// Host-side capability table: flatten the tree to dotted paths, instrumented.
// ---------------------------------------------------------------------------

interface HostCaps {
	paths: string[];
	rootNames: string[];
	invoke: (path: string, args: unknown[]) => Promise<unknown>;
}

function flatten(tree: CapabilityTree, prefix: string, into: Map<string, CapabilityFn>): void {
	for (const [key, node] of Object.entries(tree)) {
		const path = prefix ? `${prefix}.${key}` : key;
		if (typeof node === "function") into.set(path, node);
		else flatten(node, path, into);
	}
}

function hostCaps(tree: CapabilityTree, ctx: { actions: string[]; approved: boolean }): HostCaps {
	const table = new Map<string, CapabilityFn>();
	flatten(tree, "", table);
	return {
		paths: [...table.keys()],
		rootNames: Object.keys(tree),
		invoke: async (path, args) => {
			const fn = table.get(path);
			if (!fn) throw new Error(`unknown capability: ${path}`);
			ctx.actions.push(`${path}(${args.map(preview).join(", ")})`);
			// Default deny: a gated capability halts the run for the human approval
			// card unless this run carries approved=true.
			if (GATED.has(path) && !ctx.approved) throw new GateError(path, gateDetail(path, args));
			return await fn(...args);
		},
	};
}

// ---------------------------------------------------------------------------
// Compile + run (in the worker isolate)
// ---------------------------------------------------------------------------

/** Reject code the parameter-shadowing compile cannot contain (see header). */
function scanReject(code: string): string | null {
	if (/\bimport\s*\(/.test(code) || /^\s*import[\s"']/m.test(code)) {
		return "tool code may not use import";
	}
	if (/\beval\s*\(/.test(code)) return "tool code may not use eval";
	return null;
}

interface WorkerCapMsg {
	t: "cap";
	id: number;
	path: string;
	args: unknown[];
}
interface WorkerDoneMsg {
	t: "done";
	result: string;
}
interface WorkerErrMsg {
	t: "err";
	detail: string;
}
type WorkerMsg = WorkerCapMsg | WorkerDoneMsg | WorkerErrMsg;

/** Drop non-cloneable values before a capability result crosses postMessage. */
function plain(v: unknown): unknown {
	try {
		return v === undefined ? undefined : JSON.parse(JSON.stringify(v));
	} catch {
		return preview(v);
	}
}

export async function runTool(tool: Tool, args: Record<string, string> = {}, opts: ToolRunOptions = {}): Promise<ToolRunResult> {
	const ctx = { actions: [] as string[], approved: opts.approved === true };

	const rejected = scanReject(tool.code);
	if (rejected) return { status: "error", detail: rejected, actions: ctx.actions };

	const fnName = /(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)/.exec(tool.code)?.[1];
	if (!fnName) {
		return { status: "error", detail: "tool code must declare a named function", actions: ctx.actions };
	}

	const caps = hostCaps(opts.capabilities ?? buildCapabilities(), ctx);

	// Declared inputs become parameters bound to the (string) args. Reject names
	// that would break or subvert the parameter list.
	const inputNames = tool.inputs.map((i) => i.name);
	const reserved = new Set<string>([...caps.rootNames, ...SHADOWED_GLOBALS]);
	for (const name of inputNames) {
		if (!IDENT.test(name) || reserved.has(name)) {
			return { status: "error", detail: `invalid tool input name: ${JSON.stringify(name)}`, actions: ctx.actions };
		}
	}

	const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
	const worker = new Worker(new URL("./toolSandboxWorker.ts", import.meta.url).href);

	return await new Promise<ToolRunResult>((resolve) => {
		let settled = false;
		const finish = (r: ToolRunResult) => {
			if (settled) return;
			settled = true;
			clearTimeout(timer);
			worker.terminate();
			resolve(r);
		};
		// The HARD KILL: a sync infinite loop in tool code dies here, at the
		// deadline — worker.terminate() stops the thread, not just the waiting.
		const timer = setTimeout(() => finish({ status: "error", detail: `tool timed out after ${timeoutMs}ms`, actions: ctx.actions }), timeoutMs);

		worker.onmessage = (ev: MessageEvent) => {
			const msg = ev.data as WorkerMsg;
			if (msg.t === "done") {
				finish({ status: "ok", result: msg.result, actions: ctx.actions });
			} else if (msg.t === "err") {
				finish({ status: "error", detail: msg.detail, actions: ctx.actions });
			} else if (msg.t === "cap") {
				void caps
					.invoke(msg.path, msg.args)
					.then((value) => {
						if (!settled) worker.postMessage({ t: "capres", id: msg.id, ok: true, value: plain(value) });
					})
					.catch((e: unknown) => {
						if (e instanceof GateError) {
							finish({ status: "needs_approval", gate: { capability: e.capability, detail: e.detail }, actions: ctx.actions });
							return;
						}
						if (!settled) {
							worker.postMessage({ t: "capres", id: msg.id, ok: false, detail: e instanceof Error ? e.message : String(e) });
						}
					});
			}
		};
		worker.onerror = (ev: ErrorEvent) => {
			finish({ status: "error", detail: ev.message || "tool worker crashed", actions: ctx.actions });
		};

		worker.postMessage({
			t: "run",
			code: tool.code,
			fnName,
			inputNames,
			argValues: inputNames.map((n) => args[n] ?? ""),
			capPaths: caps.paths,
			shadowedGlobals: [...SHADOWED_GLOBALS],
		});
	});
}
