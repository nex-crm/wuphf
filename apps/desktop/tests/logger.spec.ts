import {
  existsSync,
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  createLogger,
  type LogPayload,
  StructuredLogger,
  UnsafeLogPayloadError,
} from "../src/main/logger.ts";

const fsMock = vi.hoisted(() => ({
  mkdirSync: vi.fn<typeof import("node:fs").mkdirSync>(),
  statSync: vi.fn<typeof import("node:fs").statSync>(),
}));

const electronMock = vi.hoisted(() => ({
  getPath: vi.fn<(name: string) => string>(),
}));

vi.mock("node:fs", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:fs")>();
  fsMock.mkdirSync.mockImplementation(actual.mkdirSync);
  fsMock.statSync.mockImplementation(actual.statSync);
  return {
    ...actual,
    mkdirSync: fsMock.mkdirSync,
    statSync: fsMock.statSync,
  };
});

vi.mock("electron", () => ({
  app: {
    getPath: electronMock.getPath,
  },
}));

const tempDirs: string[] = [];

describe("StructuredLogger", () => {
  beforeEach(() => {
    electronMock.getPath.mockReset();
    fsMock.mkdirSync.mockClear();
    fsMock.statSync.mockClear();
  });

  afterEach(() => {
    for (const dir of tempDirs.splice(0)) {
      rmSync(dir, { force: true, recursive: true });
    }
  });

  it("writes structured JSONL to Electron's local logs directory and mirrors to console", () => {
    const logDirectory = createTempDir();
    electronMock.getPath.mockReturnValue(logDirectory);
    const consoleLines: string[] = [];
    let nowMs = 100;
    const sink = new StructuredLogger({
      consoleWriter: (level, line) => {
        consoleLines.push(`${level}:${line}`);
      },
      monotonicNow: () => nowMs,
    });
    const logger = sink.forModule("main");

    logger.info("app_when_ready", { isPackaged: false, version: "0.0.0-test" });
    nowMs = 125;
    logger.error("unhandled_rejection", { reason: "boom" });

    expect(electronMock.getPath).toHaveBeenCalledWith("logs");
    const records = readLogRecords(logDirectory);
    expect(records).toEqual([
      {
        ts: 100,
        eventLsn: 1,
        level: "info",
        module: "main",
        event: "app_when_ready",
        isPackaged: false,
        version: "0.0.0-test",
      },
      {
        ts: 125,
        eventLsn: 2,
        level: "error",
        module: "main",
        event: "unhandled_rejection",
        reason: "boom",
      },
    ]);
    expect(consoleLines).toHaveLength(2);
    expect(consoleLines[0]).toContain('"event":"app_when_ready"');
    expect(consoleLines[1]).toContain('"level":"error"');
  });

  it("rotates main.log at the configured size and keeps the last two rotations", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      maxFileBytes: 400,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("broker");

    for (let index = 0; index < 8; index += 1) {
      logger.warn("broker_restart_scheduled", {
        reason: `entry-${index}-${"x".repeat(80)}`,
        restartCount: index,
        backoffMs: 250,
        maxRestartRetries: 5,
      });
    }

    expect(existsSync(join(logDirectory, "main.log"))).toBe(true);
    expect(existsSync(join(logDirectory, "main.1.log"))).toBe(true);
    expect(existsSync(join(logDirectory, "main.2.log"))).toBe(true);
    expect(existsSync(join(logDirectory, "main.3.log"))).toBe(false);
    expect(fsMock.statSync).toHaveBeenCalledTimes(1);
    expect(readLogRecords(logDirectory).at(-1)).toMatchObject({
      module: "broker",
      event: "broker_restart_scheduled",
      restartCount: 7,
    });
  });

  it("initializes the log directory once across repeated writes", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("broker");

    for (let index = 0; index < 100; index += 1) {
      logger.info("broker_liveness_ping");
    }

    expect(fsMock.mkdirSync).toHaveBeenCalledOnce();
    expect(fsMock.mkdirSync).toHaveBeenCalledWith(logDirectory, { recursive: true });
    expect(readLogRecords(logDirectory)).toHaveLength(100);
  });

  it("recreates the log directory if it is deleted after initialization", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("broker");

    logger.info("broker_liveness_ping");
    expect(readLogRecords(logDirectory)).toHaveLength(1);

    rmSync(logDirectory, { recursive: true });
    logger.warn("broker_restart_scheduled", {
      reason: "after-cleanup",
      restartCount: 1,
      backoffMs: 250,
      maxRestartRetries: 5,
    });

    expect(existsSync(logDirectory)).toBe(true);
    expect(fsMock.mkdirSync).toHaveBeenCalledTimes(2);
    expect(readLogRecords(logDirectory)).toEqual([
      expect.objectContaining({
        module: "broker",
        event: "broker_restart_scheduled",
        reason: "after-cleanup",
      }),
    ]);
  });

  it("continues without throwing if log directory recovery fails", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("broker");

    logger.info("broker_liveness_ping");
    rmSync(logDirectory, { recursive: true });
    fsMock.mkdirSync.mockImplementationOnce(() => {
      throw new Error("mkdir retry failed");
    });

    expect(() => logger.info("broker_liveness_ping")).not.toThrow();
    expect(existsSync(logDirectory)).toBe(false);
  });

  it("writes a single line larger than the rotation threshold without rotating", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      maxFileBytes: 1,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    logger.error("app_start_failed", { error: "boom" });

    expect(existsSync(join(logDirectory, "main.log"))).toBe(true);
    expect(existsSync(join(logDirectory, "main.1.log"))).toBe(false);
    expect(fsMock.statSync).not.toHaveBeenCalled();
    expect(readLogRecords(logDirectory)).toHaveLength(1);
  });

  it("skips rotation without throwing if the active log is deleted before rename", () => {
    const logDirectory = createTempDir();
    const activePath = join(logDirectory, "main.log");
    writeFileSync(activePath, "x".repeat(360), "utf8");
    const renameCalls: Array<readonly [string, string]> = [];
    const rotationFs = {
      existsSync(path: Parameters<typeof existsSync>[0]) {
        if (String(path) === activePath) {
          unlinkSync(activePath);
          return false;
        }
        return existsSync(path);
      },
      renameSync(
        sourcePath: Parameters<typeof renameSync>[0],
        targetPath: Parameters<typeof renameSync>[1],
      ) {
        renameCalls.push([String(sourcePath), String(targetPath)]);
        renameSync(sourcePath, targetPath);
      },
      unlinkSync(path: Parameters<typeof unlinkSync>[0]) {
        unlinkSync(path);
      },
    };
    const sink = new StructuredLogger({
      logDirectory,
      rotationFs,
      maxFileBytes: 400,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    expect(() => logger.info("app_start_failed", { error: "boom" })).not.toThrow();

    expect(renameCalls).toHaveLength(0);
    expect(existsSync(join(logDirectory, "main.1.log"))).toBe(false);
    expect(readLogRecords(logDirectory)).toEqual([
      expect.objectContaining({
        module: "main",
        event: "app_start_failed",
        error: "boom",
      }),
    ]);
  });

  it("routes default console writes by level and accepts a directory resolver", () => {
    const logDirectory = createTempDir();
    const stdoutWrite = vi.spyOn(process.stdout, "write").mockImplementation(() => true);
    const stderrWrite = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    const sink = new StructuredLogger({
      logDirectory: () => logDirectory,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    logger.info("info_event");
    logger.error("unhandled_rejection", { reason: "boom" });

    expect(stdoutWrite).toHaveBeenCalledOnce();
    expect(stderrWrite).toHaveBeenCalledOnce();
    expect(readLogRecords(logDirectory)).toHaveLength(2);

    stdoutWrite.mockRestore();
    stderrWrite.mockRestore();
  });

  it("creates module loggers from the default Electron logs sink", () => {
    const logDirectory = createTempDir();
    electronMock.getPath.mockReturnValue(logDirectory);
    const stdoutWrite = vi.spyOn(process.stdout, "write").mockImplementation(() => true);

    createLogger("main").info("app_when_ready", { isPackaged: true });

    expect(readLogRecords(logDirectory)[0]).toMatchObject({
      module: "main",
      event: "app_when_ready",
      isPackaged: true,
    });

    stdoutWrite.mockRestore();
  });

  it("continues with console-only logging if the Electron logs path is unavailable", () => {
    electronMock.getPath.mockImplementation(() => {
      throw new Error("app path unavailable");
    });
    const consoleLines: string[] = [];
    const sink = new StructuredLogger({
      consoleWriter: (_level, line) => {
        consoleLines.push(line);
      },
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    expect(() => logger.info("app_start_failed", { error: "boom" })).not.toThrow();
    expect(consoleLines).toHaveLength(1);
  });

  it("does not throw if the local file sink cannot be opened", () => {
    const logDirectory = createTempDir();
    const blockedLogDirectory = join(logDirectory, "not-a-directory");
    writeFileSync(blockedLogDirectory, "not a directory", "utf8");
    const sink = new StructuredLogger({
      logDirectory: blockedLogDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    expect(() => logger.info("app_start_failed", { error: "boom" })).not.toThrow();
  });

  it("rejects unsafe event names and unknown payload keys", () => {
    const sink = new StructuredLogger({
      logDirectory: createTempDir(),
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    expect(() => logger.info("bad event")).toThrow("Invalid log event");
    expect(() => sink.forModule("bad module")).toThrow("Invalid log module");
    expect(() => logger.info("app_start_failed", { unsafe: "value" } as LogPayload)).toThrow(
      UnsafeLogPayloadError,
    );
  });

  it("truncates oversized safe string values before writing", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    logger.error("unhandled_rejection", { reason: "x".repeat(9_000) });

    const record = readLogRecords(logDirectory)[0] as { readonly reason?: unknown } | undefined;
    expect(typeof record?.reason).toBe("string");
    expect((record?.reason as string).endsWith("...")).toBe(true);
    expect((record?.reason as string).length).toBeLessThan(9_000);
  });

  it("marks byte-oversized string values that fit within the char cap", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");
    const reason = "€".repeat(3_000);

    logger.error("unhandled_rejection", { reason });

    const record = readLogRecords(logDirectory)[0] as { readonly reason?: unknown } | undefined;
    expect(record?.reason).toBe(`${reason}...`);
  });

  it("pre-caps multi-megabyte string values before writing", () => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("main");

    logger.error("unhandled_rejection", { reason: "x".repeat(2 * 1024 * 1024) });

    const record = readLogRecords(logDirectory)[0] as { readonly reason?: unknown } | undefined;
    expect(typeof record?.reason).toBe("string");
    expect((record?.reason as string).endsWith("...")).toBe(true);
    expect(record?.reason as string).toHaveLength(8_195);
  });

  it.each(["url", "path", "token", "secret"])("rejects banned payload key %s", (key) => {
    const logDirectory = createTempDir();
    const sink = new StructuredLogger({
      logDirectory,
      consoleWriter: () => undefined,
      monotonicNow: () => 1,
    });
    const logger = sink.forModule("ipc");
    const payload = { [key]: "unsafe" } as LogPayload;

    expect(() => logger.warn("ipc_payload_rejected", payload)).toThrow(UnsafeLogPayloadError);
    expect(existsSync(join(logDirectory, "main.log"))).toBe(false);
  });
});

function createTempDir(): string {
  const dir = mkdtempSync(join(tmpdir(), "wuphf-desktop-logger-"));
  tempDirs.push(dir);
  return dir;
}

function readLogRecords(logDirectory: string): readonly Record<string, unknown>[] {
  return readFileSync(join(logDirectory, "main.log"), "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as Record<string, unknown>);
}
