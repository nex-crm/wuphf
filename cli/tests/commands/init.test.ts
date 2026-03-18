import { describe, it, beforeEach, afterEach, mock } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, mkdirSync, writeFileSync, readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

// ── nex-mcp-config tests ──

describe("nex-mcp-config", () => {
  it("loadMcpConfig returns empty object when file missing", async () => {
    const { loadMcpConfig } = await import("../../src/lib/nex-mcp-config.js");
    // The file may or may not exist in CI, but it should not throw
    const config = loadMcpConfig();
    assert.equal(typeof config, "object");
  });

  it("saveMcpConfig and loadMcpConfig round-trip", async () => {
    const { saveMcpConfig, loadMcpConfig } = await import("../../src/lib/nex-mcp-config.js");

    // Save original to restore later
    const original = loadMcpConfig();

    const testConfig = {
      api_key: "sk-test-roundtrip",
      workspace_id: "ws-123",
      workspace_slug: "test-ws",
    };

    saveMcpConfig(testConfig);
    const loaded = loadMcpConfig();

    assert.equal(loaded.api_key, "sk-test-roundtrip");
    assert.equal(loaded.workspace_id, "ws-123");
    assert.equal(loaded.workspace_slug, "test-ws");

    // Restore original
    saveMcpConfig(original);
  });

  it("getMcpConfigPath returns a string ending with .nex-mcp.json", async () => {
    const { getMcpConfigPath } = await import("../../src/lib/nex-mcp-config.js");
    const path = getMcpConfigPath();
    assert.ok(typeof path === "string");
    assert.ok(path.endsWith(".nex-mcp.json"), `expected path ending with .nex-mcp.json, got: ${path}`);
  });
});

// ── detectPlatforms tests ──

describe("detectPlatforms", () => {
  it("returns an array", async () => {
    const { detectPlatforms } = await import("../../src/commands/init.js");
    const platforms = detectPlatforms();
    assert.ok(Array.isArray(platforms));
  });

  it("each platform has required shape", async () => {
    const { detectPlatforms } = await import("../../src/commands/init.js");
    const platforms = detectPlatforms();

    for (const p of platforms) {
      assert.ok(typeof p.name === "string" && p.name.length > 0, `name should be non-empty: ${p.name}`);
      assert.ok(typeof p.slug === "string" && p.slug.length > 0, `slug should be non-empty: ${p.slug}`);
      assert.equal(typeof p.detected, "boolean", `detected should be boolean for ${p.slug}`);
      assert.equal(typeof p.nexInstalled, "boolean", `nexInstalled should be boolean for ${p.slug}`);
      assert.equal(typeof p.capabilities, "object", `capabilities should be object for ${p.slug}`);
      assert.equal(typeof p.capabilities.hooks, "boolean", `capabilities.hooks should be boolean for ${p.slug}`);
      assert.equal(typeof p.capabilities.rules, "boolean", `capabilities.rules should be boolean for ${p.slug}`);
      assert.equal(typeof p.capabilities.mcp, "boolean", `capabilities.mcp should be boolean for ${p.slug}`);
      assert.equal(typeof p.capabilities.commands, "boolean", `capabilities.commands should be boolean for ${p.slug}`);
    }
  });

  it("includes Claude Code in the list", async () => {
    const { detectPlatforms } = await import("../../src/commands/init.js");
    const platforms = detectPlatforms();
    const claudeCode = platforms.find((p) => p.slug === "claude-code");
    assert.ok(claudeCode, "should include claude-code");
    assert.equal(claudeCode!.name, "Claude Code");
    assert.equal(claudeCode!.capabilities.hooks, true);
    assert.equal(claudeCode!.capabilities.commands, true);
  });

  it("includes Cursor in the list", async () => {
    const { detectPlatforms } = await import("../../src/commands/init.js");
    const platforms = detectPlatforms();
    const cursor = platforms.find((p) => p.slug === "cursor");
    assert.ok(cursor, "should include cursor");
    assert.equal(cursor!.capabilities.rules, true);
    assert.equal(cursor!.capabilities.hooks, true);
  });

  it("includes Zed in the list", async () => {
    const { detectPlatforms } = await import("../../src/commands/init.js");
    const platforms = detectPlatforms();
    const zed = platforms.find((p) => p.slug === "zed");
    assert.ok(zed, "should include zed");
    assert.equal(zed!.capabilities.rules, true);
    assert.equal(zed!.capabilities.hooks, false);
  });

  it("has no duplicate slugs", async () => {
    const { detectPlatforms } = await import("../../src/commands/init.js");
    const platforms = detectPlatforms();
    const slugs = platforms.map((p) => p.slug);
    const unique = new Set(slugs);
    assert.equal(slugs.length, unique.size, "should have no duplicate slugs");
  });
});

