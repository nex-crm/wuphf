import { execFile } from "node:child_process";
import { utilityProcess } from "electron";

import type { BrokerSnapshot, BrokerStatus } from "../shared/api-contract.ts";
import type { Logger } from "./logger.ts";
import { monotonicNowMs } from "./monotonic-clock.ts";

const BROKER_SERVICE_NAME = "wuphf-broker";
const BROKER_ENV_ALLOWLIST = ["PATH", "HOME", "USER", "LANG", "LC_ALL", "TZ"] as const;
const DEFAULT_STOP_GRACE_MS = 5_000;
const DEFAULT_FORCE_STOP_GRACE_MS = 1_000;
const DEFAULT_FIRST_BACKOFF_MS = 250;
const DEFAULT_MAX_BACKOFF_MS = 60_000;
const DEFAULT_MAX_RESTART_RETRIES = 5;
const DEFAULT_STABILITY_WINDOW_MS = 60_000;
const DEFAULT_LIVENESS_STALE_MS = 5_000;
// All three arrow bodies execute through the no-logger smoke test in
// broker-supervisor.spec.ts: start → broker_starting/broker_started (info),
// liveness staleness → broker_ping_missed (warn), restart cap → broker_restart_cap_reached (error).
const NOOP_LOGGER: Logger = {
  info: () => undefined,
  warn: () => undefined,
  error: () => undefined,
};

type UtilityProcessHandle = ReturnType<typeof utilityProcess.fork>;
type ForkUtilityProcess = typeof utilityProcess.fork;
type RunWindowsTaskkill = (pid: number, options: { readonly force: boolean }) => Promise<void>;
type KillProcess = (pid: number, signal: NodeJS.Signals) => void;
export type ExecFileRunner = (
  file: string,
  args: readonly string[],
  callback: (error: Error | null) => void,
) => void;
type MonotonicNow = () => number;

export interface BrokerSupervisorConfig {
  readonly brokerEntryPath: string;
  readonly envSource?: NodeJS.ProcessEnv;
  readonly forkProcess?: ForkUtilityProcess;
  readonly platform?: NodeJS.Platform;
  readonly runWindowsTaskkill?: RunWindowsTaskkill;
  readonly killProcess?: KillProcess;
  readonly monotonicNow?: MonotonicNow;
  readonly stopGraceMs?: number;
  readonly forceStopGraceMs?: number;
  readonly firstBackoffMs?: number;
  readonly maxBackoffMs?: number;
  readonly maxRestartRetries?: number;
  readonly stabilityWindowMs?: number;
  readonly livenessStaleMs?: number;
  readonly onFatal?: (reason: string) => void;
  readonly logger?: Logger;
}

export class BrokerSupervisor {
  private readonly brokerEntryPath: string;
  private readonly envSource: NodeJS.ProcessEnv;
  private readonly forkProcess: ForkUtilityProcess;
  private readonly platform: NodeJS.Platform;
  private readonly runWindowsTaskkill: RunWindowsTaskkill;
  private readonly killProcess: KillProcess;
  private readonly monotonicNow: MonotonicNow;
  private readonly stopGraceMs: number;
  private readonly forceStopGraceMs: number;
  private readonly firstBackoffMs: number;
  private readonly maxBackoffMs: number;
  private readonly maxRestartRetries: number;
  private readonly stabilityWindowMs: number;
  private readonly livenessStaleMs: number;
  private readonly onFatal: ((reason: string) => void) | undefined;
  private readonly logger: Logger;

  private brokerProcess: UtilityProcessHandle | null = null;
  private restartTimer: NodeJS.Timeout | null = null;
  private status: BrokerStatus = "unknown";
  private restartCount = 0;
  private stopping = false;
  private fatalReason: string | null = null;
  private lastRestartScheduledAtMs: number | null = null;
  private startedAtMs: number | null = null;
  private aliveSinceMs: number | null = null;
  private lastPingAtMs: number | null = null;

  constructor(config: BrokerSupervisorConfig) {
    this.brokerEntryPath = config.brokerEntryPath;
    this.envSource = config.envSource ?? process.env;
    this.forkProcess = config.forkProcess ?? utilityProcess.fork.bind(utilityProcess);
    this.platform = config.platform ?? process.platform;
    this.runWindowsTaskkill = config.runWindowsTaskkill ?? runWindowsTaskkill;
    this.killProcess = config.killProcess ?? process.kill.bind(process);
    this.monotonicNow = config.monotonicNow ?? monotonicNowMs;
    this.stopGraceMs = config.stopGraceMs ?? DEFAULT_STOP_GRACE_MS;
    this.forceStopGraceMs = config.forceStopGraceMs ?? DEFAULT_FORCE_STOP_GRACE_MS;
    this.firstBackoffMs = config.firstBackoffMs ?? DEFAULT_FIRST_BACKOFF_MS;
    this.maxBackoffMs = config.maxBackoffMs ?? DEFAULT_MAX_BACKOFF_MS;
    this.maxRestartRetries = config.maxRestartRetries ?? DEFAULT_MAX_RESTART_RETRIES;
    this.stabilityWindowMs = config.stabilityWindowMs ?? DEFAULT_STABILITY_WINDOW_MS;
    this.livenessStaleMs = config.livenessStaleMs ?? DEFAULT_LIVENESS_STALE_MS;
    this.onFatal = config.onFatal;
    this.logger = config.logger ?? NOOP_LOGGER;
  }

