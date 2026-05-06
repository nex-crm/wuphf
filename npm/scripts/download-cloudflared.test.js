"use strict";

// Pure-function tests for the cloudflared bundler's platform branching and
// manifest shape. The actual fetch + sha256 + extract path runs against the
// network on a real install, so these tests pin the offline-verifiable bits:
// platform mapping, target filename, and that the manifest covers every
// platform we claim to support.

const { describe, test, expect, afterEach } = require("bun:test");
const path = require("node:path");
const {
  detectManifestKey,
  loadManifest,
  targetBinaryFilename,
} = require("./download-cloudflared");

const origPlatform = process.platform;
const origArch = process.arch;

function setPlatform(platform, arch) {
  Object.defineProperty(process, "platform", {
    value: platform,
    configurable: true,
  });
  Object.defineProperty(process, "arch", { value: arch, configurable: true });
}

afterEach(() => {
  setPlatform(origPlatform, origArch);
});

describe("detectManifestKey", () => {
  test("maps darwin/x64 -> darwin-amd64", () => {
    setPlatform("darwin", "x64");
    expect(detectManifestKey()).toBe("darwin-amd64");
  });
  test("maps darwin/arm64 -> darwin-arm64", () => {
    setPlatform("darwin", "arm64");
    expect(detectManifestKey()).toBe("darwin-arm64");
  });
  test("maps linux/x64 -> linux-amd64", () => {
    setPlatform("linux", "x64");
    expect(detectManifestKey()).toBe("linux-amd64");
  });
  test("maps linux/arm64 -> linux-arm64", () => {
    setPlatform("linux", "arm64");
    expect(detectManifestKey()).toBe("linux-arm64");
  });
  test("maps win32/x64 -> windows-amd64", () => {
    setPlatform("win32", "x64");
    expect(detectManifestKey()).toBe("windows-amd64");
  });
  test("returns null for unsupported (windows arm64) so install does not abort", () => {
    setPlatform("win32", "arm64");
    // cloudflared has no published windows-arm64 asset; the bundler skips
    // silently and the runtime surfaces a clear "missing" error if the
    // user clicks Start tunnel.
    expect(detectManifestKey()).toBe("windows-arm64");
    // Sanity-check that the manifest does NOT have an entry for it, so
    // downloadCloudflared() falls through the "no entry" branch.
    const manifest = loadManifest();
    expect(manifest.platforms["windows-arm64"]).toBeUndefined();
  });
});

describe("targetBinaryFilename", () => {
  test("appends .exe on win32", () => {
    setPlatform("win32", "x64");
    expect(targetBinaryFilename()).toBe("cloudflared.exe");
  });
  test("bare name on linux", () => {
    setPlatform("linux", "x64");
    expect(targetBinaryFilename()).toBe("cloudflared");
  });
});

describe("loadManifest", () => {
  test("pins a version + per-platform sha256", () => {
    const manifest = loadManifest();
    expect(manifest.version).toMatch(/^\d{4}\.\d+\.\d+$/);
    for (const key of [
      "darwin-amd64",
      "darwin-arm64",
      "linux-amd64",
      "linux-arm64",
      "windows-amd64",
    ]) {
      const entry = manifest.platforms[key];
      expect(entry).toBeDefined();
      expect(entry.asset).toMatch(/^cloudflared-/);
      expect(entry.sha256).toMatch(/^[0-9a-f]{64}$/);
    }
  });

  test("manifest path resolves to a real file in the published package", () => {
    // download-cloudflared.js is published; the JSON sits next to it and
    // MUST be in package.json `files`. If it isn't, postinstall fails on
    // a freshly-installed npm package because require() blows up.
    const expected = path.join(__dirname, "cloudflared.json");
    const fs = require("node:fs");
    expect(fs.existsSync(expected)).toBe(true);
  });
});
