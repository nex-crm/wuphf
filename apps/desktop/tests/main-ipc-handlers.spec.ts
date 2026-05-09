import type { IpcMainInvokeEvent } from "electron";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { handleGetAppVersion } from "../src/main/ipc/get-app-version.ts";
import { handleGetBrokerStatus } from "../src/main/ipc/get-broker-status.ts";
import { handleGetPlatform } from "../src/main/ipc/get-platform.ts";
import { handleOpenExternal } from "../src/main/ipc/open-external.ts";
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

  it("reveals absolute paths without reading file contents", () => {
    expect(handleShowItemInFolder(event, { path: "/tmp/wuphf.txt" })).toEqual({ ok: true });
    expect(electronMock.showItemInFolder).toHaveBeenCalledWith("/tmp/wuphf.txt");
  });
});

describe("getAppVersion handler", () => {
  it("rejects malformed or non-empty payloads", () => {
    expect(() => handleGetAppVersion(event, undefined)).toThrow(
      "getAppVersion expects an empty request object",
    );
    expect(() => handleGetAppVersion(event, null)).toThrow(
      "getAppVersion expects an empty request object",
    );
    expect(() => handleGetAppVersion(event, { extra: true })).toThrow(
      "getAppVersion expects an empty request object",
    );
  });

  it("returns app.getVersion", () => {
    expect(handleGetAppVersion(event, {})).toEqual({ version: "0.0.0-test" });
  });
});

describe("getPlatform handler", () => {
  it("rejects malformed or non-empty payloads", () => {
    expect(() => handleGetPlatform(event, undefined)).toThrow(
      "getPlatform expects an empty request object",
    );
    expect(() => handleGetPlatform(event, null)).toThrow(
      "getPlatform expects an empty request object",
    );
    expect(() => handleGetPlatform(event, { extra: true })).toThrow(
      "getPlatform expects an empty request object",
    );
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
    getStatus: () => "alive" as const,
    getPid: () => 1234,
    getRestartCount: () => 2,
  };

  it("rejects malformed or non-empty payloads", () => {
    expect(() => handleGetBrokerStatus(brokerSupervisor, event, undefined)).toThrow(
      "getBrokerStatus expects an empty request object",
    );
    expect(() => handleGetBrokerStatus(brokerSupervisor, event, null)).toThrow(
      "getBrokerStatus expects an empty request object",
    );
    expect(() => handleGetBrokerStatus(brokerSupervisor, event, { extra: true })).toThrow(
      "getBrokerStatus expects an empty request object",
    );
  });

  it("returns broker supervisor lifecycle state only", () => {
    expect(handleGetBrokerStatus(brokerSupervisor, event, {})).toEqual({
      status: "alive",
      pid: 1234,
      restartCount: 2,
    });
  });
});
