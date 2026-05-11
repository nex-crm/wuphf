import { appendFileSync, existsSync, mkdirSync, renameSync, statSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import { app } from "electron";

import { monotonicNowMs } from "./monotonic-clock.ts";

export type LogLevel = "info" | "warn" | "error";
export type LogPayloadValue = string | number | boolean | null;
export type LogPayload = Readonly<Record<string, LogPayloadValue>>;

export interface Logger {
  readonly info: (event: string, payload?: LogPayload) => void;
  readonly warn: (event: string, payload?: LogPayload) => void;
  readonly error: (event: string, payload?: LogPayload) => void;
}

export interface StructuredLoggerConfig {
  // `() => null` disables filesystem logging — useful for tests that exercise
  // the structured-logger validator without writing to disk. The default
  // resolver also returns null when `app.getPath("logs")` throws.
  readonly logDirectory?: string | (() => string | null);
  readonly maxFileBytes?: number;
  readonly consoleWriter?: (level: LogLevel, line: string) => void;
  readonly monotonicNow?: () => number;
  readonly rotationFs?: LoggerRotationFileSystem;
}

export interface LoggerRotationFileSystem {
  readonly existsSync: typeof existsSync;
  readonly renameSync: typeof renameSync;
  readonly unlinkSync: typeof unlinkSync;
}

export class UnsafeLogPayloadError extends Error {
  constructor(key: string) {
    super(`Unsafe log payload key: ${key}`);
    this.name = "UnsafeLogPayloadError";
  }
}

const LOG_FILE_NAME = "main.log";
const DEFAULT_MAX_FILE_BYTES = 10 * 1024 * 1024;
const ROTATED_FILE_COUNT = 2;
const MAX_LOG_STRING_BYTES = 8_192;
export const LOG_NAME_PATTERN = /^[a-z0-9_.:-]+$/;
const BANNED_PAYLOAD_KEY_FRAGMENTS = ["url", "path", "token", "secret", "password"] as const;
const SAFE_PAYLOAD_KEYS = new Set([
  "alreadyStopping",
  "arch",
  "attempt",
  "backoffMs",
  "channel",
  "code",
  "droppedKeys",
  "error",
  "eventLsn",
  "exitCode",
  "force",
  "isPackaged",
  "lastPingAt",
  "livenessAgeMs",
  "location",
  "maxRestartRetries",
  "payloadBytes",
  "pid",
  "platform",
  "port",
  "processType",
  "reason",
  "reportBytes",
  "rendererKind",
  "restartCount",
  "serviceName",
  "scheme",
  "signal",
  "stack",
  "status",
  "type",
  "uptimeMs",
  "version",
  "windowCount",
]);

const defaultRotationFileSystem: LoggerRotationFileSystem = {
  existsSync,
  renameSync,
  unlinkSync,
};

export class StructuredLogger {
  private readonly resolveLogDirectory: () => string | null;
  private readonly maxFileBytes: number;
  private readonly consoleWriter: (level: LogLevel, line: string) => void;
  private readonly monotonicNow: () => number;
  private readonly rotationFs: LoggerRotationFileSystem;
  private readonly currentFileBytes = new Map<string, number>();
  // Cache successful mkdirs, but clear on ENOENT so external log-dir removal
  // can recover with one mkdir + append retry instead of disabling file logs.
  private initializedLogDirectory: string | null = null;
  private eventLsn = 0;

  constructor(config: StructuredLoggerConfig = {}) {
    this.resolveLogDirectory = createLogDirectoryResolver(config.logDirectory);
    this.maxFileBytes = config.maxFileBytes ?? DEFAULT_MAX_FILE_BYTES;
    this.consoleWriter = config.consoleWriter ?? writeLineToConsole;
    this.monotonicNow = config.monotonicNow ?? monotonicNowMs;
    this.rotationFs = config.rotationFs ?? defaultRotationFileSystem;
  }

  forModule(module: string): Logger {
    assertLogName(module, "module");
    return new ModuleLogger(module, this);
  }

  write(level: LogLevel, module: string, event: string, payload: LogPayload = {}): void {
    assertLogName(event, "event");
    const safePayload = validatePayload(payload);
    const record = {
      ts: this.monotonicNow(),
      eventLsn: this.nextEventLsn(),
      level,
      module,
      event,
      ...safePayload,
    };
    const line = JSON.stringify(record);
    this.consoleWriter(level, line);
    this.writeLineToFile(line);
  }

  private nextEventLsn(): number {
    this.eventLsn += 1;
    return this.eventLsn;
  }

  private writeLineToFile(line: string): void {
    const logDirectory = this.resolveLogDirectory();
    if (logDirectory === null) {
      return;
    }

    const logPath = join(logDirectory, LOG_FILE_NAME);
    const lineWithTerminator = `${line}\n`;
    const lineBytes = Buffer.byteLength(lineWithTerminator, "utf8");

    try {
      if (this.initializedLogDirectory !== logDirectory) {
        mkdirSync(logDirectory, { recursive: true });
        this.initializedLogDirectory = logDirectory;
      }
      if (
        lineBytes < this.maxFileBytes &&
        this.getCurrentFileBytes(logPath) + lineBytes > this.maxFileBytes
      ) {
        rotateLogs(logDirectory, this.rotationFs);
        this.currentFileBytes.set(logPath, 0);
      }
      appendFileSync(logPath, lineWithTerminator, "utf8");
      this.currentFileBytes.set(logPath, (this.currentFileBytes.get(logPath) ?? 0) + lineBytes);
    } catch (error) {
      if (!isMissingPathError(error)) {
        return;
      }

      this.initializedLogDirectory = null;
      try {
        mkdirSync(logDirectory, { recursive: true });
        this.initializedLogDirectory = logDirectory;
        const retryBaseBytes = currentFileSize(logPath);
        appendFileSync(logPath, lineWithTerminator, "utf8");
        this.currentFileBytes.set(logPath, retryBaseBytes + lineBytes);
      } catch {
        return;
      }
    }
  }

  private getCurrentFileBytes(logPath: string): number {
    const cachedBytes = this.currentFileBytes.get(logPath);
    if (cachedBytes !== undefined) {
      return cachedBytes;
    }

    // Avoid statting every append. If another local actor truncates or mutates
    // the log file, rotation can happen one file early or late; the drift is
    // bounded by maxFileBytes and corrected after the next rotation.
    const bytes = currentFileSize(logPath);
    this.currentFileBytes.set(logPath, bytes);
    return bytes;
  }
}

class ModuleLogger implements Logger {
  constructor(
    private readonly module: string,
    private readonly sink: StructuredLogger,
  ) {}

  info(event: string, payload?: LogPayload): void {
    this.sink.write("info", this.module, event, payload);
  }

  warn(event: string, payload?: LogPayload): void {
    this.sink.write("warn", this.module, event, payload);
  }

  error(event: string, payload?: LogPayload): void {
    this.sink.write("error", this.module, event, payload);
  }
}

const defaultStructuredLogger = new StructuredLogger();

export function createLogger(module: string): Logger {
  return defaultStructuredLogger.forModule(module);
}

function createLogDirectoryResolver(
  logDirectory: StructuredLoggerConfig["logDirectory"],
): () => string | null {
  if (typeof logDirectory === "string") {
    return () => logDirectory;
  }

  if (typeof logDirectory === "function") {
    return () => logDirectory();
  }

  return () => {
    try {
      return app.getPath("logs");
    } catch {
      return null;
    }
  };
}

function validatePayload(payload: LogPayload): LogPayload {
  const safePayload: Record<string, LogPayloadValue> = {};
  for (const key in payload) {
    if (!Object.hasOwn(payload, key)) {
      continue;
    }
    const value = (payload as Record<string, LogPayloadValue>)[key] as LogPayloadValue;
    validatePayloadKey(key);
    safePayload[key] = normalizePayloadValue(value);
  }
  return safePayload;
}

function validatePayloadKey(key: string): void {
  if (!isSafePayloadKey(key)) {
    throw new UnsafeLogPayloadError(key);
  }
}

// Exposed so callers that funnel UNTRUSTED payloads (e.g. broker subprocess
// log forwarding) can pre-filter to safe keys instead of crashing the main
// process on the first banned key. Mirrors the gate in `validatePayloadKey`.
export function isSafePayloadKey(key: string): boolean {
  const normalizedKey = key.toLowerCase();
  if (BANNED_PAYLOAD_KEY_FRAGMENTS.some((fragment) => normalizedKey.includes(fragment))) {
    return false;
  }
  return SAFE_PAYLOAD_KEYS.has(key);
}

function normalizePayloadValue(value: LogPayloadValue): LogPayloadValue {
  if (typeof value !== "string") {
    return value;
  }

  if (value.length > MAX_LOG_STRING_BYTES) {
    return `${value.slice(0, MAX_LOG_STRING_BYTES)}...`;
  }

  if (Buffer.byteLength(value, "utf8") <= MAX_LOG_STRING_BYTES) {
    return value;
  }

  return `${value}...`;
}

function assertLogName(value: string, label: string): void {
  if (!LOG_NAME_PATTERN.test(value)) {
    throw new Error(`Invalid log ${label}: ${value}`);
  }
}

function currentFileSize(logPath: string): number {
  try {
    return statSync(logPath).size;
  } catch {
    return 0;
  }
}

function isMissingPathError(error: unknown): boolean {
  return error instanceof Error && (error as NodeJS.ErrnoException).code === "ENOENT";
}

function rotateLogs(logDirectory: string, rotationFs: LoggerRotationFileSystem): void {
  const oldestPath = join(logDirectory, `main.${ROTATED_FILE_COUNT}.log`);
  if (rotationFs.existsSync(oldestPath)) {
    rotationFs.unlinkSync(oldestPath);
  }

  for (let index = ROTATED_FILE_COUNT - 1; index >= 1; index -= 1) {
    const sourcePath = join(logDirectory, `main.${index}.log`);
    const targetPath = join(logDirectory, `main.${index + 1}.log`);
    if (rotationFs.existsSync(sourcePath)) {
      rotationFs.renameSync(sourcePath, targetPath);
    }
  }

  const activePath = join(logDirectory, LOG_FILE_NAME);
  if (!rotationFs.existsSync(activePath)) {
    return;
  }
  rotationFs.renameSync(activePath, join(logDirectory, "main.1.log"));
}

function writeLineToConsole(level: LogLevel, line: string): void {
  const stream = level === "error" || level === "warn" ? process.stderr : process.stdout;
  stream.write(`${line}\n`);
}
