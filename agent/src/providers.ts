// Provider detection for the FE Settings surface. Reports which inference paths
// are usable — subscription CLIs the operator is already logged into, BYOK keys,
// and local Ollama — so the FE can show what's connected and prompt /login or BYOK
// for the rest. No secrets are returned, only availability.

import { existsSync } from "node:fs";

export interface Provider {
	id: string;
	label: string;
	available: boolean;
	via: "subscription_cli" | "api_key" | "local" | "none";
}

function onPath(bin: string): boolean {
	const dirs = (process.env.PATH ?? "").split(":");
	return dirs.some((d) => d && existsSync(`${d}/${bin}`));
}

function envAny(...names: string[]): boolean {
	return names.some((n) => (process.env[n] ?? "").trim().length > 0);
}

export function detectProviders(): Provider[] {
	const out: Provider[] = [];

	if (envAny("ANTHROPIC_API_KEY")) out.push({ id: "anthropic", label: "Anthropic API", available: true, via: "api_key" });
	else if (onPath("claude")) out.push({ id: "anthropic", label: "Claude Code (subscription)", available: true, via: "subscription_cli" });
	else out.push({ id: "anthropic", label: "Anthropic", available: false, via: "none" });

	if (envAny("OPENAI_API_KEY")) out.push({ id: "openai", label: "OpenAI API", available: true, via: "api_key" });
	else if (onPath("codex")) out.push({ id: "codex", label: "Codex (subscription)", available: true, via: "subscription_cli" });

	out.push({ id: "ollama", label: "Ollama (local / open-weight)", available: onPath("ollama"), via: onPath("ollama") ? "local" : "none" });

	return out;
}

export function providersPayload(): { providers: Provider[]; any_available: boolean } {
	const providers = detectProviders();
	return { providers, any_available: providers.some((p) => p.available) };
}