  start(): void {
    if (this.brokerProcess !== null || this.fatalReason !== null) {
      return;
    }

    this.stopping = false;
    this.status = "starting";
    this.aliveSinceMs = null;
    this.lastPingAtMs = null;
    this.startedAtMs = this.monotonicNow();
    this.logger.info("broker_starting", {
      restartCount: this.restartCount,
      serviceName: BROKER_SERVICE_NAME,
    });

    let brokerProcess: UtilityProcessHandle;
    try {
      brokerProcess = this.forkProcess(this.brokerEntryPath, [], {
        serviceName: BROKER_SERVICE_NAME,
        stdio: "pipe",
        env: buildBrokerEnv(this.envSource),
      });
    } catch (error) {
      this.status = "dead";
      this.startedAtMs = null;
      this.logger.error("broker_start_failed", {
        error: errorMessage(error),
        restartCount: this.restartCount,
        serviceName: BROKER_SERVICE_NAME,
      });
      throw error;
    }

    this.brokerProcess = brokerProcess;
    this.logger.info("broker_started", {
      pid: getProcessPid(brokerProcess),
      restartCount: this.restartCount,
      serviceName: BROKER_SERVICE_NAME,
    });
    drainBrokerStdio(brokerProcess);
    brokerProcess.on("message", (message: unknown) => {
      if (isAliveMessage(message)) {
        const nowMs = this.monotonicNow();
        if (this.status !== "alive") {
          this.aliveSinceMs = nowMs;
          this.logger.info("broker_alive", {
            pid: getProcessPid(brokerProcess),
            restartCount: this.restartCount,
          });
        }
        this.lastPingAtMs = nowMs;
        this.status = "alive";
      }
    });
    brokerProcess.once("exit", (exitCode: number) => {
      this.handleExit(brokerProcess, exitCode, null);
    });
  }

  getStatus(): BrokerStatus {
    if (
      this.status === "alive" &&
      this.lastPingAtMs !== null &&
      this.monotonicNow() - this.lastPingAtMs > this.livenessStaleMs
    ) {
      const nowMs = this.monotonicNow();
      this.status = "unresponsive";
      this.logger.warn("broker_ping_missed", {
        pid: getProcessPid(this.brokerProcess),
        lastPingAt: this.lastPingAtMs,
        livenessAgeMs: nowMs - this.lastPingAtMs,
        restartCount: this.restartCount,
      });
      return "unresponsive";
    }

    return this.status;
  }

  getPid(): number | null {
    return getProcessPid(this.brokerProcess);
  }

  getRestartCount(): number {
    return this.restartCount;
  }

  getSnapshot(): BrokerSnapshot {
    return {
      status: this.getStatus(),
      pid: getProcessPid(this.brokerProcess),
      restartCount: this.restartCount,
    };
  }

  getLastRestartScheduledAtMs(): number | null {
    return this.lastRestartScheduledAtMs;
  }

  async stop(): Promise<void> {
    this.stopping = true;
    this.clearRestartTimer();
    this.logger.info("broker_stop_requested", {
      pid: getProcessPid(this.brokerProcess),
      restartCount: this.restartCount,
      status: this.status,
    });

    const brokerProcess = this.brokerProcess;
    if (brokerProcess === null) {
      this.status = "dead";
      this.startedAtMs = null;
      this.aliveSinceMs = null;
      this.lastPingAtMs = null;
      this.logger.info("broker_stop_noop");
      return;
    }

    await new Promise<void>((resolve) => {
      let settled = false;
      let stopTimer: NodeJS.Timeout | null = null;

      const settle = (): void => {
        if (settled) {
          return;
        }
        settled = true;
        if (stopTimer !== null) {
          clearTimeout(stopTimer);
        }
        brokerProcess.off("exit", settle);
        if (this.brokerProcess === brokerProcess) {
          this.brokerProcess = null;
        }
        this.status = "dead";
        this.startedAtMs = null;
        this.aliveSinceMs = null;
        this.lastPingAtMs = null;
        this.logger.info("broker_stopped", {
          pid: getProcessPid(brokerProcess),
          restartCount: this.restartCount,
        });
        resolve();
      };

      const scheduleStopStep = (delayMs: number, step: () => void): void => {
        stopTimer = setTimeout(() => {
          stopTimer = null;
          step();
        }, delayMs);
      };

      brokerProcess.once("exit", settle);
      this.requestGracefulStop(brokerProcess);

      scheduleStopStep(this.stopGraceMs, () => {
        this.requestProcessTermination(brokerProcess);
        scheduleStopStep(this.forceStopGraceMs, () => {
          this.forceStop(brokerProcess);
          settle();
        });
      });
    });
  }