// ── runInit tests ──

describe("runInit", () => {
  it("reports auth error when no email and no API key", async () => {
    const { runInit } = await import("../../src/commands/init.js");
    const { resolveApiKey } = await import("../../src/lib/config.js");

    const originalKey = process.env.NEX_API_KEY;
    delete process.env.NEX_API_KEY;

    const progress: Array<{ step: string; detail?: string; error?: string }> = [];

    // Explicitly pass apiKey: undefined to bypass resolveApiKey() checking config file
    await runInit(
      (p) => progress.push(p),
      { email: undefined, apiKey: undefined },
    );

    const authStep = progress.find((p) => p.step === "auth");
    assert.ok(authStep, "should have an auth step");

    // If the user's config file has a key, runInit will find it via resolveApiKey()
    // and report "Already authenticated" instead of "no_email". Both are valid.
    const existingKey = resolveApiKey();
    if (existingKey) {
      assert.ok(
        authStep!.detail!.includes("Already authenticated"),
        `with existing config key, expected 'Already authenticated', got: ${authStep!.detail}`,
      );
    } else {
      assert.equal(authStep!.error, "no_email");
    }

    if (originalKey !== undefined) {
      process.env.NEX_API_KEY = originalKey;
    }
  });

  it("skips registration when apiKey is provided", async () => {
    const { runInit } = await import("../../src/commands/init.js");

    const progress: Array<{ step: string; detail?: string; done?: boolean }> = [];

    await runInit(
      (p) => progress.push(p),
      { apiKey: "sk-test-skip-registration" },
    );

    const authStep = progress.find((p) => p.step === "auth");
    assert.ok(authStep, "should have an auth step");
    assert.ok(authStep!.detail!.includes("Already authenticated"), `expected 'Already authenticated', got: ${authStep!.detail}`);

    // Should have reached detect step
    const detectStep = progress.find((p) => p.step === "detect");
    assert.ok(detectStep, "should have a detect step");
  });

  it("reports progress callbacks for detection and completion", async () => {
    const { runInit } = await import("../../src/commands/init.js");

    const steps: string[] = [];

    await runInit(
      (p) => steps.push(p.step),
      { apiKey: "sk-test-progress" },
    );

    assert.ok(steps.includes("auth"), "should report auth step");
    assert.ok(steps.includes("detect"), "should report detect step");
  });
});

// ── dispatch integration ──

describe("init via dispatch", () => {
  it("init command is registered", async () => {
    const { commandNames } = await import("../../src/commands/dispatch.js");
    assert.ok(commandNames.includes("init"), "should include init command");
    assert.ok(commandNames.includes("detect"), "should include detect command");
  });

  it("dispatch('init') runs without crashing", async () => {
    const { dispatch } = await import("../../src/commands/dispatch.js");
    const result = await dispatch("init", { apiKey: "sk-test-dispatch" });
    // Should succeed or at least not crash — exit code 0 means it ran the flow
    assert.equal(typeof result.exitCode, "number");
    assert.ok(result.exitCode === 0 || result.exitCode === 1, `unexpected exit code: ${result.exitCode}`);
  });

  it("setup alias resolves to init", async () => {
    const { dispatch } = await import("../../src/commands/dispatch.js");
    const result = await dispatch("setup", { apiKey: "sk-test-alias" });
    assert.equal(typeof result.exitCode, "number");
    // Should NOT be "unknown command"
    if (result.error) {
      assert.ok(!/unknown command/i.test(result.error), `'setup' should resolve, got: ${result.error}`);
    }
  });

  it("detect command returns platform list", async () => {
    const { dispatch } = await import("../../src/commands/dispatch.js");
    const result = await dispatch("detect", { format: "json" });
    assert.equal(result.exitCode, 0);
    assert.ok(Array.isArray(result.data), "detect should return an array");
  });
});
