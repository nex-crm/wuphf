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
const LOG_NAME_PATTERN = /^[a-z0-9_.:-]+$/;
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
  "maxRestartRetries",
  "payloadBytes",
  "pid",
  "platform",
  "port",
  "processType",
  "reason",
  "rendererKind",
  "restartCount",
  "serviceName",
  "scheme",
  "signal",
  "stack",
  "status",
  "uptimeMs",
  "version",
  "windowCount",
]);

export class StructuredLogger {
  private readonly resolveLogDirectory: () => string | null;
  private readonly maxFileBytes: number;
  private readonly consoleWriter: (level: LogLevel, line: string) => void;
  private readonly monotonicNow: () => number;
  private eventLsn = 0;

  constructor(config: StructuredLoggerConfig = {}) {
    this.resolveLogDirectory = createLogDirectoryResolver(config.logDirectory);
    this.maxFileBytes = config.maxFileBytes ?? DEFAULT_MAX_FILE_BYTES;
    this.consoleWriter = config.consoleWriter ?? writeLineToConsole;
    this.monotonicNow = config.monotonicNow ?? monotonicNowMs;
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

    try {
      mkdirSync(logDirectory, { recursive: true });
      const logPath = join(logDirectory, LOG_FILE_NAME);
      const lineBytes = Buffer.byteLength(line, "utf8") + 1;
      if (
        lineBytes < this.maxFileBytes &&
        currentFileSize(logPath) + lineBytes > this.maxFileBytes
      ) {
        rotateLogs(logDirectory);
      }
      appendFileSync(logPath, `${line}\n`, "utf8");
    } catch {
      return;
    }
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
  for (const [key, value] of Object.entries(payload)) {
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

  if (Buffer.byteLength(value, "utf8") <= MAX_LOG_STRING_BYTES) {
    return value;
  }

  return `${value.slice(0, MAX_LOG_STRING_BYTES)}...`;
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

function rotateLogs(logDirectory: string): void {
  const oldestPath = join(logDirectory, `main.${ROTATED_FILE_COUNT}.log`);
  if (existsSync(oldestPath)) {
    unlinkSync(oldestPath);
  }

  for (let index = ROTATED_FILE_COUNT - 1; index >= 1; index -= 1) {
    const sourcePath = join(logDirectory, `main.${index}.log`);
    const targetPath = join(logDirectory, `main.${index + 1}.log`);
    if (existsSync(sourcePath)) {
      renameSync(sourcePath, targetPath);
    }
  }

  const activePath = join(logDirectory, LOG_FILE_NAME);
  // Defensive — rotation is only invoked from the size-trigger inside
  // `appendToLogFile`, which has just written to `activePath`, so the
  // file MUST exist at this point. The existence check guards against
  // an external delete racing the rotation; that path is genuinely
  // unreachable in-process.
  /* v8 ignore start */
  if (!existsSync(activePath)) {
    return;
  }
  /* v8 ignore stop */
  renameSync(activePath, join(logDirectory, "main.1.log"));
}

function writeLineToConsole(level: LogLevel, line: string): void {
  const stream = level === "error" || level === "warn" ? process.stderr : process.stdout;
  stream.write(`${line}\n`);
}
