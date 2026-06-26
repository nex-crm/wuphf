"use strict";

const { describe, test, expect, afterEach, beforeEach } = require("bun:test");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const {
  cacheDir,
  ensureCacheDir,
  setBackupExclusionRunnerForTest,
} = require("./version-check");

const origPlatform = process.platform;
const origRuntimeHome = process.env.WUPHF_RUNTIME_HOME;

function setPlatform(platform) {
  Object.defineProperty(process, "platform", { value: platform, configurable: true });
}

beforeEach(() => {
  setPlatform("linux");
  setBackupExclusionRunnerForTest(async () => {});
});

afterEach(() => {
  setPlatform(origPlatform);
  if (origRuntimeHome === undefined) {
    delete process.env.WUPHF_RUNTIME_HOME;
  } else {
    process.env.WUPHF_RUNTIME_HOME = origRuntimeHome;
  }
  setBackupExclusionRunnerForTest(async () => {});
});

describe("ensureCacheDir", () => {
  test("creates the wuphf cache directory under the runtime home", async () => {
    const home = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-cache-home-"));
    process.env.WUPHF_RUNTIME_HOME = home;

    await ensureCacheDir();

    expect(fs.statSync(path.join(home, ".wuphf", "cache")).isDirectory()).toBe(true);
    expect(cacheDir()).toBe(path.join(home, ".wuphf", "cache"));
  });

  test("marks the cache directory as excluded from backups on macOS", async () => {
    const home = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-cache-home-"));
    const calls = [];
    process.env.WUPHF_RUNTIME_HOME = home;
    setPlatform("darwin");
    setBackupExclusionRunnerForTest(async (target) => {
      calls.push(target);
    });

    await ensureCacheDir();

    expect(calls).toContain(path.join(home, ".wuphf", "cache"));
  });

  test("does not call the backup exclusion runner outside macOS", async () => {
    const home = fs.mkdtempSync(path.join(os.tmpdir(), "wuphf-cache-home-"));
    const calls = [];
    process.env.WUPHF_RUNTIME_HOME = home;
    setPlatform("linux");
    setBackupExclusionRunnerForTest(async (target) => {
      calls.push(target);
    });

    await ensureCacheDir();

    expect(calls).toEqual([]);
  });
});
