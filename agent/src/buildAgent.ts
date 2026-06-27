// The BUILD agent (pi-mono engine): plain-language description -> WorkflowSpec.
//
// Uses pi-ai's multi-provider `complete` for a structured spec call. The narrow
// BUILD task (compile a description into a deterministic spec) needs one good
// structured call, not the full tool-loop; the loop (gbrain/browsersniff tools)
// comes at the discovery slice on the same pi-mono stack.

import { complete, type Context, type Model } from "@mariozechner/pi-ai";
import { apiKeyFor, resolveModel } from "./model.js";
import { extractJson, SCHEMA_PROMPT, validateSpec, type WorkflowSpec } from "./wire.js";

export interface BuildOptions {
	model?: Model<string>;
	toolId?: string;
	apiKey?: string;
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
	const ctx: Context = {
		systemPrompt: SCHEMA_PROMPT,
		messages: [{ role: "user", content: message.trim(), timestamp: Date.now() }],
	};
	const res = await complete(model, ctx, { apiKey: opts.apiKey ?? apiKeyFor(model) } as never);
	return validateSpec(extractJson(textOf(res.content as { type: string; text?: string }[])), opts.toolId);
}

/** Stream the assembled steps (FE staggered reveal), then the full spec. Mirrors
 * the Python harness /build/stream event contract. */
export async function* streamWorkflow(
	message: string,
	opts: BuildOptions = {},
): AsyncGenerator<{ type: "step"; step: WorkflowSpec["steps"][number] } | { type: "spec"; spec: WorkflowSpec }> {
	const spec = await buildWorkflow(message, opts);
	for (const step of spec.steps) yield { type: "step", step };
	yield { type: "spec", spec };
}
