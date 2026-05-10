import type { IpcMainInvokeEvent } from "electron";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { assertMaxStringLength } from "../src/main/ipc/_guards.ts";
import { handleGetAppVersion } from "../src/main/ipc/get-app-version.ts";
import { handleGetBrokerStatus } from "../src/main/ipc/get-broker-status.ts";
import { handleGetPlatform, narrowPlatform } from "../src/main/ipc/get-platform.ts";
import { createOpenExternalHandler, handleOpenExternal } from "../src/main/ipc/open-external.ts";
import { createIpcHandlers } from "../src/main/ipc/register-handlers.ts";
import { handleShowItemInFolder } from "../src/main/ipc/show-item-in-folder.ts";
import type { Logger, LogPayload } from "../src/main/logger.ts";
import { IpcChannel } from "../src/shared/api-contract.ts";

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
    "http://[",
    "file:///tmp/wuphf.txt",
    "javascript:alert(1)",
    "data:text/plain,wuphf",
    "vbscript:x",
  ])("rejects unsafe URL scheme %s", async (url) => {
    await expect(handleOpenExternal(event, { url })).resolves.toMatchObject({ ok: false });
    expect(electronMock.openExternal).not.toHaveBeenCalled();
  });

  it.each([
    ["https://example.com", "https://example.com/"],
    ["http://example.com", "http://example.com/"],
    ["mailto:fd@example.com?subject=hi", "mailto:fd@example.com?subject=hi"],
  ])("opens allowlisted scheme %s through the OS shell", async (input, expectedHandoff) => {
    await expect(handleOpenExternal(event, { url: input })).resolves.toEqual({
      ok: true,
    });
    expect(electronMock.openExternal).toHaveBeenCalledWith(expectedHandoff);
  });

  it("returns an error response when the OS shell rejects the URL handoff", async () => {
    electronMock.openExternal.mockRejectedValueOnce(new Error("OS refused URL"));
    const openExternal = createOpenExternalHandler({ monotonicNow: () => 0 });

    await expect(openExternal(event, { url: "https://example.com" })).resolves.toEqual({
      ok: false,
      error: "OS refused URL",
    });
  });

  it("returns a stable error response when shell.openExternal rejects without an Error", async () => {
    electronMock.openExternal.mockRejectedValueOnce("rejected");
    const openExternal = createOpenExternalHandler({ monotonicNow: () => 0 });

    await expect(openExternal(event, { url: "https://example.com" })).resolves.toEqual({
      ok: false,
      error: "shell.openExternal rejected",
    });
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

  it("logs rejected URL payloads with channel and reason only", async () => {
    const { logger, calls } = createMemoryLogger();
    const openExternal = createOpenExternalHandler({ logger, monotonicNow: () => 0 });

    await expect(openExternal(event, { url: "file:///Users/fran/private.txt" })).resolves.toEqual({
      ok: false,
      error: "Unsupported external URL protocol: file:",
    });

    expect(calls).toEqual([
      {
        level: "warn",
        event: "ipc_payload_rejected",
        payload: {
          channel: IpcChannel.OpenExternal,
          reason: "unsupported_scheme",
          scheme: "file:",
        },
      },
    ]);
  });

  it("rate-limits the sixth rapid OS browser handoff", async () => {
    let nowMs = 0;
    const openExternal = createOpenExternalHandler({ monotonicNow: () => nowMs });

    for (let index = 0; index < 5; index += 1) {
      await expect(openExternal(event, { url: `https://example.com/${index}` })).resolves.toEqual({
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

  it("rejects Windows network and device paths regardless of host platform", () => {
    // \\server\share — backslash UNC. NTLM credential leak via SMB if Explorer
    // is asked to reveal a renderer-controlled remote path.
    expect(handleShowItemInFolder(event, { path: "\\\\attacker\\share\\file.txt" })).toEqual({
      ok: false,
      error: "Path must not be a Windows network or device path",
    });

    // //server/share — forward-slash UNC.
    expect(handleShowItemInFolder(event, { path: "//attacker/share/file.txt" })).toEqual({
      ok: false,
      error: "Path must not be a Windows network or device path",
    });

    // \\?\UNC\server\share\file — DOS-device UNC long-path form.
    expect(handleShowItemInFolder(event, { path: "\\\\?\\UNC\\attacker\\share\\file" })).toEqual({
      ok: false,
      error: "Path must not be a Windows network or device path",
    });

    // \\.\PhysicalDrive0 — DOS-device raw-device path.
    expect(handleShowItemInFolder(event, { path: "\\\\.\\PhysicalDrive0" })).toEqual({
      ok: false,
      error: "Path must not be a Windows network or device path",
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

  it("returns a stable error when the OS shell throws a non-Error value", () => {
    electronMock.showItemInFolder.mockImplementationOnce(() => {
      throw "rejected";
    });

    expect(handleShowItemInFolder(event, { path: "/legit/file" })).toEqual({
      ok: false,
      error: "Failed to reveal path in OS file manager",
    });
  });
});

describe("IPC guard helpers", () => {
  it("rejects non-string values before byte-length checks", () => {
    expect(assertMaxStringLength(42, 5, "field")).toEqual({
      valid: false,
      error: "field must be a string",
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

describe("IPC handler registration logging", () => {
  it("threads the IPC logger into empty-payload handlers", () => {
    const { logger, calls } = createMemoryLogger();
    const handlers = createIpcHandlers(
      {
        getSnapshot: () => ({
          status: "alive",
          pid: 1234,
          restartCount: 2,
        }),
      },
      { logger },
    );

    expect(handlers[IpcChannel.GetAppVersion](event, { extra: true })).toEqual({
      ok: false,
      error: "getAppVersion expects an empty request object",
    });

    expect(calls).toEqual([
      {
        level: "warn",
        event: "ipc_payload_rejected",
        payload: {
          channel: IpcChannel.GetAppVersion,
          reason: "invalid_request",
        },
      },
    ]);
  });
});

interface LogCall {
  readonly level: "info" | "warn" | "error";
  readonly event: string;
  readonly payload: LogPayload | undefined;
}

function createMemoryLogger(): { readonly logger: Logger; readonly calls: LogCall[] } {
  const calls: LogCall[] = [];
  const push = (level: LogCall["level"], event: string, payload?: LogPayload): void => {
    calls.push({ level, event, payload });
  };

  return {
    calls,
    logger: {
      info: (event, payload) => push("info", event, payload),
      warn: (event, payload) => push("warn", event, payload),
      error: (event, payload) => push("error", event, payload),
    },
  };
}