  private handleExit(
    exitedProcess: UtilityProcessHandle,
    exitCode: number | null = null,
    signal: string | null = null,
  ): void {
    if (this.brokerProcess !== exitedProcess) {
      return;
    }

    const nowMs = this.monotonicNow();
    const startedAtMs = this.startedAtMs;
    this.logger.warn("broker_exited", {
      pid: getProcessPid(exitedProcess),
      exitCode,
      signal,
      restartCount: this.restartCount,
      uptimeMs: startedAtMs === null ? null : nowMs - startedAtMs,
      lastPingAt: this.lastPingAtMs,
    });

    this.brokerProcess = null;
    this.lastPingAtMs = null;

    if (this.stopping) {
      this.status = "dead";
      this.aliveSinceMs = null;
      this.startedAtMs = null;
      return;
    }

    this.resetRestartCountAfterStableWindow();
    this.scheduleRestart();
  }

  private scheduleRestart(): void {
    const nextRestartCount = this.restartCount + 1;
    if (nextRestartCount > this.maxRestartRetries) {
      this.status = "dead";
      this.fatalReason = `Broker exited after ${this.restartCount} restart retries`;
      this.logger.error("broker_restart_cap_reached", {
        restartCount: this.restartCount,
        maxRestartRetries: this.maxRestartRetries,
      });
      this.onFatal?.(this.fatalReason);
      return;
    }

    this.restartCount = nextRestartCount;
    this.status = "starting";
    const backoffMs = Math.min(
      this.maxBackoffMs,
      this.firstBackoffMs * 2 ** (nextRestartCount - 1),
    );

    this.lastRestartScheduledAtMs = this.monotonicNow();
    this.logger.warn("broker_restart_scheduled", {
      restartCount: this.restartCount,
      backoffMs,
      maxRestartRetries: this.maxRestartRetries,
    });
    this.restartTimer = setTimeout(() => {
      this.restartTimer = null;
      // Symmetric guard at callback entry. Closes the same real-Node race
      // documented below in handleRestartStartFailure: if stop() runs while
      // this callback is already on the event queue, clearTimeout cannot
      // recall it. Without this check, start() would unconditionally clear
      // stopping=false and fork a fresh broker AFTER stop() completed,
      // leaking a process whether start() throws or succeeds.
      if (this.stopping || this.fatalReason !== null) {
        this.logger.info("broker_restart_skipped", {
          restartCount: this.restartCount,
          reason: this.stopping ? "stopping" : "fatal",
        });
        return;
      }
      this.logger.info("broker_restart_attempt", {
        restartCount: this.restartCount,
        maxRestartRetries: this.maxRestartRetries,
      });
      try {
        this.start();
      } catch (error) {
        this.handleRestartStartFailure(error);
      }
    }, backoffMs);
  }

  private handleRestartStartFailure(error: unknown): void {
    this.status = "dead";
    this.brokerProcess = null;
    this.startedAtMs = null;
    this.aliveSinceMs = null;
    this.lastPingAtMs = null;
    const message = errorMessage(error);
    this.logger.error("broker_restart_start_failed", {
      error: message,
      restartCount: this.restartCount,
      maxRestartRetries: this.maxRestartRetries,
      serviceName: BROKER_SERVICE_NAME,
    });

    // Belt-and-suspenders for the restart-after-stop race that the timer
    // callback's entry guard (in scheduleRestart) closes for the common path.
    // The entry guard catches the case where stop() ran before the callback
    // fired. This guard catches the residual case where stop() flips
    // stopping=true synchronously from inside start() (e.g. a synchronous
    // forkProcess hook that calls back into the supervisor) and then start()
    // throws. Without it we would fall through to scheduleRestart() and
    // leak a fresh broker AFTER stop() requested shutdown. The single-thread
    // event-loop model and Vitest fake timers make a deterministic test for
    // this exact path infeasible, so coverage is suppressed.
    /* v8 ignore start */
    if (this.stopping) {
      return;
    }
    /* v8 ignore stop */

    if (this.restartCount >= this.maxRestartRetries) {
      this.fatalReason = `Broker start failed after ${this.restartCount} restart retries: ${message}`;
      this.onFatal?.(this.fatalReason);
      return;
    }

    this.scheduleRestart();
  }

