import { afterEach, expect, test } from "bun:test";
import { detectProviders } from "./providers.js";

// Snapshot the env this suite mutates, restore after each case.
const ENV = { OPENAI_API_KEY: process.env.OPENAI_API_KEY, PATH: process.env.PATH };
afterEach(() => {
	if (ENV.OPENAI_API_KEY === undefined) delete process.env.OPENAI_API_KEY;
	else process.env.OPENAI_API_KEY = ENV.OPENAI_API_KEY;
	process.env.PATH = ENV.PATH;
});

test("the anthropic, codex, and ollama rows are always present (regression: CodeRabbit providers.ts:33)", () => {
	delete process.env.OPENAI_API_KEY;
	process.env.PATH = ""; // nothing on PATH -> codex would have been dropped before the fix
	const providers = detectProviders();
	expect(providers.map((p) => p.id)).toEqual(["anthropic", "codex", "ollama"]);
	expect(providers.find((p) => p.id === "codex")?.available).toBe(false); // disabled, but shown
});

test("an OpenAI key surfaces under the stable id 'codex', never 'openai'", () => {
	process.env.OPENAI_API_KEY = "fake-openai-key-for-test";
	process.env.PATH = "";
	const providers = detectProviders();
	const codex = providers.find((p) => p.id === "codex");
	expect(codex?.id).toBe("codex"); // resolveModel only accepts "codex"
	expect(codex?.available).toBe(true);
	expect(providers.some((p) => p.id === "openai")).toBe(false);
});
