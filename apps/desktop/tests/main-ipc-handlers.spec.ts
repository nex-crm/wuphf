import type { IpcMainInvokeEvent } from "electron";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { handleGetAppVersion } from "../src/main/ipc/get-app-version.ts";
import { handleGetBrokerStatus } from "../src/main/ipc/get-broker-status.ts";
import { handleGetPlatform, narrowPlatform } from "../src/main/ipc/get-platform.ts";
import {
  createOpenExternalHandler,
  handleOpenExternal,
} from "../src/main/ipc/open-external.ts";
import { handleShowItemInFolder } from "../src/main/ipc/show-item-in-folder.ts";

const electronMock = vi.hoisted(() => ({
  openExternal: vi.fn<(url: string) => Promise<void>>(() => Promise.resolve()),
  showItemInFolder: vi.fn<(path: string) => void>(),
  getVersion: vi.fn<() => string>(() => "0.0.0-test"),
}));

vi.mock("electron", () => ({
  shell: {
    openExternal: electronMock.openExternal,
    showItemInFolder: electronMock.showItemInFolder,
  },
  app: {
    getVersion: electronMock.getVersion,
  },
}));

const event = {} as IpcMainInvokeEvent;

describe("openExternal handler", () => {
  beforeEach(() => {
    electronMock.openExternal.mockClear();
  });

  it("rejects non-string, missing, and extra payload fields", async () => {
    await expect(handleOpenExternal(event, undefined)).resolves.toMatchObject({ ok: false });
    await expect(handleOpenExternal(event, null)).resolves.toMatchObject({ ok: false });
    await expect(handleOpenExternal(event, {})).resolves.toMatchObject({ ok: false });
    await expect(handleOpenExternal(event, { url: 42 })).resolves.toMatchObject({ ok: false });
    await expect(
      handleOpenExternal(event, { url: "https://example.com", extra: true }),
    ).resolves.toMatchObject({ ok: false });
    expect(electronMock.openExternal).not.toHaveBeenCalled();
  });

  it.each([
    "file:///tmp/wuphf.txt",
    "javascript:alert(1)",
    "data:text/plain,wuphf",
    "vbscript:x",
  ])("rejects unsafe URL scheme %s", async (url) => {
    await expect(handleOpenExternal(event, { url })).resolves.toMatchObject({ ok: false });
    expect(electronMock.openExternal).not.toHaveBeenCalled();
  });

  it("opens https URLs through the OS shell", async () => {
    await expect(handleOpenExternal(event, { url: "https://example.com" })).resolves.toEqual({
      ok: true,
    });
    expect(electronMock.openExternal).toHaveBeenCalledWith("https://example.com/");
  });

  it("rejects oversized URL payloads before parsing or opening them", async () => {
    await expect(
      handleOpenExternal(event, { url: `https://example.com/${"a".repeat(8_192)}` }),
    ).resolves.toEqual({
      ok: false,
      error: "url must be at most 8192 bytes",
    });
    expect(electronMock.openExternal).not.toHaveBeenCalled();
  });

  it("rate-limits the sixth rapid OS browser handoff", async () => {
    let nowMs = 0;
    const openExternal = createOpenExternalHandler({ monotonicNow: () => nowMs });

    for (let index = 0; index < 5; index += 1) {
      await expect(
        openExternal(event, { url: `https://example.com/${index}` }),
      ).resolves.toEqual({
        ok: true,
      });
    }

    await expect(openExternal(event, { url: "https://example.com/rate-limited" })).resolves.toEqual(
      {
        ok: false,
        error: "rate_limited",
      },
    );
    expect(electronMock.openExternal).toHaveBeenCalledTimes(5);

    nowMs = 10_000;
    await expect(openExternal(event, { url: "https://example.com/after-window" })).resolves.toEqual(
      {
        ok: true,
      },
    );
    expect(electronMock.openExternal).toHaveBeenCalledTimes(6);
  });
});

