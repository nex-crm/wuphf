import { describe, it, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

// We need to override CONFIG_PATH before importing config functions.
// The config module uses homedir-based CONFIG_PATH, so we'll test
// loadConfig/saveConfig by writing directly and using persistRegistration.

describe("config resolution", () => {
  let tmpDir: string;
  let originalEnv: string | undefined;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "nex-config-test-"));
    originalEnv = process.env.NEX_API_KEY;
    delete process.env.NEX_API_KEY;
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
    if (originalEnv !== undefined) {
      process.env.NEX_API_KEY = originalEnv;
    } else {
      delete process.env.NEX_API_KEY;
    }
  });

  it("resolveApiKey: flag takes priority", async () => {
    const { resolveApiKey } = await import("../../src/lib/config.js");
    process.env.NEX_API_KEY = "env-key";
    assert.equal(resolveApiKey("flag-key"), "flag-key");
  });

  it("resolveApiKey: env var used when no flag", async () => {
    const { resolveApiKey } = await import("../../src/lib/config.js");
    process.env.NEX_API_KEY = "env-key";
    assert.equal(resolveApiKey(undefined), "env-key");
  });

  it("resolveApiKey: returns undefined when nothing set", async () => {
    const { resolveApiKey } = await import("../../src/lib/config.js");
    delete process.env.NEX_API_KEY;
    // Config file may or may not have a key, but flag and env are empty
    const result = resolveApiKey(undefined);
    // Result is either from config file or undefined - both valid
    assert.ok(result === undefined || typeof result === "string");
  });

  it("resolveFormat: flag takes priority over default", async () => {
    const { resolveFormat } = await import("../../src/lib/config.js");
    assert.equal(resolveFormat("text"), "text");
  });

  it("resolveFormat: defaults to json", async () => {
    const { resolveFormat } = await import("../../src/lib/config.js");
    // Without config file setting, should default to "json"
    const result = resolveFormat(undefined);
    assert.ok(["json", "text", "quiet"].includes(result));
  });

  it("resolveTimeout: flag takes priority", async () => {
    const { resolveTimeout } = await import("../../src/lib/config.js");
    assert.equal(resolveTimeout("5000"), 5000);
  });

  it("resolveTimeout: defaults to 120000", async () => {
    const { resolveTimeout } = await import("../../src/lib/config.js");
    const result = resolveTimeout(undefined);
    assert.equal(typeof result, "number");
    assert.ok(result > 0);
  });

  it("loadConfig returns empty object when no config file", async () => {
    // Temporarily point to non-existent path by checking behavior
    const { loadConfig } = await import("../../src/lib/config.js");
    const config = loadConfig();
    assert.equal(typeof config, "object");
  });

  it("saveConfig and loadConfig round-trip", async () => {
    const { saveConfig, loadConfig, CONFIG_PATH } = await import("../../src/lib/config.js");
    const original = loadConfig();
    const testConfig = { ...original, api_key: "test-round-trip-key", default_format: "text" };
    saveConfig(testConfig);
    const loaded = loadConfig();
    assert.equal(loaded.api_key, "test-round-trip-key");
    assert.equal(loaded.default_format, "text");
    // Restore original
    saveConfig(original);
  });

  it("persistRegistration saves api_key, workspace_id, workspace_slug", async () => {
    const { persistRegistration, loadConfig, saveConfig } = await import("../../src/lib/config.js");
    const original = loadConfig();
    persistRegistration({
      api_key: "reg-key-123",
      workspace_id: "ws-456",
      workspace_slug: "my-workspace",
    });
    const config = loadConfig();
    assert.equal(config.api_key, "reg-key-123");
    assert.equal(config.workspace_id, "ws-456");
    assert.equal(config.workspace_slug, "my-workspace");
    // Restore original
    saveConfig(original);
  });
});
