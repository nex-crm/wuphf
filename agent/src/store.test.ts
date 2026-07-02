import { afterAll, expect, test } from "bun:test";
import { existsSync, mkdtempSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { AgentStore, defaultDataDir, sanitizeAgentId } from "./store.js";
import type { Tool } from "./wire.js";

const dirs: string[] = [];
function tmpStore(): { store: AgentStore; dir: string } {
	const dir = join(mkdtempSync(join(tmpdir(), "wuphf-agent-store-")), "data");
	dirs.push(dir);
	return { store: new AgentStore(dir), dir };
}
afterAll(() => {
	for (const dir of dirs) rmSync(join(dir, ".."), { recursive: true, force: true });
});

const TOOL: Tool = { name: "weeklyPipelineSummary", title: "Weekly pipeline summary", purpose: "p", inputs: [], code: "async function weeklyPipelineSummary() { return 'x'; }" };

test("WUPHF_AGENT_DATA_DIR drives the default data dir", () => {
	expect(defaultDataDir({ WUPHF_AGENT_DATA_DIR: "/tmp/somewhere" })).toBe("/tmp/somewhere");
	// Unset -> the package-relative default.
	expect(defaultDataDir({})).toContain(".wuphf-agent-data");
});

test("agent ids sanitize path separators and reject dot-only ids", () => {
	expect(sanitizeAgentId("sales-copilot")).toBe("sales-copilot");
	expect(sanitizeAgentId("../../etc/passwd")).toBe(".._.._etc_passwd");
	expect(sanitizeAgentId("a/b\\c")).toBe("a_b_c");
	expect(() => sanitizeAgentId("")).toThrow("invalid agent id");
	expect(() => sanitizeAgentId("..")).toThrow("invalid agent id");
	expect(() => sanitizeAgentId("   ")).toThrow("invalid agent id");
});

test("a traversal-shaped agent id stays INSIDE the data dir", () => {
	const { store, dir } = tmpStore();
	store.upsertTool("../escape", TOOL);
	const files = readdirSync(dir);
	expect(files).toEqual([".._escape.json"]);
});

test("the data dir is created lazily on first save (loads read empty before)", () => {
	const { store, dir } = tmpStore();
	expect(existsSync(dir)).toBe(false);
	expect(store.listTools("a1")).toEqual([]); // no dir yet -> empty, no throw
	expect(store.agents()).toEqual([]);
	store.upsertTool("a1", TOOL);
	expect(existsSync(dir)).toBe(true);
	expect(store.agents()).toEqual(["a1"]);
});

test("upsertTool persists with version 1, re-authoring the same name bumps it", () => {
	const { store } = tmpStore();
	const v1 = store.upsertTool("a1", TOOL);
	expect(v1.version).toBe(1);
	const v2 = store.upsertTool("a1", { ...TOOL, purpose: "updated" });
	expect(v2.version).toBe(2);
	const tools = store.listTools("a1");
	expect(tools).toHaveLength(1);
	expect(tools[0].purpose).toBe("updated");
	// A DIFFERENT name is a new tool at version 1.
	expect(store.upsertTool("a1", { ...TOOL, name: "other" }).version).toBe(1);
	expect(store.listTools("a1")).toHaveLength(2);
});

test("agents are isolated per file", () => {
	const { store } = tmpStore();
	store.upsertTool("a1", TOOL);
	expect(store.listTools("a2")).toEqual([]);
});





test("artifacts persist with generated id + ISO at", () => {
	const { store } = tmpStore();
	const a = store.addArtifact("a1", { type: "md", title: "r-run-1.md", producedBy: "R", content: "out" });
	expect(a.id.startsWith("art_")).toBe(true);
	expect(new Date(a.at).toISOString()).toBe(a.at);
	expect(store.listArtifacts("a1")).toHaveLength(1);
});

test("data survives a fresh store instance over the same dir (atomic file write)", () => {
	const { store, dir } = tmpStore();
	store.upsertTool("a1", TOOL);
	store.addArtifact("a1", { type: "md", title: "r-run-1.md", producedBy: "R", content: "out" });
	const reopened = new AgentStore(dir);
	expect(reopened.listTools("a1")).toHaveLength(1);
	expect(reopened.listArtifacts("a1")).toHaveLength(1);
	// No stray tmp file left behind.
	expect(readdirSync(dir).filter((f) => f.endsWith(".tmp"))).toEqual([]);
});
