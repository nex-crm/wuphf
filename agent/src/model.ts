// Model resolution for the BUILD agent, via pi-ai's multi-provider layer.
//
// Key-free first: in production the operator does a one-time pi `/login` (Claude
// Pro/Max, ChatGPT/Codex, or Copilot) and we resolve a subscription-OAuth model;
// for local/dev and the open-weight path we point pi-ai at a local Ollama model
// (no key, no login). BYOK (env API key) also works. This is the whole reason for
// pi-mono: one abstraction over subscription / key / open-weight.

import type { Model } from "@mariozechner/pi-ai";

export type Provider = "ollama" | "anthropic" | "codex";

/** A local Ollama model via pi-ai's OpenAI-compatible api. No key, no login. */
export function ollamaModel(id = process.env.HARNESS_MODEL ?? "qwen2.5-coder:1.5b"): Model<"openai-completions"> {
	return {
		id,
		name: `ollama:${id}`,
		api: "openai-completions",
		provider: "ollama",
		baseUrl: process.env.OLLAMA_BASE_URL ?? "http://localhost:11434/v1",
		reasoning: false,
		input: ["text"],
		cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
		contextWindow: 32768,
		maxTokens: 2048,
	};
}

/** The API key pi-ai should use for a model. Ollama needs only a placeholder. */
export function apiKeyFor(model: Model<string>): string | undefined {
	if (model.provider === "ollama") return "ollama";
	return undefined; // pi-ai resolves env keys / OAuth credentials itself
}

/**
 * Resolve the build model for the agent core. Defaults to Ollama (key-free) so
 * the agent runs out-of-the-box. Subscription providers (anthropic|codex) are
 * resolved by pi-ai's getModel + OAuth at the SERVICE layer, not here — so this
 * core path fails loud rather than silently substituting Ollama for a requested
 * subscription model (which would run the wrong engine without anyone noticing).
 */
export function resolveModel(provider: Provider = (process.env.HARNESS_PROVIDER as Provider) || "ollama"): Model<string> {
	if (provider === "ollama") return ollamaModel();
	throw new Error(`resolveModel: provider "${provider}" must be resolved at the service layer (pi-ai OAuth), not the agent core`);
}