describe("showItemInFolder handler", () => {
  beforeEach(() => {
    electronMock.showItemInFolder.mockClear();
  });

  it("rejects non-string, missing, and extra payload fields", () => {
    expect(handleShowItemInFolder(event, undefined)).toMatchObject({ ok: false });
    expect(handleShowItemInFolder(event, null)).toMatchObject({ ok: false });
    expect(handleShowItemInFolder(event, {})).toMatchObject({ ok: false });
    expect(handleShowItemInFolder(event, { path: 42 })).toMatchObject({ ok: false });
    expect(handleShowItemInFolder(event, { path: "/tmp/wuphf.txt", extra: true })).toMatchObject({
      ok: false,
    });
    expect(electronMock.showItemInFolder).not.toHaveBeenCalled();
  });

  it("rejects non-absolute paths", () => {
    expect(handleShowItemInFolder(event, { path: "relative/file.txt" })).toMatchObject({
      ok: false,
    });
    expect(electronMock.showItemInFolder).not.toHaveBeenCalled();
  });

  it("rejects parent traversal and NUL bytes", () => {
    expect(handleShowItemInFolder(event, { path: "/etc/../etc/passwd" })).toMatchObject({
      ok: false,
    });
    expect(handleShowItemInFolder(event, { path: "/legit\0/file" })).toMatchObject({
      ok: false,
    });
    expect(electronMock.showItemInFolder).not.toHaveBeenCalled();
  });

  it("rejects oversized path payloads before normalization", () => {
    expect(handleShowItemInFolder(event, { path: `/${"a".repeat(32_768)}` })).toEqual({
      ok: false,
      error: "path must be at most 32768 bytes",
    });
    expect(electronMock.showItemInFolder).not.toHaveBeenCalled();
  });

  it("reveals absolute paths without reading file contents", () => {
    expect(handleShowItemInFolder(event, { path: "/legit/file" })).toEqual({ ok: true });
    expect(electronMock.showItemInFolder).toHaveBeenCalledWith("/legit/file");
  });

  it("returns an error when the OS shell refuses to reveal the path", () => {
    electronMock.showItemInFolder.mockImplementationOnce(() => {
      throw new Error("OS refused reveal");
    });

    expect(handleShowItemInFolder(event, { path: "/legit/file" })).toEqual({
      ok: false,
      error: "OS refused reveal",
    });
  });
});

describe("getAppVersion handler", () => {
  it("rejects malformed or non-empty payloads without throwing", () => {
    expect(handleGetAppVersion(event, undefined)).toEqual({
      ok: false,
      error: "getAppVersion expects an empty request object",
    });
    expect(handleGetAppVersion(event, null)).toEqual({
      ok: false,
      error: "getAppVersion expects an empty request object",
    });
    expect(handleGetAppVersion(event, { extra: true })).toEqual({
      ok: false,
      error: "getAppVersion expects an empty request object",
    });
  });

  it("returns app.getVersion", () => {
    expect(handleGetAppVersion(event, {})).toEqual({ version: "0.0.0-test" });
  });
});

describe("getPlatform handler", () => {
  it.each([
    "aix",
    "android",
    "darwin",
    "freebsd",
    "haiku",
    "linux",
    "openbsd",
    "sunos",
    "win32",
    "cygwin",
    "netbsd",
  ] satisfies readonly NodeJS.Platform[])("narrows supported Node platform %s", (platform) => {
    expect(narrowPlatform(platform)).toBe(platform);
  });

  it("throws for platforms outside Node's current typed union", () => {
    expect(() => narrowPlatform("plan9" as NodeJS.Platform)).toThrow("Unknown platform: plan9");
  });

  it("rejects malformed or non-empty payloads without throwing", () => {
    expect(handleGetPlatform(event, undefined)).toEqual({
      ok: false,
      error: "getPlatform expects an empty request object",
    });
    expect(handleGetPlatform(event, null)).toEqual({
      ok: false,
      error: "getPlatform expects an empty request object",
    });
    expect(handleGetPlatform(event, { extra: true })).toEqual({
      ok: false,
      error: "getPlatform expects an empty request object",
    });
  });

  it("returns process platform and architecture", () => {
    expect(handleGetPlatform(event, {})).toEqual({
      platform: process.platform,
      arch: process.arch,
    });
  });
});

describe("getBrokerStatus handler", () => {
  const brokerSupervisor = {
    getSnapshot: () => ({
      status: "alive" as const,
      pid: 1234,
      restartCount: 2,
    }),
  };

  it("rejects malformed or non-empty payloads without throwing", () => {
    expect(handleGetBrokerStatus(brokerSupervisor, event, undefined)).toEqual({
      ok: false,
      error: "getBrokerStatus expects an empty request object",
    });
    expect(handleGetBrokerStatus(brokerSupervisor, event, null)).toEqual({
      ok: false,
      error: "getBrokerStatus expects an empty request object",
    });
    expect(handleGetBrokerStatus(brokerSupervisor, event, { extra: true })).toEqual({
      ok: false,
      error: "getBrokerStatus expects an empty request object",
    });
  });

  it("returns broker supervisor lifecycle state only", () => {
    expect(handleGetBrokerStatus(brokerSupervisor, event, {})).toEqual({
      status: "alive",
      pid: 1234,
      restartCount: 2,
    });
  });
});
