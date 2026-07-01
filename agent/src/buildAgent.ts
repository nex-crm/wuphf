// The BUILD agent (pi-mono engine): plain-language description -> WorkflowSpec.
//
// Uses pi-ai's multi-provider `complete` for a structured spec call. The narrow
// BUILD task (compile a description into a deterministic spec) needs one good
// structured call, not the full tool-loop; the loop (gbrain/browsersniff tools)
// comes at the discovery slice on the same pi-mono stack.

import { complete, type Context, type Model, type StreamOptions } from "@mariozechner/pi-ai";
import { apiKeyFor, resolveModel } from "./model.js";
import { extractJson, SCHEMA_PROMPT, validateSpec, type WorkflowSpec } from "./wire.js";

// A stalled provider or a client that drops mid-build must not pin the request
// open forever — fall back to a hard cap when the caller passes no signal.
const DEFAULT_BUILD_TIMEOUT_MS = 60_000;

export interface BuildOptions {
	model?: Model<string>;
	toolId?: string;
	apiKey?: string;
	/** Caller's abort signal (e.g. the HTTP request's signal). Aborts the model call. */
	signal?: AbortSignal;
	/** Hard timeout for the model call; defaults to DEFAULT_BUILD_TIMEOUT_MS. */
	timeoutMs?: number;
	/** Override the pi-ai completion call in tests so they never hit a live model. */
	complete?: typeof complete;
}

function asError(reason: unknown): Error {
	if (reason instanceof Error) return reason;
	return new Error(typeof reason === "string" ? reason : "build aborted");
}

function textOf(content: { type: string; text?: string }[]): string {
	return content
		.filter((c) => c.type === "text" && typeof c.text === "string")
		.map((c) => c.text as string)
		.join("");
}

/** Compile a description into a validated WorkflowSpec via the pi-ai model layer. */
export async function buildWorkflow(message: string, opts: BuildOptions = {}): Promise<WorkflowSpec> {
	const model = opts.model ?? resolveModel();
	const completeFn = opts.complete ?? complete;
	const timeoutMs = opts.timeoutMs ?? DEFAULT_BUILD_TIMEOUT_MS;
	const ctx: Context = {
		systemPrompt: SCHEMA_PROMPT,
		messages: [{ role: "user", content: message.trim(), timestamp: Date.now() }],
	};

	// One signal that aborts on either the caller's signal or the timeout, whichever
	// fires first. Built by hand (not AbortSignal.any) so it works across runtimes.
	const ctrl = new AbortController();
	const onAbort = () => ctrl.abort(asError(opts.signal?.reason));
	if (opts.signal?.aborted) ctrl.abort(asError(opts.signal.reason));
	else opts.signal?.addEventListener("abort", onAbort, { once: true });
	const timer = setTimeout(() => ctrl.abort(new Error(`build timed out after ${timeoutMs}ms`)), timeoutMs);

	try {
		// Fail loud before spending a model call when we are already aborted.
		if (ctrl.signal.aborted) throw asError(ctrl.signal.reason);
		const res = await completeFn(model, ctx, {
			apiKey: opts.apiKey ?? apiKeyFor(model),
			signal: ctrl.signal,
		} satisfies StreamOptions);
		return validateSpec(extractJson(textOf(res.content as { type: string; text?: string }[])), opts.toolId);
	} finally {
		clearTimeout(timer);
		opts.signal?.removeEventListener("abort", onAbort);
	}
}

/** Stream the assembled steps (FE staggered reveal), then the full spec — the
 * /build/stream event contract. */
export async function* streamWorkflow(
	message: string,
	opts: BuildOptions = {},
): AsyncGenerator<{ type: "step"; step: WorkflowSpec["steps"][number] } | { type: "spec"; spec: WorkflowSpec }> {
	const spec = await buildWorkflow(message, opts);
	for (const step of spec.steps) yield { type: "step", step };
	yield { type: "spec", spec };
}
