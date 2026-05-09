import { execFile } from "node:child_process";
import { utilityProcess } from "electron";

import type { BrokerStatus } from "../shared/api-contract.ts";
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

type UtilityProcessHandle = ReturnType<typeof utilityProcess.fork>;
type ForkUtilityProcess = typeof utilityProcess.fork;
type RunWindowsTaskkill = (pid: number, options: { readonly force: boolean }) => Promise<void>;
type MonotonicNow = () => number;

export interface BrokerSupervisorConfig {
  readonly brokerEntryPath: string;
  readonly envSource?: NodeJS.ProcessEnv;
  readonly forkProcess?: ForkUtilityProcess;
  readonly platform?: NodeJS.Platform;
  readonly runWindowsTaskkill?: RunWindowsTaskkill;
  readonly monotonicNow?: MonotonicNow;
  readonly stopGraceMs?: number;
  readonly forceStopGraceMs?: number;
  readonly firstBackoffMs?: number;
  readonly maxBackoffMs?: number;
  readonly maxRestartRetries?: number;
  readonly stabilityWindowMs?: number;
  readonly livenessStaleMs?: number;
  readonly onFatal?: (reason: string) => void;
}

export class BrokerSupervisor {
  private readonly brokerEntryPath: string;
  private readonly envSource: NodeJS.ProcessEnv;
  private readonly forkProcess: ForkUtilityProcess;
  private readonly platform: NodeJS.Platform;
  private readonly runWindowsTaskkill: RunWindowsTaskkill;
  private readonly monotonicNow: MonotonicNow;
  private readonly stopGraceMs: number;
  private readonly forceStopGraceMs: number;
  private readonly firstBackoffMs: number;
  private readonly maxBackoffMs: number;
  private readonly maxRestartRetries: number;
  private readonly stabilityWindowMs: number;
  private readonly livenessStaleMs: number;
  private readonly onFatal: ((reason: string) => void) | undefined;

  private brokerProcess: UtilityProcessHandle | null = null;
  private restartTimer: NodeJS.Timeout | null = null;
  private status: BrokerStatus = "unknown";
  private restartCount = 0;
  private stopping = false;
  private fatalReason: string | null = null;
  private lastRestartScheduledAtMs: number | null = null;
  private aliveSinceMs: number | null = null;
  private lastPingAtMs: number | null = null;

  constructor(config: BrokerSupervisorConfig) {
    this.brokerEntryPath = config.brokerEntryPath;
    this.envSource = config.envSource ?? process.env;
    this.forkProcess = config.forkProcess ?? utilityProcess.fork.bind(utilityProcess);
    this.platform = config.platform ?? process.platform;
    this.runWindowsTaskkill = config.runWindowsTaskkill ?? runWindowsTaskkill;
    this.monotonicNow = config.monotonicNow ?? monotonicNowMs;
    this.stopGraceMs = config.stopGraceMs ?? DEFAULT_STOP_GRACE_MS;
    this.forceStopGraceMs = config.forceStopGraceMs ?? DEFAULT_FORCE_STOP_GRACE_MS;
    this.firstBackoffMs = config.firstBackoffMs ?? DEFAULT_FIRST_BACKOFF_MS;
    this.maxBackoffMs = config.maxBackoffMs ?? DEFAULT_MAX_BACKOFF_MS;
    this.maxRestartRetries = config.maxRestartRetries ?? DEFAULT_MAX_RESTART_RETRIES;
    this.stabilityWindowMs = config.stabilityWindowMs ?? DEFAULT_STABILITY_WINDOW_MS;
    this.livenessStaleMs = config.livenessStaleMs ?? DEFAULT_LIVENESS_STALE_MS;
    this.onFatal = config.onFatal;
  }

  start(): void {
    if (this.brokerProcess !== null || this.fatalReason !== null) {
      return;
    }

    this.stopping = false;
    this.status = "starting";
    this.aliveSinceMs = null;
    this.lastPingAtMs = null;
    const brokerProcess = this.forkProcess(this.brokerEntryPath, [], {
      serviceName: BROKER_SERVICE_NAME,
      stdio: "pipe",
      env: buildBrokerEnv(this.envSource),
    });

    this.brokerProcess = brokerProcess;
    drainBrokerStdio(brokerProcess);
    brokerProcess.on("message", (message: unknown) => {
      if (isAliveMessage(message)) {
        const nowMs = this.monotonicNow();
        if (this.status !== "alive") {
          this.aliveSinceMs = nowMs;
        }
        this.lastPingAtMs = nowMs;
        this.status = "alive";
      }
    });
    brokerProcess.once("exit", () => {
      this.handleExit(brokerProcess);
    });
  }

  getStatus(): BrokerStatus {
    if (
      this.status === "alive" &&
      this.lastPingAtMs !== null &&
      this.monotonicNow() - this.lastPingAtMs > this.livenessStaleMs
    ) {
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

  getLastRestartScheduledAtMs(): number | null {
    return this.lastRestartScheduledAtMs;
  }

  async stop(): Promise<void> {
    this.stopping = true;
    this.clearRestartTimer();

    const brokerProcess = this.brokerProcess;
    if (brokerProcess === null) {
      this.status = "dead";
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

  private handleExit(exitedProcess: UtilityProcessHandle): void {
    if (this.brokerProcess !== exitedProcess) {
      return;
    }

    this.brokerProcess = null;
    this.lastPingAtMs = null;

    if (this.stopping) {
      this.status = "dead";
      this.aliveSinceMs = null;
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
    this.restartTimer = setTimeout(() => {
      this.restartTimer = null;
      this.start();
    }, backoffMs);
  }

  private clearRestartTimer(): void {
    if (this.restartTimer !== null) {
      clearTimeout(this.restartTimer);
      this.restartTimer = null;
    }
  }

  private requestGracefulStop(brokerProcess: UtilityProcessHandle): void {
    safePostMessage(brokerProcess, { type: "shutdown" });
  }

  private requestProcessTermination(brokerProcess: UtilityProcessHandle): void {
    if (this.platform === "win32") {
      const pid = getProcessPid(brokerProcess);
      if (pid !== null) {
        void this.runWindowsTaskkill(pid, { force: false }).catch(() => undefined);
      }
      return;
    }

    killUtilityProcess(brokerProcess);
  }

  private forceStop(brokerProcess: UtilityProcessHandle): void {
    if (this.platform === "win32") {
      const pid = getProcessPid(brokerProcess);
      if (pid !== null) {
        void this.runWindowsTaskkill(pid, { force: true }).catch(() => undefined);
        return;
      }
    }

    killUtilityProcess(brokerProcess, "SIGKILL");
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

function killUtilityProcess(brokerProcess: UtilityProcessHandle, signal?: NodeJS.Signals): void {
  if (signal === undefined) {
    brokerProcess.kill();
    return;
  }

  // Electron 33's type omits the signal overload; keep the handle-bound receiver.
  Reflect.apply(brokerProcess.kill, brokerProcess, [signal]);
}

function runWindowsTaskkill(pid: number, options: { readonly force: boolean }): Promise<void> {
  const args = ["/pid", String(pid), "/T"];
  if (options.force) {
    args.push("/F");
  }

  return new Promise((resolve, reject) => {
    execFile("taskkill", args, (error) => {
      if (error === null) {
        resolve();
        return;
      }

      reject(error);
    });
  });
}