  private clearRestartTimer(): void {
    if (this.restartTimer !== null) {
      clearTimeout(this.restartTimer);
      this.restartTimer = null;
    }
  }

  private requestGracefulStop(brokerProcess: UtilityProcessHandle): void {
    this.logger.info("broker_graceful_stop_requested", {
      pid: getProcessPid(brokerProcess),
      restartCount: this.restartCount,
    });
    safePostMessage(brokerProcess, { type: "shutdown" });
  }

  private requestProcessTermination(brokerProcess: UtilityProcessHandle): void {
    const pid = getProcessPid(brokerProcess);
    this.logger.warn("broker_termination_requested", {
      pid,
      force: false,
      restartCount: this.restartCount,
    });
    if (this.platform === "win32") {
      if (pid !== null) {
        void this.runWindowsTaskkill(pid, { force: false }).catch((error: unknown) => {
          this.logTaskkillFailure(pid, false, error);
        });
      }
      return;
    }

    killUtilityProcess(brokerProcess);
  }

  private forceStop(brokerProcess: UtilityProcessHandle): void {
    const pid = getProcessPid(brokerProcess);
    this.logger.warn("broker_termination_requested", {
      pid,
      force: true,
      restartCount: this.restartCount,
    });
    if (this.platform === "win32") {
      if (pid !== null) {
        void this.runWindowsTaskkill(pid, { force: true }).catch((error: unknown) => {
          this.logTaskkillFailure(pid, true, error);
        });
        return;
      }
    }

    if (pid !== null) {
      try {
        this.killProcess(pid, "SIGKILL");
        return;
      } catch (error) {
        this.logger.warn("broker_posix_sigkill_failed", {
          pid,
          error: errorMessage(error),
          code: errorCode(error),
          restartCount: this.restartCount,
        });
      }
    }

    killUtilityProcess(brokerProcess);
  }

  private logTaskkillFailure(pid: number, force: boolean, error: unknown): void {
    this.logger.warn("broker_taskkill_failed", {
      pid,
      force,
      error: errorMessage(error),
      code: errorCode(error),
    });
  }

  private resetRestartCountAfterStableWindow(): void {
    if (
      this.aliveSinceMs !== null &&
      this.monotonicNow() - this.aliveSinceMs > this.stabilityWindowMs
    ) {
      this.restartCount = 0;
    }
    this.aliveSinceMs = null;
  }
}

export function buildBrokerEnv(envSource: NodeJS.ProcessEnv): Record<string, string> {
  const brokerEnv: Record<string, string> = {};
  for (const key of BROKER_ENV_ALLOWLIST) {
    const value = envSource[key];
    if (typeof value === "string") {
      brokerEnv[key] = value;
    }
  }
  return brokerEnv;
}

function getProcessPid(brokerProcess: UtilityProcessHandle | null): number | null {
  const pid = brokerProcess?.pid;
  return typeof pid === "number" ? pid : null;
}

function isAliveMessage(message: unknown): message is { readonly alive: true } {
  return (
    typeof message === "object" &&
    message !== null &&
    Object.hasOwn(message, "alive") &&
    (message as { readonly alive?: unknown }).alive === true
  );
}

function safePostMessage(brokerProcess: UtilityProcessHandle, message: unknown): void {
  try {
    brokerProcess.postMessage(message);
  } catch {
    // The force path remains armed; a closed message port should not block app quit.
  }
}

function drainBrokerStdio(brokerProcess: UtilityProcessHandle): void {
  brokerProcess.stdout?.on("data", discardBrokerOutput);
  brokerProcess.stderr?.on("data", discardBrokerOutput);
}

function discardBrokerOutput(_chunk: unknown): void {
  // Drain pipe-backed stdio without logging future broker app data into the main process.
}

function killUtilityProcess(brokerProcess: UtilityProcessHandle): void {
  brokerProcess.kill();
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function errorCode(error: unknown): string | null {
  if (typeof error !== "object" || error === null || !Object.hasOwn(error, "code")) {
    return null;
  }

  const code = (error as { readonly code?: unknown }).code;
  return typeof code === "string" ? code : null;
}

export function runWindowsTaskkill(
  pid: number,
  options: { readonly force: boolean },
  execFileRunner: ExecFileRunner = defaultExecFile,
): Promise<void> {
  const args = ["/pid", String(pid), "/T"];
  if (options.force) {
    args.push("/F");
  }

  return new Promise((resolve, reject) => {
    execFileRunner("taskkill", args, (error) => {
      if (error === null) {
        resolve();
        return;
      }

      reject(error);
    });
  });
}

function defaultExecFile(
  file: string,
  args: readonly string[],
  callback: (error: Error | null) => void,
): void {
  execFile(file, [...args], (error) => {
    callback(error);
  });
}
