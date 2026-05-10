import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  createLogger,
  type LogPayload,
  StructuredLogger,
  UnsafeLogPayloadError,
} from "../src/main/logger.ts";

const electronMock = vi.hoisted(() => ({
  getPath: vi.fn<(name: string) => string>(),
}));

vi.mock("electron", () => ({
  app: {
    getPath: electronMock.getPath,
  },
}));

const tempDirs: string[] = [];

describe("StructuredLogger", () => {
  beforeEach(() => {
    electronMock.getPath.mockReset();
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
    expect(readLogRecords(logDirectory).at(-1)).toMatchObject({
      module: "broker",
      event: "broker_restart_scheduled",
      restartCount: 7,
    });
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
