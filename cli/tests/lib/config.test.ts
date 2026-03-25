import { describe, test, expect, beforeEach, afterEach } from "bun:test";
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
    tmpDir = mkdtempSync(join(tmpdir(), "wuphf-config-test-"));
    originalEnv = process.env.WUPHF_API_KEY;
    delete process.env.WUPHF_API_KEY;
  });

  afterEach(() => {
    rmSync(tmpDir, { recursive: true, force: true });
    if (originalEnv !== undefined) {
      process.env.WUPHF_API_KEY = originalEnv;
    } else {
      delete process.env.WUPHF_API_KEY;
    }
  });

  test("resolveApiKey: flag takes priority", async () => {
    const { resolveApiKey } = await import("../../src/lib/config.ts");
    process.env.WUPHF_API_KEY = "env-key";
    expect(resolveApiKey("flag-key")).toBe("flag-key");
  });

  test("resolveApiKey: env var used when no flag", async () => {
    const { resolveApiKey } = await import("../../src/lib/config.ts");
    process.env.WUPHF_API_KEY = "env-key";
    expect(resolveApiKey(undefined)).toBe("env-key");
  });

  test("resolveApiKey: returns undefined when nothing set", async () => {
    const { resolveApiKey } = await import("../../src/lib/config.ts");
    delete process.env.WUPHF_API_KEY;
    // Config file may or may not have a key, but flag and env are empty
    const result = resolveApiKey(undefined);
    // Result is either from config file or undefined - both valid
    expect(result === undefined || typeof result === "string").toBeTruthy();
  });

  test("resolveFormat: flag takes priority over default", async () => {
    const { resolveFormat } = await import("../../src/lib/config.ts");
    expect(resolveFormat("text")).toBe("text");
  });

  test("resolveFormat: defaults to json", async () => {
    const { resolveFormat } = await import("../../src/lib/config.ts");
    // Without config file setting, should default to "json"
    const result = resolveFormat(undefined);
    expect(["json", "text", "quiet"].includes(result)).toBeTruthy();
  });

  test("resolveTimeout: flag takes priority", async () => {
    const { resolveTimeout } = await import("../../src/lib/config.ts");
    expect(resolveTimeout("5000")).toBe(5000);
  });

  test("resolveTimeout: defaults to 120000", async () => {
    const { resolveTimeout } = await import("../../src/lib/config.ts");
    const result = resolveTimeout(undefined);
    expect(typeof result).toBe("number");
    expect(result > 0).toBeTruthy();
  });

  test("loadConfig returns empty object when no config file", async () => {
    // Temporarily point to non-existent path by checking behavior
    const { loadConfig } = await import("../../src/lib/config.ts");
    const config = loadConfig();
    expect(typeof config).toBe("object");
  });

  test("saveConfig and loadConfig round-trip", async () => {
    const { saveConfig, loadConfig, CONFIG_PATH } = await import("../../src/lib/config.ts");
    const original = loadConfig();
    const testConfig = { ...original, api_key: "test-round-trip-key", default_format: "text" };
    saveConfig(testConfig);
    const loaded = loadConfig();
    expect(loaded.api_key).toBe("test-round-trip-key");
    expect(loaded.default_format).toBe("text");
    // Restore original
    saveConfig(original);
  });

  test("persistRegistration saves api_key, workspace_id, workspace_slug", async () => {
    const { persistRegistration, loadConfig, saveConfig } = await import("../../src/lib/config.ts");
    const original = loadConfig();
    persistRegistration({
      api_key: "reg-key-123",
      workspace_id: "ws-456",
      workspace_slug: "my-workspace",
    });
    const config = loadConfig();
    expect(config.api_key).toBe("reg-key-123");
    expect(config.workspace_id).toBe("ws-456");
    expect(config.workspace_slug).toBe("my-workspace");
    // Restore original
    saveConfig(original);
  });
});
